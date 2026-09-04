package kql

import (
	"regexp"
	"strconv"
	"strings"
)

// fingerprintStep is one SQL-text masking pass. Every step is expressed as an
// RE2 regex so the exact same definitions run in Go (tests, offline grouping)
// and in KQL (replace_regex), keeping server-side and local fingerprints identical.
type fingerprintStep struct {
	pattern     string
	replacement string
}

// sqlFingerprintSteps normalizes SQL text into a stable query-shape
// fingerprint, following the conventions of pt-query-digest and pgBadger.
// Order matters: comments first (they may contain quotes/numbers), then
// literals, then placeholders, then whitespace, then casing.
var sqlFingerprintSteps = []fingerprintStep{
	// 1. /* */ block comments: sqlcommenter / driver trace hints vary per request
	{`(?s)/\*.*?\*/`, " "},
	// 2. -- line comments
	{`--[^\r\n]*`, " "},
	// 3. hex literals (0x1F vs 31 must not split a group)
	{`0x[0-9a-fA-F]+`, "?"},
	// 4. single-quoted strings, handling '' doubling and \' escaping
	{`'(?:[^'\\]|\\.|'')*'`, "'?'"},
	// 5. double-quoted strings/identifiers
	{`"(?:[^"\\]|\\.)*"`, `"?"`},
	// 6. numeric literals, including negatives
	{`-?\b\d+(\.\d+)?\b`, "?"},
	// 7. comma-separated placeholder runs collapsed: IN (?, ?, ?) and
	//    VALUES (?, ?) tuples stop splitting groups by batch length
	{`\?(?:\s*,\s*\?)+`, "?"},
	// 8. surrounding whitespace trimmed
	{`^\s+|\s+$`, ""},
	// 9. inner whitespace collapsed
	{`\s+`, " "},
}

var sqlFingerprintRegexps = compileFingerprintSteps(sqlFingerprintSteps)

func compileFingerprintSteps(steps []fingerprintStep) []*regexp.Regexp {
	regexps := make([]*regexp.Regexp, len(steps))
	for i, step := range steps {
		regexps[i] = regexp.MustCompile(step.pattern)
	}
	return regexps
}

// SQLFingerprint normalizes SQL text into the query-shape fingerprint used for
// grouping. It applies the same RE2 passes the grouped KQL query applies
// server-side, byte for byte, and is the reference implementation for tests.
func SQLFingerprint(sql string) string {
	s := strings.TrimSpace(sql)
	for i, re := range sqlFingerprintRegexps {
		s = re.ReplaceAllString(s, sqlFingerprintSteps[i].replacement)
	}
	return strings.ToLower(s)
}

// kqlVerbatim renders a Go string as a KQL verbatim string literal (@"..."),
// doubling embedded quotes as KQL requires
func kqlVerbatim(s string) string {
	return "@" + `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// buildFingerprintExtends renders the KQL extend pipeline for SqlFingerprint
// from the shared step definitions, so the grouped slow-logs query can never
// drift from the Go reference implementation
func buildFingerprintExtends() string {
	var sb strings.Builder
	for i, step := range sqlFingerprintSteps {
		sb.WriteString("| extend F")
		sb.WriteString(strconv.Itoa(i + 1))
		sb.WriteString(" = replace_regex(F")
		sb.WriteString(strconv.Itoa(i))
		sb.WriteString(", ")
		sb.WriteString(kqlVerbatim(step.pattern))
		sb.WriteString(", ")
		sb.WriteString(kqlVerbatim(step.replacement))
		sb.WriteString(")\n")
	}
	return sb.String()
}
