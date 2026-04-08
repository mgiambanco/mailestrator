import SwiftUI
import UserNotifications

@main
struct MailClientApp: App {
    @UIApplicationDelegateAdaptor(AppDelegate.self) var appDelegate
    @StateObject private var store = MailStore()

    var body: some Scene {
        WindowGroup {
            ContentView()
                .environmentObject(store)
                .task {
                    await requestPushPermission()
                    await store.refreshAll()
                }
                .onReceive(NotificationCenter.default.publisher(for: .deviceTokenReceived)) { notif in
                    if let token = notif.object as? String {
                        store.setDeviceToken(token)
                    }
                }
                .onReceive(NotificationCenter.default.publisher(for: .openEmail)) { notif in
                    guard
                        let info = notif.userInfo,
                        let accountID = info["account_id"] as? String,
                        let emailID = info["email_id"] as? String
                    else { return }
                    if let account = store.accounts.first(where: { $0.id == accountID }) {
                        store.selectedAccount = account
                        Task {
                            if let email = try? await APIClient.shared.getEmail(
                                accountID: accountID, emailID: emailID, token: account.token) {
                                store.selectedEmail = email
                            }
                        }
                    }
                }
        }
    }

    private func requestPushPermission() async {
        let center = UNUserNotificationCenter.current()
        let granted = try? await center.requestAuthorization(options: [.alert, .sound, .badge])
        if granted == true {
            await MainActor.run {
                UIApplication.shared.registerForRemoteNotifications()
            }
        }
    }
}
