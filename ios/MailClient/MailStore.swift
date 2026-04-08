import Foundation
import Combine
import Security
import UserNotifications

@MainActor
final class MailStore: ObservableObject {
    @Published var accounts: [Account] = []
    @Published var emailsByAccount: [String: [Email]] = [:]
    @Published var selectedAccount: Account?
    @Published var selectedEmail: Email?

    private var wsSessions: [String: URLSessionWebSocketTask] = [:]
    private var deviceToken: String?

    // Cursor for next page per account: the received_at of the oldest loaded email.
    private var nextCursorByAccount: [String: Date] = [:]
    // Whether more pages exist per account.
    @Published var hasMoreByAccount: [String: Bool] = [:]
    @Published var loadingMoreByAccount: [String: Bool] = [:]

    // Keychain service/account keys for persisting account list.
    // Account IDs act as bearer credentials — Keychain protects them at rest.
    private let keychainService = "com.mailclient.accounts"
    private let keychainAccount = "account_list"

    init() {
        loadPersistedAccounts()
    }

    // Total unread across all accounts — drives the app icon badge.
    var totalUnreadCount: Int {
        emailsByAccount.values.reduce(0) { $0 + $1.filter { !$0.read }.count }
    }

    // MARK: - Account management

    func createAccount() async {
        do {
            var account = try await APIClient.shared.createAccount()
            // Register APNs device token immediately if we have one.
            if let dt = deviceToken {
                try? await APIClient.shared.registerDeviceToken(dt, for: account.id, token: account.token)
                account.deviceTokenRegistered = true
            }
            accounts.append(account)
            emailsByAccount[account.id] = []
            persistAccounts()
            connectWebSocket(for: account)
            if selectedAccount == nil { selectedAccount = account }
        } catch {
            print("createAccount error: \(error)")
        }
    }

    func deleteAccount(_ account: Account) async {
        disconnectWebSocket(for: account.id)
        try? await APIClient.shared.deleteAccount(account.id, token: account.token)
        accounts.removeAll { $0.id == account.id }
        emailsByAccount.removeValue(forKey: account.id)
        persistAccounts()
        if selectedAccount?.id == account.id {
            selectedAccount = accounts.first
        }
    }

    // MARK: - Email loading

    private func token(for accountID: String) -> String {
        accounts.first(where: { $0.id == accountID })?.token ?? ""
    }

    // Loads the first page, replacing any existing emails for this account.
    func loadEmails(for accountID: String) async {
        do {
            let page = try await APIClient.shared.listEmails(
                accountID: accountID, token: token(for: accountID))
            emailsByAccount[accountID] = page.emails
            hasMoreByAccount[accountID] = page.has_more
            nextCursorByAccount[accountID] = page.emails.last?.received_at
            updateBadge()
        } catch {
            print("loadEmails error: \(error)")
        }
    }

    // Appends the next page to the existing list.
    func loadMoreEmails(for accountID: String) async {
        guard hasMoreByAccount[accountID] == true,
              loadingMoreByAccount[accountID] != true,
              let cursor = nextCursorByAccount[accountID] else { return }

        loadingMoreByAccount[accountID] = true
        defer { loadingMoreByAccount[accountID] = false }

        do {
            let page = try await APIClient.shared.listEmails(
                accountID: accountID, token: token(for: accountID), before: cursor)
            emailsByAccount[accountID, default: []].append(contentsOf: page.emails)
            hasMoreByAccount[accountID] = page.has_more
            nextCursorByAccount[accountID] = page.emails.last?.received_at
            updateBadge()
        } catch {
            print("loadMoreEmails error: \(error)")
        }
    }

    func deleteEmail(_ email: Email) async {
        try? await APIClient.shared.deleteEmail(
            accountID: email.account_id, emailID: email.id,
            token: token(for: email.account_id))
        emailsByAccount[email.account_id]?.removeAll { $0.id == email.id }
        if selectedEmail?.id == email.id { selectedEmail = nil }
        updateBadge()
    }

    // MARK: - Account labels (#20)

    func renameAccount(_ account: Account, label: String) {
        guard let idx = accounts.firstIndex(where: { $0.id == account.id }) else { return }
        accounts[idx].label = label
        persistAccounts()
    }

    // MARK: - Badge (#18)

    func updateBadge() {
        let count = totalUnreadCount
        UNUserNotificationCenter.current().setBadgeCount(count) { _ in }
    }

    // MARK: - Device token

    func setDeviceToken(_ dt: String) {
        deviceToken = dt
        Task {
            for account in accounts where !account.deviceTokenRegistered {
                try? await APIClient.shared.registerDeviceToken(dt, for: account.id, token: account.token)
                if let idx = accounts.firstIndex(where: { $0.id == account.id }) {
                    accounts[idx].deviceTokenRegistered = true
                }
            }
            persistAccounts()
        }
    }

