package cmd

import (
	"fmt"
	"os"

	"github.com/fatih/color"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/denjamio/azlens/pkg/config"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage azlens configuration and project profiles",
	Long:  `View and initialize project/environment profiles in .azlens.yaml.`,
}

var configListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all configured project profiles",
	RunE: func(cmd *cobra.Command, args []string) error {
		rt := runtimeFrom(cmd)
		table := tablewriter.NewWriter(os.Stdout)
		table.SetHeader([]string{"Active", "Profile", "App Insights", "Workspace", "Role"})
		table.SetBorder(true)

		for _, key := range rt.Config.AvailableProfiles() {
			prof, err := rt.Config.GetProfile(key)
			if err != nil {
				continue
			}

			activeMark := ""
			if key == rt.ProfileName {
				activeMark = color.GreenString("✓ (current)")
			}

			insName := prof.Target.Insights.Name
			if insName == "" {
				insName = "-"
			}
			wsID := prof.Target.Logs.WorkspaceID
			if wsID == "" {
				wsID = "-"
			}
			roleScope := prof.Target.Role
			if roleScope == "" {
				roleScope = "-"
			}

			table.Append([]string{
				activeMark,
				key,
				insName,
				wsID,
				roleScope,
			})
		}
		table.Render()
		return nil
	},
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Display the active profile configuration",
	RunE: func(cmd *cobra.Command, args []string) error {
		rt := runtimeFrom(cmd)
		prof, err := rt.Config.GetProfile(rt.ProfileName)
		if err != nil {
			return err
		}

		fmt.Printf("Active Profile: %s (Source: %s)\n\n", color.CyanString(rt.ProfileName), rt.Config.LoadedPath)
		data, err := yaml.Marshal(prof)
		if err != nil {
			return fmt.Errorf("failed rendering profile: %w", err)
		}
		fmt.Println(string(data))
		return nil
	},
}

var configInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Create a starter .azlens.yaml config file in current directory",
	RunE: func(cmd *cobra.Command, args []string) error {
		target := ".azlens.yaml"
		if _, err := os.Stat(target); err == nil {
			return fmt.Errorf("%s already exists in current directory", target)
		}

		if err := os.WriteFile(target, []byte(config.StarterConfigTemplate), 0644); err != nil {
			return fmt.Errorf("failed writing %s: %w", target, err)
		}

		color.Green("✓ Created %s (commented template with prod, staging, dev environments).", target)
		fmt.Println("  Next step: run 'azlens doctor' to review missing parameters.")
		return nil
	},
}

func init() {
	configCmd.AddCommand(configListCmd)
	configCmd.AddCommand(configShowCmd)
	configCmd.AddCommand(configInitCmd)

	RootCmd.AddCommand(configCmd)
}
