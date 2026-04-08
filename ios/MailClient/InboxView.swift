import SwiftUI

struct InboxView: View {
    @EnvironmentObject var store: MailStore
    // #19 — search state
    @State private var searchText = ""

    private var allEmails: [Email] {
        guard let account = store.selectedAccount else { return [] }
        return store.emailsByAccount[account.id] ?? []
    }

    // Filter by from / subject / body when searchText is non-empty.
    private var filteredEmails: [Email] {
        guard !searchText.isEmpty else { return allEmails }
        let q = searchText.lowercased()
        return allEmails.filter {
            $0.from.lowercased().contains(q) ||
            $0.subject.lowercased().contains(q) ||
            $0.body_text.lowercased().contains(q)
        }
    }

    var body: some View {
        Group {
            if let account = store.selectedAccount {
                List(selection: $store.selectedEmail) {
                    ForEach(filteredEmails) { email in
                        EmailRow(email: email)
                            .tag(email)
                            .swipeActions(edge: .trailing, allowsFullSwipe: true) {
                                Button(role: .destructive) {
                                    Task { await store.deleteEmail(email) }
                                } label: {
                                    Label("Delete", systemImage: "trash")
                                }
                            }
                    }

                    // Load More footer — hide when a search is active
                    if searchText.isEmpty && store.hasMoreByAccount[account.id] == true {
                        HStack {
                            Spacer()
                            if store.loadingMoreByAccount[account.id] == true {
                                ProgressView()
                            } else {
                                Button("Load More") {
                                    Task { await store.loadMoreEmails(for: account.id) }
                                }
                                .buttonStyle(.bordered)
                            }
                            Spacer()
                        }
                        .padding(.vertical, 8)
                        .listRowSeparator(.hidden)
                    }
                }
                .listStyle(.plain)
                // #19 — search bar
                .searchable(text: $searchText, placement: .navigationBarDrawer(displayMode: .automatic), prompt: "Search messages")
                .navigationTitle(account.displayName)
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    // #17 — copy address button
                    ToolbarItem(placement: .topBarTrailing) {
                        Button {
                            UIPasteboard.general.string = account.address
                        } label: {
                            Image(systemName: "doc.on.doc")
                        }
                        .help("Copy \(account.address)")
                    }
                }
                .refreshable {
                    await store.loadEmails(for: account.id)
                }
                .overlay {
                    if allEmails.isEmpty {
                        ContentUnavailableView(
                            "No Messages",
                            systemImage: "tray",
                            description: Text("Emails sent to \(account.address) will appear here.")
                        )
                    } else if filteredEmails.isEmpty {
                        ContentUnavailableView.search(text: searchText)
                    }
                }
            } else {
                ContentUnavailableView(
                    "No Account Selected",
                    systemImage: "envelope",
                    description: Text("Tap + to create a new random email address.")
                )
            }
        }
    }
}

struct EmailRow: View {
    let email: Email

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text(email.from)
                    .font(.subheadline.weight(email.read ? .regular : .semibold))
                    .lineLimit(1)
                Spacer()
                Text(email.received_at, style: .relative)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            HStack(spacing: 4) {
                Text(email.subject.isEmpty ? "(no subject)" : email.subject)
                    .font(.subheadline)
                    .foregroundStyle(email.read ? .secondary : .primary)
                    .lineLimit(1)
                if email.attachment_count > 0 {
                    Image(systemName: "paperclip")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
            Text(email.body_text)
                .font(.caption)
                .foregroundStyle(.secondary)
                .lineLimit(2)
        }
        .padding(.vertical, 4)
        .overlay(alignment: .leading) {
            if !email.read {
                Circle()
                    .fill(.blue)
                    .frame(width: 8, height: 8)
                    .offset(x: -14)
            }
        }
    }
}
