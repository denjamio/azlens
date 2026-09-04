package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/denjamio/azlens/pkg/azure"
	"github.com/denjamio/azlens/pkg/config"
)

// subscriptionCheckTimeout bounds each 'az account show' session lookup
const subscriptionCheckTimeout = 5 * time.Second

// backendSession couples a configured subscription with the Entra directory
// (tenant ID) that hosts it and the isolated az profile that holds its session
type backendSession struct {
	subscription string
	tenant       string
	// azConfigDir is the isolated AZURE_CONFIG_DIR for this directory (empty =
	// the user's main az profile, when no directory_id is configured)
	azConfigDir string
}

// env returns the process environment prefix for az invocations of this backend
func (b backendSession) env() []string {
	if b.azConfigDir == "" {
		return nil
	}
	return []string{"AZURE_CONFIG_DIR=" + b.azConfigDir}
}

// subscriptionAccessible reports whether the subscription is available in the
// given az CLI profile. azlens never stores or refreshes tokens: session and
// token management remain entirely inside the az CLI.
func subscriptionAccessible(ctx context.Context, subscriptionID string, env []string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, subscriptionCheckTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "az", "--only-show-errors", "account", "show",
		"--subscription", subscriptionID, "--query", "id", "-o", "tsv")
	cmd.Env = append(os.Environ(), env...)
	if err := cmd.Run(); err != nil {
		// az executed but the subscription is not in the account list (or the
		// session is expired): treat both as "needs login"
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return false, nil
		}
		return false, fmt.Errorf("failed checking az session for subscription '%s': %w", subscriptionID, err)
	}
	return true, nil
}

// configuredBackends returns the deduplicated backends targeted by the profile,
// each with its subscription, hosting directory and isolated az profile
func configuredBackends(prof config.Profile) []backendSession {
	build := func(sub, tenant string) backendSession {
		b := backendSession{
			subscription: strings.TrimSpace(sub),
			tenant:       strings.TrimSpace(tenant),
		}
		if b.tenant != "" {
			// Isolation failure is non-fatal here: the backend falls back to the
			// main az profile and the query itself surfaces any real problem
			if dir, err := azure.AzureConfigDir(b.tenant); err == nil {
				b.azConfigDir = dir
			}
		}
		return b
	}

	insights := build(prof.Target.Insights.SubscriptionID, prof.Target.Insights.DirectoryID)
	logs := build(prof.Target.Logs.SubscriptionID, prof.Target.Logs.DirectoryID)

	var out []backendSession
	if insights.subscription != "" {
		out = append(out, insights)
	}
	if logs.subscription != "" && logs.subscription != insights.subscription {
		out = append(out, logs)
	}
	return out
}

// configuredSubscriptions returns the deduplicated subscriptions targeted by the profile
func configuredSubscriptions(prof config.Profile) []string {
	backends := configuredBackends(prof)
	subs := make([]string, 0, len(backends))
	for _, b := range backends {
		subs = append(subs, b.subscription)
	}
	return subs
}

// isInteractiveTerminal reports whether stdin and stdout are attached to a TTY.
// Interactive login is only launched on real terminals; CI runs fail fast
// instead of hanging on a device-code prompt nobody can answer.
func isInteractiveTerminal() bool {
	for _, f := range []*os.File{os.Stdin, os.Stdout} {
		info, err := f.Stat()
		if err != nil || (info.Mode()&os.ModeCharDevice) == 0 {
			return false
		}
	}
	return true
}

// azLoginArgs builds the az login arguments: '--tenant <id>' when the directory
// hosting the missing subscription is configured, plain 'az login' otherwise
func azLoginArgs(tenant string) []string {
	if tenant != "" {
		return []string{"login", "--tenant", tenant}
	}
	return []string{"login"}
}

