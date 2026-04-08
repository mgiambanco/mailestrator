import SwiftUI
import QuickLook
import UniformTypeIdentifiers

// MARK: - AttachmentRow

/// A single row in the attachment list. Tapping it downloads the file and
/// opens it in QuickLook.
struct AttachmentRow: View {
    let meta: AttachmentMeta
    let accountID: String
    let emailID: String
    let token: String

    @State private var previewURL: URL?
    @State private var isLoading = false
    @State private var error: String?

    var body: some View {
        Button(action: download) {
            HStack(spacing: 12) {
                Image(systemName: iconName(for: meta.content_type))
                    .font(.title3)
                    .foregroundStyle(.secondary)
                    .frame(width: 32)

                VStack(alignment: .leading, spacing: 2) {
                    Text(meta.filename)
                        .font(.subheadline)
                        .lineLimit(1)
                    Text(formattedSize(meta.size))
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }

                Spacer()

                if isLoading {
                    ProgressView()
                        .controlSize(.small)
                } else {
                    Image(systemName: "arrow.down.circle")
                        .foregroundStyle(.blue)
                }
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .quickLookPreview($previewURL)
        .alert("Download failed", isPresented: .constant(error != nil), actions: {
            Button("OK") { error = nil }
        }, message: {
            Text(error ?? "")
        })
    }

    private func download() {
        guard !isLoading else { return }
        isLoading = true
        Task {
            do {
                let data = try await APIClient.shared.downloadAttachment(
                    accountID: accountID,
                    emailID: emailID,
                    attachmentID: meta.id,
                    token: token
                )
                let url = try writeTempFile(data: data, filename: meta.filename)
                await MainActor.run {
                    previewURL = url
                    isLoading = false
                }
            } catch {
                await MainActor.run {
                    self.error = error.localizedDescription
                    isLoading = false
                }
            }
        }
    }

    /// Writes bytes to a temporary file with the correct extension so
    /// QuickLook picks the right viewer.
    private func writeTempFile(data: Data, filename: String) throws -> URL {
        let dir = FileManager.default.temporaryDirectory
            .appendingPathComponent("attachments", isDirectory: true)
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        let dest = dir.appendingPathComponent(filename)
        try data.write(to: dest)
        return dest
    }
}

// MARK: - AttachmentListView

/// Displays the attachment section inside an email detail view.
struct AttachmentListView: View {
    let attachments: [AttachmentMeta]
    let accountID: String
    let emailID: String
    let token: String

    var body: some View {
        if !attachments.isEmpty {
            VStack(alignment: .leading, spacing: 4) {
                Label("Attachments", systemImage: "paperclip")
                    .font(.caption.weight(.semibold))
                    .foregroundStyle(.secondary)
                    .padding(.bottom, 2)

                ForEach(attachments) { att in
                    AttachmentRow(
                        meta: att,
                        accountID: accountID,
                        emailID: emailID,
                        token: token
                    )
                    .padding(.vertical, 4)
                    if att.id != attachments.last?.id {
                        Divider()
                    }
                }
            }
            .padding()
            .background(.quaternary, in: RoundedRectangle(cornerRadius: 10))
        }
    }
}

// MARK: - Helpers

private func iconName(for contentType: String) -> String {
    switch true {
    case contentType.hasPrefix("image/"):            return "photo"
    case contentType == "application/pdf":           return "doc.richtext"
    case contentType.hasPrefix("audio/"):            return "waveform"
    case contentType.hasPrefix("video/"):            return "play.rectangle"
    case contentType.contains("zip"),
         contentType.contains("compressed"):        return "archivebox"
    case contentType.hasPrefix("text/"):             return "doc.text"
    case contentType.contains("spreadsheet"),
         contentType.contains("excel"):             return "tablecells"
    case contentType.contains("presentation"),
         contentType.contains("powerpoint"):        return "play.square"
    default:                                         return "doc"
    }
}

private func formattedSize(_ bytes: Int) -> String {
    let formatter = ByteCountFormatter()
    formatter.allowedUnits = [.useKB, .useMB]
    formatter.countStyle = .file
    return formatter.string(fromByteCount: Int64(bytes))
}
