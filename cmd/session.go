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

// configuredSubscriptions returns the deduplicated subscriptions targeted by the profile
func configuredSubscriptions(prof config.Profile) []string {
	insightsSub := strings.TrimSpace(prof.Target.Insights.Subscription)
	logsSub := strings.TrimSpace(prof.Target.Logs.Subscription)

	subs := make([]string, 0, 2)
	if insightsSub != "" {
		subs = append(subs, insightsSub)
	}
	if logsSub != "" && logsSub != insightsSub {
		subs = append(subs, logsSub)
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

// launchAzLogin hands the terminal over to 'az login' so the user can
// authenticate the directory owning the missing subscriptions
func launchAzLogin(missing []string) error {
	fmt.Fprintf(os.Stderr, "\n🔐 Subscription(s) %s not in the active az session.\n   Launching 'az login' — pick the account/directory that owns them (tokens stay inside the az CLI)...\n\n", strings.Join(missing, ", "))
	cmd := exec.Command("az", "login")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ensureSubscriptionSessions verifies that every subscription targeted by the
// profile is available in the current az session. When one is missing and
// running on a TTY, the interactive 'az login' flow is launched once and the
// subscriptions are re-verified. Non-TTY runs (CI) fail fast with actionable
// guidance instead of hanging.
func ensureSubscriptionSessions(prof config.Profile) error {
	subs := configuredSubscriptions(prof)
	if len(subs) == 0 {
		return nil
	}

	ctx := context.Background()
	var missing []string
	for _, s := range subs {
		ok, err := subscriptionAccessible(ctx, s)
		if err != nil {
			return err
		}
		if !ok {
			missing = append(missing, s)
		}
	}
	if len(missing) == 0 {
		return nil
	}

	if isInteractiveTerminal() {
		if err := launchAzLogin(missing); err != nil {
			fmt.Fprintf(os.Stderr, "⚠️  'az login' did not complete (%v)\n", err)
		} else {
			var still []string
			for _, s := range missing {
				ok, err := subscriptionAccessible(ctx, s)
				if err == nil && ok {
					continue
				}
				still = append(still, s)
			}
			if len(still) == 0 {
				fmt.Fprintln(os.Stderr, "✓ Azure session covers all configured subscriptions")
				return nil
			}
			missing = still
		}
	}

	return fmt.Errorf("subscription(s) not in the active az session: %s\n💡 Hint: Run 'az login --tenant <tenant-id>' for the directory hosting each subscription and retry (azlens does not store tokens; sessions are managed by the az CLI)", strings.Join(missing, ", "))
}
