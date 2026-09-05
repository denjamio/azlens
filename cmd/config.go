package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/denjamio/azlens/pkg/config"
	"github.com/denjamio/azlens/pkg/reporter"
)

// configCmd represents the config command (Section 6.6).
// Question answered: "Where and how does AzLens look?"
var configCmd = &cobra.Command{
	Use:     "config",
	Short:   "View and manage AzLens configuration and environment profiles",
	Long:    `View effective configuration, list environment profiles, and identify config file paths.`,
	GroupID: "supporting",
}

var configProfilesCmd = &cobra.Command{
	Use:     "profiles",
	Aliases: []string{"list"},
	Short:   "List all configured environment profiles and identify the default profile",
	RunE: func(cmd *cobra.Command, args []string) error {
		rt := runtimeFrom(cmd)
		defaultProfile := rt.Config.GetDefaultProfile()

		table := reporter.NewTable(os.Stdout, []string{"Active", "Profile", "App Insights", "Workspace", "Service"},
			[]int{reporter.AlignLeft, reporter.AlignLeft, reporter.AlignLeft, reporter.AlignLeft, reporter.AlignLeft})

		for _, key := range rt.Config.AvailableProfiles() {
			prof, err := rt.Config.GetProfile(key)
			if err != nil {
				continue
			}

			activeMark := ""
			if key == rt.ProfileName {
				activeMark = color.GreenString("✓ (active)")
			} else if key == defaultProfile {
				activeMark = color.CyanString("(default)")
			}

			insName := prof.Target.GetInsightsName()
			if insName == "" {
				insName = "-"
			}
			wsID := prof.Target.Logs.WorkspaceID
			if wsID == "" {
				wsID = "-"
			}
			serviceScope := prof.Target.Service
			if serviceScope == "" {
				serviceScope = prof.Target.GetRoleName()
			}
			if serviceScope == "" {
				serviceScope = "-"
			}

			table.Append([]string{
				activeMark,
				key,
				insName,
				wsID,
				serviceScope,
			})
		}
		table.Render()
		return nil
	},
}

var configPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Print the path of the configuration file currently in use",
	RunE: func(cmd *cobra.Command, args []string) error {
		rt := runtimeFrom(cmd)
		if rt.Config != nil && rt.Config.LoadedPath != "" {
			fmt.Println(rt.Config.LoadedPath)
		} else {
			fmt.Println("in-memory defaults (no configuration file found)")
		}
		return nil
	},
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Display the effective active profile configuration with redacted secrets",
	RunE: func(cmd *cobra.Command, args []string) error {
		rt := runtimeFrom(cmd)
		prof, err := rt.Config.GetProfile(rt.ProfileName)
		if err != nil {
			return err
		}

		loadedFrom := rt.Config.LoadedPath
		if loadedFrom == "" {
			loadedFrom = "in-memory defaults"
		}

		fmt.Printf("Active Profile: %s (Source: %s)\n\n", color.CyanString(rt.ProfileName), loadedFrom)

		// Redact secrets in custom dimensions or fields
		redactedProf := redactProfileSecrets(prof)

		data, err := yaml.Marshal(redactedProf)
		if err != nil {
			return fmt.Errorf("failed rendering profile: %w", err)
		}
		fmt.Println(string(data))
		return nil
	},
}

func redactProfileSecrets(prof config.Profile) config.Profile {
	redacted := prof
	if len(prof.Target.CustomDimensions) > 0 {
		cleaned := make(map[string]string, len(prof.Target.CustomDimensions))
		for k, v := range prof.Target.CustomDimensions {
			lowerK := strings.ToLower(k)
			if strings.Contains(lowerK, "secret") ||
				strings.Contains(lowerK, "password") ||
				strings.Contains(lowerK, "token") ||
				strings.Contains(lowerK, "key") {
				cleaned[k] = "[REDACTED]"
			} else {
				cleaned[k] = v
			}
		}
		redacted.Target.CustomDimensions = cleaned
	}
	return redacted
}

var configInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Create a starter azlens.yaml config file in current directory",
	RunE: func(cmd *cobra.Command, args []string) error {
		target := "azlens.yaml"
		if _, err := os.Stat(target); err == nil {
			return fmt.Errorf("%s already exists in current directory", target)
		}

		if err := os.WriteFile(target, []byte(config.StarterConfigTemplate), 0644); err != nil {
			return fmt.Errorf("failed writing %s: %w", target, err)
		}

		color.Green("✓ Created %s (commented template with prod, staging, dev environments).", target)
		fmt.Println("  Next step: commit this file — it is the team-shared configuration.")
		fmt.Println("  Then run 'azlens doctor' to review missing parameters.")
		return nil
	},
}

func init() {
	configCmd.AddCommand(configProfilesCmd)
	configCmd.AddCommand(configPathCmd)
	configCmd.AddCommand(configShowCmd)
	configCmd.AddCommand(configInitCmd)

	RootCmd.AddCommand(configCmd)
}
