import Foundation

/// Small regex helpers over NSRegularExpression. The contract/outline parsers
/// build patterns from interpolated keys and need multiline anchors; runtime
/// regex keeps that readable and avoids Swift literal-interpolation pitfalls.
enum Rx {
    /// First match anywhere; returns capture groups (index 0 = whole match).
    /// `.anchorsMatchLines` makes ^/$ match line boundaries, like JS /m.
    /// ICU's `$` also matches before the final line terminator (JS does not),
    /// which would truncate a `(.+)$` capture on the last line — so a single
    /// trailing newline is removed before matching.
    static func first(_ pattern: String, in text: String, multiline: Bool = true) -> [String]? {
        var subject = text
        if subject.hasSuffix("\n") { subject.removeLast() }
        guard let regex = try? NSRegularExpression(
            pattern: pattern,
            options: multiline ? [.anchorsMatchLines] : []
        ) else { return nil }
        let range = NSRange(subject.startIndex..., in: subject)
        guard let match = regex.firstMatch(in: subject, range: range) else { return nil }
        return captures(of: match, in: subject)
    }

    /// Whole-string match (like Regex.wholeMatch). The pattern is wrapped so a
    /// lazy quantifier cannot leave the first match short of the full range.
    static func whole(_ pattern: String, in text: String) -> [String]? {
        guard let regex = try? NSRegularExpression(pattern: "^(?:" + pattern + ")$") else { return nil }
        let range = NSRange(text.startIndex..., in: text)
        guard let match = regex.firstMatch(in: text, range: range), match.range == range else { return nil }
        return captures(of: match, in: text)
    }

    static func matches(_ pattern: String, in text: String, multiline: Bool = true) -> Bool {
        first(pattern, in: text, multiline: multiline) != nil
    }

    private static func captures(of match: NSTextCheckingResult, in text: String) -> [String] {
        (0..<match.numberOfRanges).map { index in
            let range = match.range(at: index)
            guard range.location != NSNotFound, let swiftRange = Range(range, in: text) else { return "" }
            return String(text[swiftRange])
        }
    }

    /// Escapes a literal key for interpolation into a pattern.
    static func escape(_ text: String) -> String {
        NSRegularExpression.escapedPattern(for: text)
    }
}