    // MARK: - WebSocket

    func connectAllWebSockets() {
        for account in accounts {
            connectWebSocket(for: account)
        }
    }

    private func connectWebSocket(for account: Account) {
        let url = APIClient.shared.webSocketURL(for: account.id, token: account.token)
        let task = URLSession.shared.webSocketTask(with: url)
        wsSessions[account.id] = task
        task.resume()
        listenWebSocket(task: task, accountID: account.id)
        // URLSessionWebSocketTask auto-replies to server pings with pongs.
        // We also send a client-side ping every 25s so we detect a dead
        // server before the next message is due.
        schedulePing(task: task, accountID: account.id)
    }

    private func disconnectWebSocket(for accountID: String) {
        wsSessions[accountID]?.cancel(with: .goingAway, reason: nil)
        wsSessions.removeValue(forKey: accountID)
    }

    // Sends a ping and schedules the next one. If the ping fails the
    // connection is dead — reconnect immediately (the reconnect path itself
    // uses exponential backoff, handled in listenWebSocket).
    private func schedulePing(task: URLSessionWebSocketTask, accountID: String) {
        DispatchQueue.main.asyncAfter(deadline: .now() + 25) { [weak self] in
            guard let self,
                  // Make sure this task is still the active one for this account.
                  let active = self.wsSessions[accountID], active === task else { return }
            task.sendPing { error in
                Task { @MainActor in
                    if error != nil {
                        // Server unreachable — trigger reconnect via the same
                        // exponential-backoff path used by listenWebSocket.
                        self.reconnectWebSocket(accountID: accountID, attempt: 0)
                    } else {
                        self.schedulePing(task: task, accountID: accountID)
                    }
                }
            }
        }
    }

    private func listenWebSocket(task: URLSessionWebSocketTask, accountID: String) {
        task.receive { [weak self] result in
            guard let self else { return }
            switch result {
            case .failure:
                Task { @MainActor in
                    self.reconnectWebSocket(accountID: accountID, attempt: 0)
                }
            case .success(let message):
                if case .string(let text) = message,
                   let data = text.data(using: .utf8) {
                    let decoder = JSONDecoder()
                    decoder.dateDecodingStrategy = .iso8601
                    if let event = try? decoder.decode(WebSocketEvent.self, from: data),
                       event.type == "new_email",
                       let email = event.email {
                        Task { @MainActor in
                            self.emailsByAccount[accountID]?.insert(email, at: 0)
                            self.updateBadge()
                        }
                    }
                }
                self.listenWebSocket(task: task, accountID: accountID)
            }
        }
    }

    // Reconnects with exponential backoff: 3s, 6s, 12s, 24s, … capped at 60s.
    private func reconnectWebSocket(accountID: String, attempt: Int) {
        let delay = min(3.0 * pow(2.0, Double(attempt)), 60.0)
        DispatchQueue.main.asyncAfter(deadline: .now() + delay) {
            Task { @MainActor in
                guard let account = self.accounts.first(where: { $0.id == accountID }) else { return }
                self.connectWebSocket(for: account)
            }
        }
    }

    // MARK: - Persistence (Keychain)
    // Account IDs are bearer credentials; Keychain encrypts them at rest
    // and excludes them from unencrypted backups.

    private func persistAccounts() {
        guard let data = try? JSONEncoder().encode(accounts) else { return }

        let query: [CFString: Any] = [
            kSecClass:       kSecClassGenericPassword,
            kSecAttrService: keychainService,
            kSecAttrAccount: keychainAccount,
        ]
        let attrs: [CFString: Any] = [kSecValueData: data]

        let status = SecItemUpdate(query as CFDictionary, attrs as CFDictionary)
        if status == errSecItemNotFound {
            var add = query
            add[kSecValueData] = data
            SecItemAdd(add as CFDictionary, nil)
        }
    }

    private func loadPersistedAccounts() {
        let query: [CFString: Any] = [
            kSecClass:            kSecClassGenericPassword,
            kSecAttrService:      keychainService,
            kSecAttrAccount:      keychainAccount,
            kSecReturnData:       true,
            kSecMatchLimit:       kSecMatchLimitOne,
        ]
        var result: AnyObject?
        guard SecItemCopyMatching(query as CFDictionary, &result) == errSecSuccess,
              let data = result as? Data,
              let saved = try? JSONDecoder().decode([Account].self, from: data) else { return }

        accounts = saved
        for account in accounts {
            emailsByAccount[account.id] = []
        }
        selectedAccount = accounts.first
    }

    // Call this after app launch to refresh all inboxes.
    func refreshAll() async {
        await withTaskGroup(of: Void.self) { group in
            for account in accounts {
                let id = account.id
                group.addTask { await self.loadEmails(for: id) }
            }
        }
        connectAllWebSockets()
    }
}
