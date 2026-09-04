package cmd

import (
	"fmt"
	"os"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

// deployCheckCmd is retained temporarily as a deprecated compatibility alias for 'azlens deploy' (Section 6.4).
var deployCheckCmd = &cobra.Command{
	Use:        "deploy-check [duration]",
	Short:      "Check telemetry before vs after deploy (deprecated; use 'azlens deploy')",
	Deprecated: "use 'azlens deploy' instead",
	Hidden:     true,
	Args:       cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Fprintln(os.Stderr, color.YellowString("⚠️  Warning: 'azlens deploy-check' is deprecated; use 'azlens deploy' instead."))
		return deployCmd.RunE(cmd, args)
	},
}

func init() {
	deployCheckCmd.Flags().StringVarP(&deployAtTimeFlag, "at", "a", "", "Deployment time to center comparison around (e.g. '14:30', '-20m')")

	RootCmd.AddCommand(deployCheckCmd)
}
