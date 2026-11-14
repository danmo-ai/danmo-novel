import Foundation

struct BookSummary: Decodable, Identifiable, Hashable {
    let id: String
    let title: String
    let genre: String
    let stage: String
    let unitCount: Int
    let reviewedCount: Int
    let openCandidates: Int
    let readOnly: Bool

    var progress: Double {
        unitCount == 0 ? 0 : Double(reviewedCount) / Double(unitCount)
    }
}

struct UnitInfo: Decodable, Identifiable, Hashable {
    let unitId: String
    let volume: Int
    let index: Int
    let status: String
    let title: String
    let chapterFrom: Int
    let chapterTo: Int
    let hasProse: Bool
    let prosePath: String
    let outlineRaw: String?

    var id: String { unitId }
    var statusTitle: String {
        switch status {
        case "reviewed": "已定稿"
        case "drafted": "待审校"
        case "accepted": "待写正文"
        case "proposed": "待写细纲"
        default: status
        }
    }
}

struct BookDetail: Decodable, Hashable {
    let summary: BookSummary
    let units: [UnitInfo]
    let revision: String
}

struct BookResponse: Decodable { let book: BookDetail }
struct DemoBookResponse: Decodable { let book: BookDetail }
struct BookFile: Decodable { let path: String; let content: String }

struct NewBookDraft: Encodable {
    let bookId: String
    let title: String
    let genre: String
    let idea: String
}

struct Chapter: Identifiable, Hashable {
    let number: Int
    let title: String
    let body: String
    var id: Int { number }
    var wordCount: Int { body.unicodeScalars.filter { !CharacterSet.whitespacesAndNewlines.contains($0) }.count }

    static func parse(_ markdown: String) -> [Chapter] {
        let lines = markdown.replacingOccurrences(of: "\r\n", with: "\n").split(separator: "\n", omittingEmptySubsequences: false)
        var result: [Chapter] = []
        var number: Int?
        var title = ""
        var body: [Substring] = []
        func flush() {
            guard let number else { return }
            result.append(Chapter(number: number, title: title, body: body.joined(separator: "\n").trimmingCharacters(in: .whitespacesAndNewlines)))
        }
        for line in lines {
            if let match = try? /^##\s*第\s*(\d+)\s*章\s*(.*)$/.wholeMatch(in: line) {
                flush()
                number = Int(match.1)
                title = String(match.2).trimmingCharacters(in: .whitespaces)
                body = []
            } else if number != nil, line.trimmingCharacters(in: .whitespaces) != "---" {
                body.append(line)
            }
        }
        flush()
        return result
    }
}

struct Provider: Decodable, Identifiable {
    let id: String
    let name: String
    let provider: String
    let baseUrl: String?
    let hasApiKey: Bool
    let models: [ProviderModel]
}

struct ProviderModel: Codable {
    let name: String
    let enabled: Bool
    let protocolDialect: String?

    init(name: String, enabled: Bool = true, protocolDialect: String? = nil) {
        self.name = name
        self.enabled = enabled
        self.protocolDialect = protocolDialect
    }
}

struct ProviderRequest: Encodable {
    let provider: String
    let name: String
    let apiKey: String
    let baseUrl: String?
    let defaultProtocolDialect: String?
    let models: [ProviderModel]
}

struct RunRecord: Decodable, Identifiable {
    let id: String
    let bookId: String
    let kind: String
    let status: String
    let modelId: String
    let callsUsed: Int
    let error: String?
    let createdAt: String

    var statusTitle: String {
        ["succeeded": "成功", "failed": "失败", "running": "运行中", "waiting_accept": "待确认", "cancelled": "已取消"][status] ?? status
    }
}
