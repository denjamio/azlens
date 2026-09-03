package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
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

// azRequiredExtensions maps the az command groups azlens invokes to the Azure
// CLI extension that provides them. Both query commands are extension commands:
// without them installed, az fails with "'X' is misspelled or not recognized by
// the system." (az itself misspells "misspelled" — the client matches both
// spellings of its output).
var azRequiredExtensions = []struct {
	Extension string
	Provides  string
}{
	{"application-insights", "az monitor app-insights query (App Insights telemetry)"},
	{"log-analytics", "az monitor log-analytics query (Log Analytics telemetry)"},
}

// missingAzExtensions returns the azlens-required extensions not installed in the az CLI.
// 'az extension list' is a local, unauthenticated operation.
func missingAzExtensions() ([]string, error) {
	out, err := exec.Command("az", "--only-show-errors", "extension", "list", "-o", "json").Output()
	if err != nil {
		return nil, fmt.Errorf("failed listing az extensions: %w", err)
	}
	var exts []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(out, &exts); err != nil {
		return nil, fmt.Errorf("failed parsing az extension list: %w", err)
	}
	installed := make(map[string]bool, len(exts))
	for _, e := range exts {
		installed[strings.ToLower(e.Name)] = true
	}
	var missing []string
	for _, req := range azRequiredExtensions {
		if !installed[req.Extension] {
			missing = append(missing, req.Extension)
		}
	}
	return missing, nil
}

// checkRequiredAzExtensions fails fast with actionable guidance when the
// extension providing the az command groups for this profile's configured
// backends is missing. log-analytics is also the App Insights fallback backend
// (queries route to the workspace when insights.name is empty).
func checkRequiredAzExtensions(prof config.Profile) error {
	missing, err := missingAzExtensions()
	if err != nil {
		// Cannot verify locally: let the query itself surface any real problem
		return nil
	}
	needed := make(map[string]bool)
	if strings.TrimSpace(prof.Target.Insights.Name) != "" {
		needed["application-insights"] = true
	}
	if strings.TrimSpace(prof.Target.Logs.WorkspaceID) != "" || strings.TrimSpace(prof.Target.Insights.Name) == "" {
		needed["log-analytics"] = true
	}
	var blocking []string
	for _, m := range missing {
		if needed[m] {
			blocking = append(blocking, m)
		}
	}
	if len(blocking) == 0 {
		return nil
	}
	hints := make([]string, 0, len(blocking))
	for _, m := range blocking {
		hints = append(hints, fmt.Sprintf("'az extension add --name %s'", m))
	}
	return fmt.Errorf("azure cli extension(s) not installed: %s — the query commands for this profile's targets will not work.\n💡 Hint: Run %s (verify with 'az extension list')", strings.Join(blocking, ", "), strings.Join(hints, " and "))
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

	// Fail fast with guidance when the az extension providing this profile's
	// backend commands is missing (avoids the cryptic az "not recognized" error)
	if err := checkRequiredAzExtensions(prof); err != nil {
		return err
	}

	// Verify the profile's subscriptions are authenticated in the current az
	// session; on a TTY, launch the interactive 'az login' flow to fix it
	if err := ensureSubscriptionSessions(prof); err != nil {
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

		// 2b. Azure CLI extension check (the query commands are extension commands)
		if azPath != "" {
			missing, extErr := missingAzExtensions()
			switch {
			case extErr != nil:
				color.Yellow("⚠️  Azure CLI extensions: could not verify (%v)", extErr)
			case len(missing) > 0:
				color.Red("✗ Azure CLI extensions: missing %s", strings.Join(missing, ", "))
				for _, m := range missing {
					for _, req := range azRequiredExtensions {
						if req.Extension == m {
							fmt.Printf("    %s\n", req.Provides)
						}
					}
					fmt.Printf("  💡 Hint: Run 'az extension add --name %s'\n", m)
				}
				hasErrors = true
			default:
				color.Green("✓ Azure CLI extensions: application-insights, log-analytics installed")
			}
		}

		// 2c. Session coverage: each subscription targeted by the active profile
		for _, s := range configuredSubscriptions(rt.Profile) {
			if ok, err := subscriptionAccessible(cmd.Context(), s); err == nil && ok {
				color.Green("✓ Subscription session: %s available in the current az session", s)
			} else {
				color.Red("✗ Subscription session: %s not available in the current az session", s)
				fmt.Println("  💡 Hint: Run 'az login --tenant <tenant-id>' for the directory hosting this subscription")
				hasErrors = true
			}
		}

		// 3. Config file check
		if rt.Config != nil && rt.Config.LoadedPath != "" {
			color.Green("✓ Configuration: loaded from %s", rt.Config.LoadedPath)
		} else {
			color.Yellow("⚠️  Configuration: running with in-memory defaults (no azlens.yaml found)")
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
