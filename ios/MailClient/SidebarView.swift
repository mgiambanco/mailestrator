import SwiftUI

struct SidebarView: View {
    @EnvironmentObject var store: MailStore

    var body: some View {
        List(selection: $store.selectedAccount) {
            Section {
                ForEach(store.accounts) { account in
                    AccountRow(account: account)
                        .tag(account)
                        .swipeActions(edge: .trailing, allowsFullSwipe: false) {
                            Button(role: .destructive) {
                                Task { await store.deleteAccount(account) }
                            } label: {
                                Label("Delete", systemImage: "trash")
                            }
                        }
                }
            } header: {
                Text("Inboxes")
            }
        }
        .listStyle(.sidebar)
        .navigationTitle("Mail")
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Button {
                    Task { await store.createAccount() }
                } label: {
                    Label("New Account", systemImage: "plus.circle.fill")
                        .labelStyle(.iconOnly)
                        .font(.title2)
                }
            }
        }
        .onChange(of: store.selectedAccount) { account in
            guard let account else { return }
            Task { await store.loadEmails(for: account.id) }
        }
    }
}

struct AccountRow: View {
    @EnvironmentObject var store: MailStore
    let account: Account

    @State private var showRenameAlert = false
    @State private var pendingLabel = ""

    private var unreadCount: Int {
        store.emailsByAccount[account.id]?.filter { !$0.read }.count ?? 0
    }

    var body: some View {
        HStack {
            VStack(alignment: .leading, spacing: 2) {
                // #20 — show label when set, address otherwise
                Text(account.displayName)
                    .font(.system(.subheadline, design: account.label.isEmpty ? .monospaced : .default))
                    .lineLimit(1)
                    .minimumScaleFactor(0.7)

                // When a label is active, show the address as a secondary line
                if !account.label.isEmpty {
                    Text(account.address)
                        .font(.caption.monospaced())
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                        .minimumScaleFactor(0.8)
                } else {
                    Text("\(store.emailsByAccount[account.id]?.count ?? 0) message(s)")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
            Spacer()
            if unreadCount > 0 {
                Text("\(unreadCount)")
                    .font(.caption.bold())
                    .foregroundStyle(.white)
                    .padding(.horizontal, 6)
                    .padding(.vertical, 2)
                    .background(.blue, in: Capsule())
            }
        }
        .padding(.vertical, 2)
        // #17 — copy address; #20 — rename
        .contextMenu {
            Button {
                UIPasteboard.general.string = account.address
            } label: {
                Label("Copy Address", systemImage: "doc.on.doc")
            }

            Button {
                pendingLabel = account.label
                showRenameAlert = true
            } label: {
                Label("Rename", systemImage: "pencil")
            }

            if !account.label.isEmpty {
                Button {
                    store.renameAccount(account, label: "")
                } label: {
                    Label("Clear Label", systemImage: "xmark.circle")
                }
            }
        }
        // #20 — rename alert with text field
        .alert("Rename Account", isPresented: $showRenameAlert) {
            TextField("Label (optional)", text: $pendingLabel)
                .autocorrectionDisabled()
            Button("Save") { store.renameAccount(account, label: pendingLabel.trimmingCharacters(in: .whitespaces)) }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("Give this account a nickname. Leave blank to show the address.")
        }
    }
}
