package reporter

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// headerAliases maps normalized (lowercase, separators removed) column
// identifiers to friendlier display titles
var headerAliases = map[string]string{
	"timestamp":         "Time",
	"timegenerated":     "Time",
	"operationname":     "Operation",
	"operationid":       "Operation ID",
	"cloudrolename":     "Role",
	"cloudroleinstance": "Instance",
	"problemid":         "Problem ID",
	"sqltext":           "SQL Text",
	"querydurationms":   "Query Duration (ms)",
	"queryhash":         "Query Hash",
}

// headerWordMap normalizes well-known tokens inside generated headers
var headerWordMap = map[string]string{
	"id":    "ID",
	"ids":   "IDs",
	"url":   "URL",
	"uri":   "URI",
	"sql":   "SQL",
	"api":   "API",
	"apis":  "APIs",
	"http":  "HTTP",
	"https": "HTTPS",
	"grpc":  "gRPC",
	"db":    "DB",
	"ip":    "IP",
	"uuid":  "UUID",
	"pct":   "%",
	"cpu":   "CPU",
	"rps":   "RPS",
}

// camelBoundary splits identifiers at lower/upper transitions and separators
var camelBoundary = regexp.MustCompile(`([a-z0-9])([A-Z])`)

// humanizeHeader converts raw KQL/column identifiers into readable titles:
// "TotalCalls" → "Total Calls", "TimeGenerated" → "Time",
// "QueryDurationMs" → "Query Duration (ms)", "operation_Name" → "Operation"
func humanizeHeader(raw string) string {
	key := strings.ToLower(strings.TrimSpace(raw))
	key = strings.ReplaceAll(key, "_", "")
	key = strings.ReplaceAll(key, ".", "")
	if alias, ok := headerAliases[key]; ok {
		return alias
	}

	normalized := camelBoundary.ReplaceAllString(strings.TrimSpace(raw), "$1 $2")
	parts := strings.FieldsFunc(normalized, func(r rune) bool {
		return r == '_' || r == '.' || r == ' ' || r == '-'
	})
	if len(parts) == 0 {
		return raw
	}

	words := make([]string, 0, len(parts))
	for i, p := range parts {
		lower := strings.ToLower(p)
		if lower == "ms" {
			if i == len(parts)-1 {
				words = append(words, "(ms)")
			} else {
				words = append(words, "Ms")
			}
			continue
		}
		if known, ok := headerWordMap[lower]; ok {
			words = append(words, known)
			continue
		}
		if upper := strings.ToUpper(p); p == upper && len(p) > 1 {
			words = append(words, upper)
			continue
		}
		words = append(words, strings.ToUpper(p[:1])+lower[1:])
	}
	return strings.Join(words, " ")
}

// timestampFormats covers the Azure Monitor timestamp shapes seen in CLI output
var timestampFormats = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05.999999999Z0700",
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02 15:04:05",
}

var timestampishRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}[Tt ]\d{2}:\d{2}:\d{2}`)

// parseTimestamp parses a string timestamp across the known Azure formats
func parseTimestamp(s string) (time.Time, bool) {
	for _, f := range timestampFormats {
		if t, err := time.Parse(f, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

var (
	msHeaderRe  = regexp.MustCompile(`(?i)(duration|latency|elapsed|_ms$|ms_|^ms)`)
	secHeaderRe = regexp.MustCompile(`(?i)(duration_s$|_s$|seconds?)`)
	pctHeaderRe = regexp.MustCompile(`(?i)(pct|percent|rate|^%|_%$)`)
)

// normalizeCell renders a raw query cell in a compact, human-friendly form:
// timestamps in local time, numbers with thousands separators, durations
// humanized, KQL arrays joined, and NULLs collapsed to "-"
func normalizeCell(header string, v interface{}) string {
	switch val := v.(type) {
	case nil:
		return "-"
	case []interface{}:
		if len(val) == 0 {
			return "-"
		}
		parts := make([]string, 0, len(val))
		for _, item := range val {
			parts = append(parts, normalizeCell("", item))
		}
		return strings.Join(parts, ", ")
	case map[string]interface{}:
		if len(val) == 0 {
			return "-"
		}
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%s=%s", k, normalizeCell("", val[k])))
		}
		return strings.Join(parts, ", ")
	case bool:
		return strconv.FormatBool(val)
	case string:
		s := strings.TrimSpace(val)
		if timestampishRe.MatchString(s) {
			if t, ok := parseTimestamp(s); ok {
				return t.Local().Format("2006-01-02 15:04:05")
			}
		}
		s = strings.ReplaceAll(s, "\n", " ")
		s = strings.ReplaceAll(s, "\r", " ")
		return strings.TrimSpace(s)
	case float64:
		return formatMetricValue(header, val)
	case int:
		return formatMetricValue(header, float64(val))
	case int64:
		return formatMetricValue(header, float64(val))
	case time.Time:
		if val.IsZero() {
			return "-"
		}
		return val.Local().Format("2006-01-02 15:04:05")
	default:
		return fmt.Sprintf("%v", v)
	}
}

// formatMetricValue formats a numeric value guided by the column header:
// duration-like headers become human durations, percent-like headers get a
// "%" suffix, and count-like headers get thousands separators
func formatMetricValue(header string, v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return "-"
	}
	if secHeaderRe.MatchString(header) {
		return formatLatencyHuman(v * 1000.0)
	}
	if msHeaderRe.MatchString(header) {
		return formatLatencyHuman(v)
	}
	if pctHeaderRe.MatchString(header) {
		return trimFloat(v) + "%"
	}
	if v == float64(int64(v)) {
		return formatNumber(int64(v))
	}
	return trimFloat(v)
}

// trimFloat renders a float with up to 2 decimals, dropping trailing zeros
func trimFloat(v float64) string {
	s := fmt.Sprintf("%.2f", v)
	s = strings.TrimRight(s, "0")
	return strings.TrimRight(s, ".")
}

// markdownCell normalizes a cell and escapes characters that break Markdown tables
func markdownCell(header string, v interface{}) string {
	s := normalizeCell(header, v)
	s = strings.ReplaceAll(s, "|", "\\|")
	return s
}
