// Package reporter implements the output formatters: terminal tables,
// GitHub/GitLab markdown, and raw JSON rendering for every command.
package reporter

import (
	"encoding/json"
	"io"
	"os"
)

// PrintJSON formats any struct into pretty JSON
func PrintJSON(w io.Writer, data interface{}) error {
	if w == nil {
		w = os.Stdout
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(data)
}
