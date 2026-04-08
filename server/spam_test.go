package main

import (
	"context"
	"fmt"
	"net"
	"testing"

	"blitiri.com.ar/go/spf"
)

// ── fake DNS resolver ────────────────────────────────────────────────────────

// fakeResolver answers LookupHost with a fixed map of host → addresses.
// Any host not in the map returns NXDOMAIN (empty addrs, nil error).
type fakeResolver struct {
	listed map[string][]string
}

func (f *fakeResolver) LookupHost(_ context.Context, host string) ([]string, error) {
	if addrs, ok := f.listed[host]; ok {
		return addrs, nil
	}
	return nil, nil
}

// ── fake SPF checker ─────────────────────────────────────────────────────────

type fakeSPF struct {
	result spf.Result
	err    error
}

func (f *fakeSPF) Check(_ context.Context, _ net.IP, _, _ string) (spf.Result, error) {
	return f.result, f.err
}

// ── helper ───────────────────────────────────────────────────────────────────

func newTestFilter(dnsbls []string, spfOn, spfReject bool, resolver dnsResolver, spfck spfChecker) *SpamFilter {
	return &SpamFilter{
		dnsbls:    dnsbls,
		spfOn:     spfOn,
		spfReject: spfReject,
		resolver:  resolver,
		spf:       spfck,
	}
}

// ── DNSBL tests ──────────────────────────────────────────────────────────────

func TestDNSBL_ListedIP_Rejected(t *testing.T) {
	ip := net.ParseIP("1.2.3.4")
	resolver := &fakeResolver{
		listed: map[string][]string{
			"4.3.2.1.zen.spamhaus.org": {"127.0.0.2"},
		},
	}
	f := newTestFilter([]string{"zen.spamhaus.org"}, false, false, resolver, &fakeSPF{})
	if err := f.CheckDNSBL(context.Background(), ip); err == nil {
		t.Error("expected rejection for listed IP, got nil")
	}
}

func TestDNSBL_CleanIP_Allowed(t *testing.T) {
	ip := net.ParseIP("5.6.7.8")
	resolver := &fakeResolver{listed: map[string][]string{}}
	f := newTestFilter([]string{"zen.spamhaus.org"}, false, false, resolver, &fakeSPF{})
	if err := f.CheckDNSBL(context.Background(), ip); err != nil {
		t.Errorf("unexpected rejection for clean IP: %v", err)
	}
}

func TestDNSBL_PrivateIP_Skipped(t *testing.T) {
	// Private IPs must never be checked — even if the resolver would say listed.
	for _, addr := range []string{"127.0.0.1", "10.0.0.1", "192.168.1.1", "172.16.0.1"} {
		ip := net.ParseIP(addr)
		resolver := &fakeResolver{
			listed: map[string][]string{
				// Any reverse lookup returns a hit — but should never be called.
				fmt.Sprintf("%s.zen.spamhaus.org", ip): {"127.0.0.2"},
			},
		}
		f := newTestFilter([]string{"zen.spamhaus.org"}, false, false, resolver, &fakeSPF{})
		if err := f.CheckDNSBL(context.Background(), ip); err != nil {
			t.Errorf("private IP %s should not be DNSBL-checked, got: %v", addr, err)
		}
	}
}

func TestDNSBL_Disabled_WhenNoneConfigured(t *testing.T) {
	ip := net.ParseIP("1.2.3.4")
	// Resolver that would fail if called.
	resolver := &fakeResolver{listed: map[string][]string{
		"4.3.2.1.zen.spamhaus.org": {"127.0.0.2"},
	}}
	f := newTestFilter([]string{}, false, false, resolver, &fakeSPF{})
	if err := f.CheckDNSBL(context.Background(), ip); err != nil {
		t.Errorf("DNSBL disabled but still rejected: %v", err)
	}
}

func TestDNSBL_DNSError_FailOpen(t *testing.T) {
	ip := net.ParseIP("1.2.3.4")
	// nil listed map means all lookups return empty — simulates NXDOMAIN.
	resolver := &fakeResolver{}
	f := newTestFilter([]string{"zen.spamhaus.org"}, false, false, resolver, &fakeSPF{})
	if err := f.CheckDNSBL(context.Background(), ip); err != nil {
		t.Errorf("DNS error should fail-open, got: %v", err)
	}
}

