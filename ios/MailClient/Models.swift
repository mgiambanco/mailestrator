import Foundation

struct Account: Identifiable, Codable, Hashable {
    let id: String
    let address: String

    // Local-only fields — never sent to the server.
    var token: String = ""              // bearer credential, stored in Keychain
    var deviceTokenRegistered: Bool = false
    var label: String = ""              // user-assigned nickname, displayed in sidebar

    /// The name shown in the sidebar: label if set, otherwise the email address.
    var displayName: String { label.isEmpty ? address : label }

    enum CodingKeys: String, CodingKey {
        case id, address, token, deviceTokenRegistered, label
    }
}

struct AttachmentMeta: Identifiable, Codable {
    let id: String
    let email_id: String
    let filename: String
    let content_type: String
    let size: Int
}

struct Email: Identifiable, Codable {
    let id: String
    let account_id: String
    let from: String
    let subject: String
    let body_text: String
    let body_html: String
    let received_at: Date
    var read: Bool
    let attachment_count: Int
    let attachments: [AttachmentMeta]?
}

struct CreateAccountResponse: Codable {
    let id: String
    let address: String
    let token: String  // returned once at creation; client must persist
}

struct EmailPage: Codable {
    let emails: [Email]
    let has_more: Bool
}

struct WebSocketEvent: Codable {
    let type: String
    let email: Email?
}
