package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/denjamio/azlens/pkg/config"
)

// subscriptionCheckTimeout bounds each 'az account show' session lookup
const subscriptionCheckTimeout = 5 * time.Second

// backendSession couples a configured subscription with the Entra directory
// (tenant ID) that hosts it, so the right 'az login --tenant <id>' can be
// launched when the subscription is not in the session
type backendSession struct {
	subscription string
	tenant       string
}

// subscriptionAccessible reports whether the subscription is available in the
// current az CLI session. azlens never stores or refreshes tokens: session and
// token management remain entirely inside the az CLI.
func subscriptionAccessible(ctx context.Context, subscriptionID string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, subscriptionCheckTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "az", "--only-show-errors", "account", "show",
		"--subscription", subscriptionID, "--query", "id", "-o", "tsv")
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
// each with its subscription and (optionally) hosting directory
func configuredBackends(prof config.Profile) []backendSession {
	insights := backendSession{
		subscription: strings.TrimSpace(prof.Target.Insights.SubscriptionID),
		tenant:       strings.TrimSpace(prof.Target.Insights.DirectoryID),
	}
	logs := backendSession{
		subscription: strings.TrimSpace(prof.Target.Logs.SubscriptionID),
		tenant:       strings.TrimSpace(prof.Target.Logs.DirectoryID),
	}

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

// azLoginCmd builds the interactive 'az login' command for the given directory.
// The child process disables the v2 login experience (account picker) so the
// flow goes straight to authentication; the user's global az CLI configuration
// is never modified. To make it permanent: 'az config set core.login_experience_v2=off'.
func azLoginCmd(tenant string) *exec.Cmd {
	cmd := exec.Command("az", azLoginArgs(tenant)...)
	cmd.Env = append(os.Environ(), "AZURE_CORE_LOGIN_EXPERIENCE_V2=off")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd
}

// launchAzLogin hands the terminal over to 'az login' so the user can
// authenticate the directory owning the missing subscriptions
func launchAzLogin(tenant string) error {
	if tenant != "" {
		fmt.Fprintf(os.Stderr, "\n🔐 Launching 'az login --tenant %s' — authenticate the directory hosting the missing subscription (tokens stay inside the az CLI)...\n\n", tenant)
	} else {
		fmt.Fprintln(os.Stderr, "\n🔐 Launching 'az login' — pick the account/directory that owns the missing subscription (tokens stay inside the az CLI)...")
	}
	return azLoginCmd(tenant).Run()
}

// ensureSubscriptionSessions verifies that every subscription targeted by the
// profile is available in the current az session. When one is missing and
// running on a TTY, the interactive login flow is launched — once per distinct
// directory ('az login --tenant <id>' when the backend's tenant is configured) —
// re-verifying after each login. Non-TTY runs (CI) fail fast with actionable
// guidance instead of hanging.
func ensureSubscriptionSessions(prof config.Profile) error {
	backends := configuredBackends(prof)
	if len(backends) == 0 {
		return nil
	}

	ctx := context.Background()
	accessible := func(sub string) bool {
		ok, err := subscriptionAccessible(ctx, sub)
		return err == nil && ok
	}

	var missing []backendSession
	for _, b := range backends {
		if !accessible(b.subscription) {
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
			if launched[b.tenant] {
				continue
			}
			launched[b.tenant] = true

			if err := launchAzLogin(b.tenant); err != nil {
				fmt.Fprintf(os.Stderr, "⚠️  'az login' did not complete (%v)\n", err)
				continue
			}

			var still []backendSession
			for _, m := range missing {
				if !accessible(m.subscription) {
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
		if b.tenant != "" {
			hints = append(hints, fmt.Sprintf("'az login --tenant %s'", b.tenant))
		} else {
			hints = append(hints, fmt.Sprintf("'az login --tenant <tenant-id of %s>'", b.subscription))
		}
	}
	return fmt.Errorf("subscription(s) not in the active az session: %s\n💡 Hint: authenticate the directory hosting each subscription and retry: %s (azlens does not store tokens; sessions are managed by the az CLI)",
		strings.Join(missingSubs, ", "), strings.Join(hints, " and "))
}
