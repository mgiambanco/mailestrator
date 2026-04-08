import SwiftUI
import WebKit

struct EmailDetailView: View {
    @EnvironmentObject var store: MailStore

    var body: some View {
        Group {
            if let email = store.selectedEmail {
                ScrollView {
                    VStack(alignment: .leading, spacing: 16) {
                        // Header
                        VStack(alignment: .leading, spacing: 6) {
                            Text(email.subject.isEmpty ? "(no subject)" : email.subject)
                                .font(.title2.bold())

                            HStack {
                                Image(systemName: "person.circle.fill")
                                    .foregroundStyle(.secondary)
                                Text(email.from)
                                    .font(.subheadline)
                                    .foregroundStyle(.secondary)
                            }

                            Text(email.received_at.formatted(date: .long, time: .shortened))
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }
                        .padding(.horizontal)

                        Divider()

                        // Body
                        if !email.body_html.isEmpty {
                            HTMLView(html: email.body_html)
                                .frame(minHeight: 300)
                                .padding(.horizontal)
                        } else {
                            Text(email.body_text.isEmpty ? "(empty message)" : email.body_text)
                                .font(.body)
                                .textSelection(.enabled)
                                .padding(.horizontal)
                        }

                        // Attachments
                        if let attachments = email.attachments, !attachments.isEmpty,
                           let account = store.selectedAccount {
                            AttachmentListView(
                                attachments: attachments,
                                accountID: account.id,
                                emailID: email.id,
                                token: account.token
                            )
                            .padding(.horizontal)
                        }
                    }
                    .padding(.vertical)
                }
                .navigationTitle("Message")
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .destructiveAction) {
                        Button(role: .destructive) {
                            Task { await store.deleteEmail(email) }
                        } label: {
                            Image(systemName: "trash")
                        }
                    }
                }
                // #18 — update badge when marking an email read
                .onAppear { store.updateBadge() }
            } else {
                ContentUnavailableView(
                    "Select a Message",
                    systemImage: "envelope.open",
                    description: Text("Choose a message from your inbox.")
                )
            }
        }
    }
}

// Simple WKWebView wrapper for rendering HTML email bodies.
struct HTMLView: UIViewRepresentable {
    let html: String

    func makeUIView(context: Context) -> WKWebView {
        let prefs = WKWebpagePreferences()
        // Disable JavaScript — email HTML must never execute scripts (XSS prevention).
        prefs.allowsContentJavaScript = false

        let config = WKWebViewConfiguration()
        config.defaultWebpagePreferences = prefs

        let wv = WKWebView(frame: .zero, configuration: config)
        wv.scrollView.isScrollEnabled = false
        return wv
    }

    func updateUIView(_ uiView: WKWebView, context: Context) {
        let wrapped = """
        <html><head>
        <meta name="viewport" content="width=device-width, initial-scale=1">
        <style>body { font-family: -apple-system; font-size: 16px; word-wrap: break-word; }</style>
        </head><body>\(html)</body></html>
        """
        // baseURL: nil prevents the WebView from loading external resources.
        uiView.loadHTMLString(wrapped, baseURL: nil)
    }
}
