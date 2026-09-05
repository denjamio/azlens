package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	// Version is populated at build time via -ldflags
	Version = "1.1.17"
	Commit  = "dev"
	Date    = "unknown"
)

var versionCmd = &cobra.Command{
	Use:     "version",
	Short:   "Print the current azlens version and build information",
	GroupID: "supporting",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("azlens version %s (commit: %s, built: %s)\n", Version, Commit, Date)
	},
}

func init() {
	RootCmd.AddCommand(versionCmd)
}
