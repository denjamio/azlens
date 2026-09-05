package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/denjamio/azlens/pkg/analysis"
	"github.com/denjamio/azlens/pkg/config"
	"github.com/denjamio/azlens/pkg/domain"
	"github.com/denjamio/azlens/pkg/reporter"
	"github.com/denjamio/azlens/pkg/telemetry"
)

func checkAzureCLIAvailable() (string, error) {
	azPath, err := exec.LookPath("az")
	if err != nil {
		return "", fmt.Errorf("the Azure CLI ('az') is not available in PATH.\n💡 Hint: Install Azure CLI (curl -sL https://aka.ms/InstallAzureCLIDeb | sudo bash) or use --mock for offline testing")
	}
	return azPath, nil
}

var azRequiredExtensions = []struct {
	Extension string
	Provides  string
}{
	{"application-insights", "az monitor app-insights query (App Insights telemetry)"},
	{"log-analytics", "az monitor log-analytics query (Log Analytics telemetry)"},
}

func missingAzExtensions() ([]string, error) {
	out, err := exec.Command("az", "extension", "list", "--only-show-errors", "-o", "json").Output()
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

func checkRequiredAzExtensions(prof config.Profile) error {
	missing, err := missingAzExtensions()
	if err != nil {
		return nil
	}
	needed := make(map[string]bool)
	if strings.TrimSpace(prof.Target.InsightsName) != "" {
		needed["application-insights"] = true
	}
	if strings.TrimSpace(prof.Target.Logs.WorkspaceID) != "" || strings.TrimSpace(prof.Target.InsightsName) == "" {
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
	return fmt.Errorf("azure cli extension(s) not installed: %s\n💡 Hint: Run %s", strings.Join(blocking, ", "), strings.Join(hints, " and "))
}

func validateProfileIssues(prof config.Profile) ([]config.ValidationIssue, error) {
	issues := prof.Validate()
	for _, iss := range issues {
		if iss.Severity == config.SeverityError {
			return issues, fmt.Errorf("[%s] %s\n💡 Hint: %s", iss.Field, iss.Message, iss.Hint)
		}
	}
	return issues, nil
}

func runPreflightDiagnostics(prof config.Profile) error {
	if _, err := checkAzureCLIAvailable(); err != nil {
		return err
	}

	if err := checkRequiredAzExtensions(prof); err != nil {
		return err
	}

	issues, err := validateProfileIssues(prof)
	for _, iss := range issues {
		if iss.Severity == config.SeverityWarning {
			fmt.Fprintf(os.Stderr, "⚠️  [%s] %s\n💡 Hint: %s\n", iss.Field, iss.Message, iss.Hint)
		}
	}
	return err
}

// doctorCmd represents the doctor command (Section 6.5).
// Question answered: "Can AzLens correctly observe this environment?"
var doctorCmd = &cobra.Command{
	Use:     "doctor",
	Short:   "Diagnose AzLens configuration, authentication, reachability, and capability coverage",
	Long:    "Inspects local environment, Azure credentials, data-source reachability, and telemetry coverage.",
	GroupID: "operational",
	RunE: func(cmd *cobra.Command, args []string) error {
		rt := runtimeFrom(cmd)
		ctx := cmd.Context()

		docResult := &reporter.DoctorResult{
			ProfileName: rt.Profile.Name,
		}
		if docResult.ProfileName == "" {
			docResult.ProfileName = rt.ProfileName
		}

		// 1. Azure CLI Authentication Check
		if mockFlag {
			docResult.AzureAuth = true
			docResult.AuthUser = "mock-user@azure.local"
		} else {
			azPath, err := checkAzureCLIAvailable()
			if err == nil && azPath != "" {
				authCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				defer cancel()
				out, azErr := exec.CommandContext(authCtx, "az", "account", "show", "--query", "{user:user.name,sub:name,subId:id}", "-o", "json").Output()
				if azErr == nil {
					var info struct {
						User  string `json:"user"`
						Sub   string `json:"sub"`
						SubID string `json:"subId"`
					}
					if json.Unmarshal(out, &info) == nil && info.Sub != "" {
						docResult.AzureAuth = true
						docResult.AuthUser = info.User
						docResult.AuthSub = info.Sub
					}
				}
			}
		}

		// 2. Configured Backends Check
		var backends []reporter.BackendStatus
		if rt.Profile.Target.InsightsName != "" {
			backends = append(backends, reporter.BackendStatus{
				Name:      "Application Insights",
				Available: true,
			})
		}
		if rt.Profile.Target.Logs.WorkspaceID != "" {
			backends = append(backends, reporter.BackendStatus{
				Name:      "Log Analytics",
				Available: true,
			})
		}
		if len(backends) == 0 {
			backends = append(backends, reporter.BackendStatus{
				Name:      "Telemetry Backend",
				Available: false,
				Details:   "neither insights_name nor logs.workspace_id configured",
			})
		}
		docResult.Backends = backends

		// 3. Telemetry Coverage Check
		now := time.Now()
		builder := telemetry.NewSnapshotBuilder(rt.Client)
		snap, _ := builder.BuildSnapshot(ctx, rt.ProfileName, rt.Profile, now.Add(-1*time.Hour), now, "last 1h")
		if snap == nil {
			snap = domain.NewSnapshot(
				domain.ProfileContext{Name: rt.ProfileName, DisplayName: rt.Profile.Name},
				domain.Scope{Service: rt.Profile.Target.Service, Role: rt.Profile.Target.RoleName, Pod: rt.Profile.Target.Pod, Database: rt.Profile.Target.Logs.Database},
				domain.WindowContext{Label: "last 1h", Start: now.Add(-1 * time.Hour), End: now},
			)
		}

		evaluator := analysis.NewCapabilityEvaluator()
		docResult.Coverage = evaluator.EvaluateCoverage(snap)

		if rt.Output == "json" {
			return reporter.PrintJSON(os.Stdout, docResult)
		}

		reporter.PrintDoctorTerminal(os.Stdout, docResult)
		return nil
	},
}

func init() {
	RootCmd.AddCommand(doctorCmd)
}
