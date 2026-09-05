package reporter

import (
	"fmt"
	"io"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/fatih/color"
	"github.com/mattn/go-runewidth"

	"github.com/denjamio/azlens/pkg/model"
)

// Column alignment semantics for the modern table renderer
const (
	AlignLeft  = 0
	AlignRight = 1
)

// forceTerminalWidth, when > 0, overrides terminal width detection (tests)
var forceTerminalWidth int

var ansiPattern = regexp.MustCompile("\x1b\\[[0-9;]*m")

// stripANSI removes SGR color escape sequences so width math stays accurate
func stripANSI(s string) string {
	return ansiPattern.ReplaceAllString(s, "")
}

// displayWidth reports the printable width of a cell, ignoring ANSI escapes
func displayWidth(s string) int {
	return runewidth.StringWidth(stripANSI(s))
}

// effectiveTableWidth resolves the rendering budget: test override > $COLUMNS >
// terminal size probe > 0 (unlimited, e.g. piped or redirected output)
func effectiveTableWidth() int {
	if forceTerminalWidth > 0 {
		return forceTerminalWidth
	}
	if v := strings.TrimSpace(os.Getenv("COLUMNS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return terminalWidth(os.Stdout)
}

var numericCellPattern = regexp.MustCompile(`^[-+~]?\d[\d,]*(?:\.\d+)?(?:%|ms|s|µs|ns|x)?$|^[-+]?\d+m\d{2}s$|^[-+]?\d+h\d{2}m$`)

// isNumericCell reports whether a formatted cell looks like a measurement
// ("1,234", "0.53%", "812ms", "1.24s", "2m05s") for auto right-alignment
func isNumericCell(s string) bool {
	s = strings.TrimSpace(stripANSI(s))
	if s == "" || s == "-" {
		return false
	}
	return numericCellPattern.MatchString(s)
}

// CellColorFunc returns the color for a data cell, or nil to keep it plain.
// It receives the zero-based row index (data rows only, not the header),
// column index, and the (already truncated) raw cell value.
type CellColorFunc func(row, col int, cell string) *color.Color

// modernTable is a terminal-width-aware UTF-8 table renderer with rounded
// borders, cyan headers, dim borders, numeric right-alignment, and width
// budgeting that shrinks flexible text columns to fit narrow screens.
type modernTable struct {
	w           io.Writer
	headers     []string
	alignments  []int
	autoAlign   bool
	rows        [][]string
	cellCap     int
	colorCell   CellColorFunc
	headerColor *color.Color
	borderColor *color.Color
}

// NewTable creates a table for w. When alignments is nil (or its length
// does not match headers), numeric columns are right-aligned automatically.
// NewTable creates a modern UTF-8 table for w. When alignments is nil (or its
// length does not match headers), numeric columns are right-aligned automatically.
func NewTable(w io.Writer, headers []string, alignments []int) *modernTable {
	t := &modernTable{
		w:           w,
		headers:     headers,
		headerColor: color.New(color.FgCyan, color.Bold),
		borderColor: color.New(color.FgHiBlack),
	}
	if len(alignments) == len(headers) {
		t.alignments = append(t.alignments, alignments...)
	} else {
		t.autoAlign = true
		t.alignments = make([]int, len(headers))
	}
	return t
}

// SetCellCap limits the natural width of every column; longer cells are
// truncated with an ellipsis. Headers are never truncated.
func (t *modernTable) SetCellCap(n int) *modernTable {
	t.cellCap = n
	return t
}

// SetCellColor registers a per-cell colorizer (see CellColorFunc)
func (t *modernTable) SetCellColor(fn CellColorFunc) *modernTable {
	t.colorCell = fn
	return t
}

// Append adds a data row; short rows are padded and newlines collapsed
func (t *modernTable) Append(row []string) {
	cleaned := make([]string, len(t.headers))
	for i := range t.headers {
		v := ""
		if i < len(row) {
			v = row[i]
		}
		v = strings.ReplaceAll(v, "\n", " ")
		v = strings.ReplaceAll(v, "\r", " ")
		cleaned[i] = strings.TrimSpace(v)
	}
	t.rows = append(t.rows, cleaned)
}

// Render draws the table: top frame, header, separator, data rows, bottom frame
func (t *modernTable) Render() {
	if len(t.headers) == 0 {
		return
	}
	widths := t.naturalWidths()
	t.fitWidths(widths)
	t.renderFrame(widths, "╭", "┬", "╮")
	t.renderRow(-1, widths, t.headers, true)
	t.renderFrame(widths, "├", "┼", "┤")
	for r, row := range t.rows {
		t.renderRow(r, widths, row, false)
	}
	t.renderFrame(widths, "╰", "┴", "╯")
}

// naturalWidths computes per-column widths from headers and cells, applying the
// cell cap and auto-detecting numeric (right-aligned) columns when unset
func (t *modernTable) naturalWidths() []int {
	widths := make([]int, len(t.headers))
	for i, h := range t.headers {
		widths[i] = displayWidth(h)
	}
	for _, row := range t.rows {
		for i := range t.headers {
			if i >= len(row) {
				continue
			}
			w := displayWidth(row[i])
			if t.cellCap > 0 && w > t.cellCap {
				w = t.cellCap
			}
			if w > widths[i] {
				widths[i] = w
			}
		}
	}
	if t.autoAlign {
		for i := range t.headers {
			allNumeric := len(t.rows) > 0
			for _, row := range t.rows {
				cell := ""
				if i < len(row) {
					cell = row[i]
				}
				if cell != "" && cell != "-" && !isNumericCell(cell) {
					allNumeric = false
					break
				}
			}
			if allNumeric {
				t.alignments[i] = AlignRight
			}
		}
	}
	return widths
}

// fitWidths shrinks flexible (left-aligned) columns proportionally so the table
// fits the terminal budget. Numeric columns and headers are never truncated;
// when the budget cannot be met the table overflows gracefully.
func (t *modernTable) fitWidths(widths []int) {
	maxw := effectiveTableWidth()
	total := len(widths) + 1
	for _, w := range widths {
		total += w + 2
	}
	if maxw <= 0 || total <= maxw {
		return
	}
	excess := total - maxw

	floors := make([]int, len(widths))
	flex := make([]int, 0, len(widths))
	for i := range widths {
		if t.alignments[i] == AlignRight {
			floors[i] = widths[i]
			continue
		}
		floors[i] = displayWidth(t.headers[i])
		if floors[i] < 6 {
			floors[i] = 6
		}
		flex = append(flex, i)
	}

	for excess > 0 {
		slack := 0
		for _, i := range flex {
			slack += widths[i] - floors[i]
		}
		if slack <= 0 {
			break
		}
		removed := 0
		for _, i := range flex {
			if excess <= 0 {
				break
			}
			share := (widths[i] - floors[i]) * excess / slack
			if share <= 0 {
				share = 1
			}
			if share > widths[i]-floors[i] {
				share = widths[i] - floors[i]
			}
			if share <= 0 {
				continue
			}
			widths[i] -= share
			removed += share
			excess -= share
		}
		if removed <= 0 {
			break
		}
	}
}

func (t *modernTable) renderFrame(widths []int, left, mid, right string) {
	var sb strings.Builder
	sb.WriteString(left)
	for i, w := range widths {
		if i > 0 {
			sb.WriteString(mid)
		}
		sb.WriteString(strings.Repeat("─", w+2))
	}
	sb.WriteString(right)
	if t.borderColor != nil {
		t.borderColor.Fprint(t.w, sb.String()+"\n")
		return
	}
	fmt.Fprintln(t.w, sb.String())
}

// renderRow draws one row; row < 0 denotes the header line
func (t *modernTable) renderRow(rowIdx int, widths []int, cells []string, header bool) {
	var sb strings.Builder
	sb.WriteString("│")
	for i := range t.headers {
		cell := ""
		if i < len(cells) {
			cell = cells[i]
		}
		if displayWidth(cell) > widths[i] {
			cell = truncateCell(cell, widths[i])
		}
		var colorizer *color.Color
		if header {
			colorizer = t.headerColor
		} else if t.colorCell != nil {
			colorizer = t.colorCell(rowIdx, i, cell)
		}
		if t.alignments[i] == AlignRight {
			cell = fmt.Sprintf("%*s", widths[i], cell)
		} else {
			cell = fmt.Sprintf("%-*s", widths[i], cell)
		}
		sb.WriteString(" ")
		if colorizer != nil {
			sb.WriteString(colorizer.Sprint(cell))
		} else {
			sb.WriteString(cell)
		}
		sb.WriteString(" │")
	}
	fmt.Fprintln(t.w, sb.String())
}

func truncateCell(s string, max int) string {
	if max <= 0 {
		return ""
	}
	return runewidth.Truncate(stripANSI(s), max, "…")
}

// formatLatencyHuman renders milliseconds compactly: 0.85ms, 812ms, 1.24s, 2m05s, 1h03m
func formatLatencyHuman(ms float64) string {
	if ms <= 0 || math.IsNaN(ms) || math.IsInf(ms, 0) {
		return "0ms"
	}
	switch {
	case ms < 1:
		return fmt.Sprintf("%.2fms", ms)
	case ms < 10:
		return fmt.Sprintf("%.1fms", ms)
	case ms < 1000:
		return fmt.Sprintf("%.0fms", ms)
	case ms < 60000:
		return fmt.Sprintf("%.2fs", ms/1000.0)
	case ms < 3600000:
		m := int(ms) / 60000
		s := (int(ms) / 1000) % 60
		return fmt.Sprintf("%dm%02ds", m, s)
	default:
		h := int(ms) / 3600000
		m := (int(ms) / 60000) % 60
		return fmt.Sprintf("%dh%02dm", h, m)
	}
}

// formatDurationHuman normalizes a slow-log duration given in seconds and/or
// milliseconds into the compact human form
func formatDurationHuman(durSec, durMs float64) string {
	switch {
	case durSec >= 1.0:
		return formatLatencyHuman(durSec * 1000.0)
	case durMs > 0:
		return formatLatencyHuman(durMs)
	case durSec > 0:
		return formatLatencyHuman(durSec * 1000.0)
	default:
		return "0ms"
	}
}

// bandColor colors a "higher is worse" metric: yellow from warnAt, red from critAt
func bandColor(v, warnAt, critAt float64) *color.Color {
	if v >= critAt {
		return colorRed
	}
	if v >= warnAt {
		return colorYellow
	}
	return nil
}

// deltaPctColor colors a percentage change: red for large regressions, yellow
// for moderate ones, green for meaningful improvements (analysis thresholds)
func deltaPctColor(pct float64) *color.Color {
	switch {
	case pct >= RegressionCritPct:
		return colorRed
	case pct >= RegressionWarnPct:
		return colorYellow
	case pct <= RegressionImprovePct:
		return colorGreen
	default:
		return nil
	}
}

// severityColor maps a regression severity to its status color
func severityColor(sev model.RegressionSeverity) *color.Color {
	switch sev {
	case model.SeverityCritical:
		return colorRed
	case model.SeverityWarning:
		return colorYellow
	case model.SeverityImprove, model.SeverityNone:
		return colorGreen
	default:
		return nil
	}
}

func truncate(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
