package cmd

import (
	"fmt"
	"os"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var updateCmd = &cobra.Command{
	Use:        "update",
	Short:      "Check for updates and self-upgrade the azlens binary (deprecated; use upgrade)",
	Deprecated: "use 'azlens upgrade' instead",
	Hidden:     true,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Fprintln(os.Stderr, color.YellowString("⚠️  Warning: 'azlens update' is deprecated; use 'azlens upgrade' instead."))
		return upgradeCmd.RunE(cmd, args)
	},
}

func init() {
	updateCmd.Flags().BoolVar(&upgradeCheckOnly, "check", false, "Only check if an upgrade is available without downloading")
	updateCmd.Flags().BoolVarP(&upgradeForce, "force", "f", false, "Force re-download even if already up to date")

	RootCmd.AddCommand(updateCmd)
}