func TestDNSBL_IPv6_Reversed(t *testing.T) {
	ip := net.ParseIP("2001:db8::1")
	reversed, err := reversedIP(ip)
	if err != nil {
		t.Fatalf("reversedIP: %v", err)
	}
	// 2001:0db8:0000:0000:0000:0000:0000:0001
	// hex = "20010db8000000000000000000000001"
	// reversed nibbles joined by ".": "1.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.8.b.d.0.1.0.0.2"
	if reversed == "" {
		t.Error("expected non-empty reversed IPv6")
	}
	t.Logf("IPv6 DNSBL form: %s", reversed)
}

// ── SPF tests ────────────────────────────────────────────────────────────────

func TestSPF_Pass_Allowed(t *testing.T) {
	ip := net.ParseIP("1.2.3.4")
	f := newTestFilter(nil, true, true, &fakeResolver{}, &fakeSPF{result: spf.Pass})
	if err := f.CheckSPF(context.Background(), ip, "mail.example.com", "sender@example.com"); err != nil {
		t.Errorf("SPF Pass should be allowed: %v", err)
	}
}

func TestSPF_Fail_Rejected_WhenRejectEnabled(t *testing.T) {
	ip := net.ParseIP("1.2.3.4")
	f := newTestFilter(nil, true, true, &fakeResolver{}, &fakeSPF{result: spf.Fail})
	if err := f.CheckSPF(context.Background(), ip, "", "sender@example.com"); err == nil {
		t.Error("SPF Fail with spfReject=true should be rejected")
	}
}

func TestSPF_Fail_LogOnly_WhenRejectDisabled(t *testing.T) {
	ip := net.ParseIP("1.2.3.4")
	f := newTestFilter(nil, true, false, &fakeResolver{}, &fakeSPF{result: spf.Fail})
	if err := f.CheckSPF(context.Background(), ip, "", "sender@example.com"); err != nil {
		t.Errorf("SPF Fail with spfReject=false should be allowed (log-only): %v", err)
	}
}

func TestSPF_SoftFail_AlwaysAllowed(t *testing.T) {
	ip := net.ParseIP("1.2.3.4")
	f := newTestFilter(nil, true, true, &fakeResolver{}, &fakeSPF{result: spf.SoftFail})
	if err := f.CheckSPF(context.Background(), ip, "", "sender@example.com"); err != nil {
		t.Errorf("SPF SoftFail should always be allowed: %v", err)
	}
}

func TestSPF_Disabled_Skipped(t *testing.T) {
	ip := net.ParseIP("1.2.3.4")
	// spfOn=false — checker should never be called
	called := false
	f := newTestFilter(nil, false, true, &fakeResolver{}, &fakeSPF{result: spf.Fail})
	_ = called
	if err := f.CheckSPF(context.Background(), ip, "", "sender@example.com"); err != nil {
		t.Errorf("SPF disabled but still rejected: %v", err)
	}
}

func TestSPF_PrivateIP_Skipped(t *testing.T) {
	ip := net.ParseIP("192.168.1.100")
	f := newTestFilter(nil, true, true, &fakeResolver{}, &fakeSPF{result: spf.Fail})
	if err := f.CheckSPF(context.Background(), ip, "", "sender@example.com"); err != nil {
		t.Errorf("private IP should skip SPF check: %v", err)
	}
}

func TestSPF_Error_FailOpen(t *testing.T) {
	ip := net.ParseIP("1.2.3.4")
	f := newTestFilter(nil, true, true, &fakeResolver{}, &fakeSPF{result: spf.TempError, err: fmt.Errorf("DNS timeout")})
	if err := f.CheckSPF(context.Background(), ip, "", "sender@example.com"); err != nil {
		t.Errorf("SPF error should fail-open: %v", err)
	}
}

// ── reversedIP tests ─────────────────────────────────────────────────────────

func TestReversedIP_IPv4(t *testing.T) {
	cases := []struct{ ip, want string }{
		{"1.2.3.4", "4.3.2.1"},
		{"192.168.1.100", "100.1.168.192"},
		{"10.0.0.1", "1.0.0.10"},
	}
	for _, tc := range cases {
		got, err := reversedIP(net.ParseIP(tc.ip))
		if err != nil {
			t.Errorf("reversedIP(%s): %v", tc.ip, err)
			continue
		}
		if got != tc.want {
			t.Errorf("reversedIP(%s) = %q, want %q", tc.ip, got, tc.want)
		}
	}
}
