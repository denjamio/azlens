package reporter

import (
	"fmt"
	"io"
	"strings"
)

// Render dispatches telemetry data to the appropriate formatter based on the
// requested output format ("table" default, "markdown"/"md", or "json").
// It centralizes the format switch so every command renders identically.
func Render[T any](w io.Writer, format string, data T, table func(io.Writer, T), markdown func(io.Writer, T)) error {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "table":
		table(w, data)
		return nil
	case "json":
		return PrintJSON(w, data)
	case "markdown", "md":
		markdown(w, data)
		return nil
	default:
		return fmt.Errorf("unsupported output format %q: expected table, markdown, or json", format)
	}
}