// azLoginCmd builds the interactive 'az login' command for the given directory
// and isolated profile. The child process disables the v2 login experience
// (account picker) so the flow goes straight to authentication; the user's
// main az profile is never modified. To make the direct flow permanent for all
// az usage: 'az config set core.login_experience_v2=off'.
func azLoginCmd(tenant, azConfigDir string) *exec.Cmd {
	cmd := exec.Command("az", azLoginArgs(tenant)...)
	cmd.Env = append(os.Environ(), "AZURE_CORE_LOGIN_EXPERIENCE_V2=off")
	if azConfigDir != "" {
		cmd.Env = append(cmd.Env, "AZURE_CONFIG_DIR="+azConfigDir)
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd
}

// launchAzLogin hands the terminal over to 'az login' so the user can
// authenticate the directory owning the missing subscriptions
func launchAzLogin(tenant, azConfigDir string) error {
	if tenant != "" {
		fmt.Fprintf(os.Stderr, "\n🔐 Launching 'az login --tenant %s' — authenticate the directory hosting the missing subscription (tokens stay inside the az CLI)...\n\n", tenant)
	} else {
		fmt.Fprintln(os.Stderr, "\n🔐 Launching 'az login' — pick the account/directory that owns the missing subscription (tokens stay inside the az CLI)...")
	}
	return azLoginCmd(tenant, azConfigDir).Run()
}

// ensureSubscriptionSessions verifies that every subscription targeted by the
// profile is available in its az profile (isolated per directory when
// directory_id is configured). When one is missing and running on a TTY, the
// interactive login flow is launched — once per distinct directory —
// re-verifying after each login. Non-TTY runs (CI) fail fast with actionable
// guidance instead of hanging.
func ensureSubscriptionSessions(prof config.Profile) error {
	backends := configuredBackends(prof)
	if len(backends) == 0 {
		return nil
	}

	ctx := context.Background()
	accessible := func(b backendSession) bool {
		ok, err := subscriptionAccessible(ctx, b.subscription, b.env())
		return err == nil && ok
	}

	var missing []backendSession
	for _, b := range backends {
		if !accessible(b) {
			missing = append(missing, b)
		}
	}
	if len(missing) == 0 {
		return nil
	}

	if isInteractiveTerminal() {
		// One login per distinct directory hosting a missing subscription;
		// plain 'az login' only when no directory is configured for it
		launched := make(map[string]bool)
		for _, b := range missing {
			key := b.tenant + "|" + b.azConfigDir
			if launched[key] {
				continue
			}
			launched[key] = true

			if err := launchAzLogin(b.tenant, b.azConfigDir); err != nil {
				fmt.Fprintf(os.Stderr, "⚠️  'az login' did not complete (%v)\n", err)
				continue
			}

			var still []backendSession
			for _, m := range missing {
				if !accessible(m) {
					still = append(still, m)
				}
			}
			missing = still
			if len(missing) == 0 {
				fmt.Fprintln(os.Stderr, "✓ Azure session covers all configured subscriptions")
				return nil
			}
		}
	}

	missingSubs := make([]string, 0, len(missing))
	hints := make([]string, 0, len(missing))
	for _, b := range missing {
		missingSubs = append(missingSubs, b.subscription)
		hints = append(hints, "'"+loginHint(b)+"'")
	}
	return fmt.Errorf("subscription(s) not in the active az session: %s\n💡 Hint: authenticate the directory hosting each subscription and retry: %s (azlens does not store tokens; sessions are managed by the az CLI)",
		strings.Join(missingSubs, ", "), strings.Join(hints, " and "))
}

// loginHint builds the exact, copy-pasteable login command for a backend
func loginHint(b backendSession) string {
	prefix := ""
	if b.azConfigDir != "" {
		prefix = "AZURE_CONFIG_DIR=" + b.azConfigDir + " "
	}
	if b.tenant != "" {
		return prefix + "az login --tenant " + b.tenant
	}
	return prefix + "az login --tenant <directory-id of " + b.subscription + ">"
}
