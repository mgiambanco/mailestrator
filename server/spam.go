package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"

	"blitiri.com.ar/go/spf"
)

// privateRanges holds CIDR blocks that are never listed in DNSBLs.
// Checking private IPs against public blocklists is meaningless and violates
// Spamhaus's usage policy.
var privateRanges []*net.IPNet

func init() {
	for _, cidr := range []string{
		"127.0.0.0/8",    // loopback
		"10.0.0.0/8",     // RFC1918
		"172.16.0.0/12",  // RFC1918
		"192.168.0.0/16", // RFC1918
		"::1/128",        // IPv6 loopback
		"fc00::/7",       // IPv6 unique-local
	} {
		_, block, _ := net.ParseCIDR(cidr)
		privateRanges = append(privateRanges, block)
	}
}

func isPrivateIP(ip net.IP) bool {
	for _, block := range privateRanges {
		if block.Contains(ip) {
			return true
		}
	}
	return false
}

// dnsResolver is a small interface over net.DefaultResolver so tests can
// inject a fake without making real DNS calls.
type dnsResolver interface {
	LookupHost(ctx context.Context, host string) ([]string, error)
}

type realResolver struct{}

func (realResolver) LookupHost(ctx context.Context, host string) ([]string, error) {
	return net.DefaultResolver.LookupHost(ctx, host)
}

// spfChecker is a small interface over the spf library so tests can inject a stub.
type spfChecker interface {
	Check(ctx context.Context, ip net.IP, helo, sender string) (spf.Result, error)
}

type realSPF struct{}

func (realSPF) Check(ctx context.Context, ip net.IP, helo, sender string) (spf.Result, error) {
	return spf.CheckHostWithSender(ip, helo, sender)
}

// SpamFilter performs DNSBL and SPF checks for incoming SMTP connections.
// All checks are fail-open: DNS errors are logged but do not reject mail.
type SpamFilter struct {
	dnsbls    []string
	spfOn     bool
	spfReject bool
	resolver  dnsResolver
	spf       spfChecker
}

func NewSpamFilter(cfg *Config) *SpamFilter {
	return &SpamFilter{
		dnsbls:    cfg.SpamDNSBLs,
		spfOn:     cfg.SpamSPF,
		spfReject: cfg.SpamSPFReject,
		resolver:  realResolver{},
		spf:       realSPF{},
	}
}

// CheckDNSBL looks up ip against each configured DNSBL zone.
// Returns a non-nil error (suitable for returning from smtp.Session) if the IP
// is listed. Returns nil if clean or if DNS errors occur (fail-open).
func (f *SpamFilter) CheckDNSBL(ctx context.Context, ip net.IP) error {
	if len(f.dnsbls) == 0 || isPrivateIP(ip) {
		return nil
	}

	reversed, err := reversedIP(ip)
	if err != nil {
		// Not an IP we can check — skip.
		return nil
	}

	for _, zone := range f.dnsbls {
		lookup := reversed + "." + zone
		addrs, err := f.resolver.LookupHost(ctx, lookup)
		if err != nil {
			// NXDOMAIN or DNS error — not listed (or unavailable, fail-open).
			continue
		}
		if len(addrs) > 0 {
			slog.Warn("smtp: DNSBL hit", "ip", ip.String(), "zone", zone, "response", addrs[0])
			return fmt.Errorf("connection refused: %s is listed in %s", ip, zone)
		}
	}
	return nil
}

// CheckSPF verifies the sender's SPF policy. If the result is Fail and
// spfReject is true, a non-nil error is returned. All other outcomes
// (Pass, SoftFail, Neutral, None, TempError, PermError) are logged but
// allowed through.
func (f *SpamFilter) CheckSPF(ctx context.Context, ip net.IP, helo, sender string) error {
	if !f.spfOn || sender == "" || isPrivateIP(ip) {
		return nil
	}

	result, err := f.spf.Check(ctx, ip, helo, sender)
	if err != nil {
		slog.Warn("smtp: SPF check error (fail-open)", "sender", sender, "ip", ip.String(), "err", err)
		return nil
	}

	slog.Info("smtp: SPF result", "sender", sender, "ip", ip.String(), "result", result)

	if result == spf.Fail && f.spfReject {
		return fmt.Errorf("SPF check failed for %s from %s", sender, ip)
	}
	return nil
}

// reversedIP returns the DNSBL-query form of an IP:
//   - IPv4 1.2.3.4  → "4.3.2.1"
//   - IPv6 uses nibble-reversed format per RFC 5782
func reversedIP(ip net.IP) (string, error) {
	if v4 := ip.To4(); v4 != nil {
		return fmt.Sprintf("%d.%d.%d.%d", v4[3], v4[2], v4[1], v4[0]), nil
	}

	v6 := ip.To16()
	if v6 == nil {
		return "", fmt.Errorf("not a valid IP")
	}
	// Expand to 32 hex nibbles and reverse.
	hex := fmt.Sprintf("%x", []byte(v6))
	runes := []rune(hex)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	nibbles := make([]string, len(runes))
	for i, r := range runes {
		nibbles[i] = string(r)
	}
	return strings.Join(nibbles, "."), nil
}
