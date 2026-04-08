import Foundation

final class APIClient {
    static let shared = APIClient()

    // Change to your server's address.
    var baseURL  = URL(string: "http://localhost:8080")!
    var wsBaseURL = "ws://localhost:8080"

    private let decoder: JSONDecoder = {
        let d = JSONDecoder()
        d.dateDecodingStrategy = .iso8601
        return d
    }()

    // MARK: - Accounts

    func createAccount() async throws -> Account {
        var req = URLRequest(url: baseURL.appendingPathComponent("accounts"))
        req.httpMethod = "POST"
        let (data, resp) = try await URLSession.shared.data(for: req)
        try validate(resp)
        let cr = try decoder.decode(CreateAccountResponse.self, from: data)
        return Account(id: cr.id, address: cr.address, token: cr.token)
    }

    func deleteAccount(_ id: String, token: String) async throws {
        var req = accountRequest("accounts/\(id)", method: "DELETE", token: token)
        let (_, resp) = try await URLSession.shared.data(for: req)
        try validate(resp)
    }

    // MARK: - Emails

    func listEmails(accountID: String, token: String, limit: Int = 50, before: Date? = nil) async throws -> EmailPage {
        var components = URLComponents(
            url: baseURL.appendingPathComponent("accounts/\(accountID)/emails"),
            resolvingAgainstBaseURL: false
        )!
        var queryItems = [URLQueryItem(name: "limit", value: "\(limit)")]
        if let before {
            let cursor = ISO8601DateFormatter().string(from: before)
            queryItems.append(URLQueryItem(name: "before", value: cursor))
        }
        components.queryItems = queryItems
        var req = URLRequest(url: components.url!)
        req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        let (data, resp) = try await URLSession.shared.data(for: req)
        try validate(resp)
        return try decoder.decode(EmailPage.self, from: data)
    }

    func getEmail(accountID: String, emailID: String, token: String) async throws -> Email {
        let req = accountRequest("accounts/\(accountID)/emails/\(emailID)", token: token)
        let (data, resp) = try await URLSession.shared.data(for: req)
        try validate(resp)
        return try decoder.decode(Email.self, from: data)
    }

    func deleteEmail(accountID: String, emailID: String, token: String) async throws {
        let req = accountRequest("accounts/\(accountID)/emails/\(emailID)", method: "DELETE", token: token)
        let (_, resp) = try await URLSession.shared.data(for: req)
        try validate(resp)
    }

    // MARK: - Attachments

    func listAttachments(accountID: String, emailID: String, token: String) async throws -> [AttachmentMeta] {
        let req = accountRequest("accounts/\(accountID)/emails/\(emailID)/attachments", token: token)
        let (data, resp) = try await URLSession.shared.data(for: req)
        try validate(resp)
        return try decoder.decode([AttachmentMeta].self, from: data)
    }

    /// Downloads an attachment and returns the raw bytes.
    func downloadAttachment(accountID: String, emailID: String, attachmentID: String, token: String) async throws -> Data {
        let req = accountRequest(
            "accounts/\(accountID)/emails/\(emailID)/attachments/\(attachmentID)",
            token: token
        )
        let (data, resp) = try await URLSession.shared.data(for: req)
        try validate(resp)
        return data
    }

    // MARK: - Device Token

    func registerDeviceToken(_ deviceToken: String, for accountID: String, token: String) async throws {
        var req = accountRequest("accounts/\(accountID)/device-token", method: "POST", token: token)
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        req.httpBody = try JSONEncoder().encode(["token": deviceToken])
        let (_, resp) = try await URLSession.shared.data(for: req)
        try validate(resp)
    }

    func removeDeviceToken(_ deviceToken: String, for accountID: String, token: String) async throws {
        var req = accountRequest("accounts/\(accountID)/device-token", method: "DELETE", token: token)
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        req.httpBody = try JSONEncoder().encode(["token": deviceToken])
        let (_, resp) = try await URLSession.shared.data(for: req)
        try validate(resp)
    }

    // MARK: - WebSocket

    // Token is passed as a query param because URLSessionWebSocketTask does
    // not support custom headers during the HTTP → WebSocket upgrade.
    func webSocketURL(for accountID: String, token: String) -> URL {
        URL(string: "\(wsBaseURL)/ws/\(accountID)?token=\(token)")!
    }

    // MARK: - Helpers

    private func accountRequest(_ path: String, method: String = "GET", token: String) -> URLRequest {
        var req = URLRequest(url: baseURL.appendingPathComponent(path))
        req.httpMethod = method
        req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        return req
    }

    private func validate(_ response: URLResponse) throws {
        guard let http = response as? HTTPURLResponse else { return }
        guard (200..<300).contains(http.statusCode) else {
            throw APIError.httpError(http.statusCode)
        }
    }
}

enum APIError: LocalizedError {
    case httpError(Int)

    var errorDescription: String? {
        switch self {
        case .httpError(let code): return "Server returned HTTP \(code)"
        }
    }
}
