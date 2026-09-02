package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/denjamio/azlens/pkg/config"
)

// checkAzureCLIAvailable looks up the az binary in PATH
func checkAzureCLIAvailable() (string, error) {
	azPath, err := exec.LookPath("az")
	if err != nil {
		return "", fmt.Errorf("the Azure CLI ('az') is not available in PATH.\n💡 Hint: Install Azure CLI (curl -sL https://aka.ms/InstallAzureCLIDeb | sudo bash) or use --mock for offline testing")
	}
	return azPath, nil
}

// validateProfileIssues runs profile validation rules and returns any error
func validateProfileIssues(prof config.Profile) ([]config.ValidationIssue, error) {
	issues := prof.Validate()
	for _, iss := range issues {
		if iss.Severity == config.SeverityError {
			return issues, fmt.Errorf("[%s] %s\n💡 Hint: %s", iss.Field, iss.Message, iss.Hint)
		}
	}
	return issues, nil
}

// runPreflightDiagnostics runs the shared pre-flight checks used by PersistentPreRunE
func runPreflightDiagnostics(prof config.Profile) error {
	if _, err := checkAzureCLIAvailable(); err != nil {
		return err
	}

	issues, err := validateProfileIssues(prof)
	for _, iss := range issues {
		if iss.Severity == config.SeverityWarning {
			// Warnings do not block execution but surface risky configurations
			fmt.Fprintf(os.Stderr, "⚠️  [%s] %s\n💡 Hint: %s\n", iss.Field, iss.Message, iss.Hint)
		}
	}
	return err
}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Run diagnostics on Azure CLI authentication and azlens configuration",
	Long:  "Inspects local environment, Azure credentials, and validates the active profile targets.",
	RunE: func(cmd *cobra.Command, args []string) error {
		rt := runtimeFrom(cmd)
		fmt.Println(color.CyanString("AzLens Doctor Diagnostic Report"))
		fmt.Println("================================")

		hasErrors := false

		// 1. Azure CLI binary check (shared check)
		azPath, err := checkAzureCLIAvailable()
		if err != nil {
			color.Red("✗ Azure CLI ('az'): not found in PATH")
			fmt.Println("  💡 Hint: Install Azure CLI (curl -sL https://aka.ms/InstallAzureCLIDeb | sudo bash)")
			hasErrors = true
		} else {
			color.Green("✓ Azure CLI ('az'): %s", azPath)
		}

		// 2. Azure CLI authentication check
		if azPath != "" {
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()
			out, azErr := exec.CommandContext(ctx, "az", "account", "show", "--query", "{user:user.name,sub:name,subId:id}", "-o", "json").Output()
			if azErr != nil {
				color.Yellow("⚠️  Azure Login: not authenticated or session expired")
				fmt.Println("  💡 Hint: Run 'az login' to authenticate with Azure")
			} else {
				var info struct {
					User  string `json:"user"`
					Sub   string `json:"sub"`
					SubID string `json:"subId"`
				}
				if json.Unmarshal(out, &info) == nil && info.Sub != "" {
					color.Green("✓ Azure Login: authenticated as %s (Subscription: %s [%s])", info.User, info.Sub, info.SubID)
				} else {
					color.Green("✓ Azure Login: session active")
				}
			}
		}

		// 3. Config file check
		if rt.Config != nil && rt.Config.LoadedPath != "" {
			color.Green("✓ Configuration: loaded from %s", rt.Config.LoadedPath)
		} else {
			color.Yellow("⚠️  Configuration: running with in-memory defaults (no .azlens.yaml found)")
			fmt.Println("  💡 Hint: Run 'azlens config init' to create a starter configuration file")
		}

		// 4. Validate active profile (shared check)
		profName := rt.ProfileName
		fmt.Printf("\nValidating profile '%s':\n", color.CyanString(profName))

		issues, valErr := validateProfileIssues(rt.Profile)
		if len(issues) == 0 {
			color.Green("✓ Profile '%s' is valid and ready to query!", profName)
		} else {
			for _, iss := range issues {
				switch iss.Severity {
				case config.SeverityError:
					color.Red("✗ [%s] %s", iss.Field, iss.Message)
					fmt.Printf("  💡 %s\n", iss.Hint)
				case config.SeverityWarning:
					color.Yellow("⚠️  [%s] %s", iss.Field, iss.Message)
					fmt.Printf("  💡 %s\n", iss.Hint)
				case config.SeverityInfo:
					color.Cyan("ℹ️  [%s] %s", iss.Field, iss.Message)
					fmt.Printf("  💡 %s\n", iss.Hint)
				}
			}
		}
		if valErr != nil {
			hasErrors = true
		}

		fmt.Println()
		if hasErrors {
			return fmt.Errorf("doctor detected configuration issues that prevent telemetry queries")
		}
		color.Green("✓ Doctor checks complete. Everything is healthy!")
		return nil
	},
}

func init() {
	RootCmd.AddCommand(doctorCmd)
}
