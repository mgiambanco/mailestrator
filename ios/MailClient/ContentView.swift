import SwiftUI

struct ContentView: View {
    @EnvironmentObject var store: MailStore

    var body: some View {
        NavigationSplitView {
            SidebarView()
        } content: {
            InboxView()
        } detail: {
            EmailDetailView()
        }
        .navigationSplitViewStyle(.balanced)
    }
}
