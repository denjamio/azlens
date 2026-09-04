package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/denjamio/azlens/pkg/config"
)

// fakeAzAccountShow builds an az stub that emulates 'az account show': exit 0
// (subscription in session) for allowed IDs, exit 1 otherwise. Any other az
// invocation fails loudly so unexpected calls surface in tests.
func fakeAzAccountShow(t *testing.T, allowedSubs []string) string {
	t.Helper()
	dir := t.TempDir()
	cases := ""
	for _, s := range allowedSubs {
		cases += "    " + s + ") echo " + s + "; exit 0 ;;\n"
	}
	script := "#!/bin/sh\n" +
		"if [ \"$2\" = \"account\" ] && [ \"$3\" = \"show\" ] && [ \"$4\" = \"--subscription\" ]; then\n" +
		"  case \"$5\" in\n" +
		cases +
		"    *) echo SubscriptionNotFound >&2; exit 1 ;;\n" +
		"  esac\n" +
		"fi\n" +
		"echo \"unexpected az invocation: $@\" >&2; exit 42\n"
	path := filepath.Join(dir, "az")
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("failed writing fake az script: %v", err)
	}
	return dir
}

func TestSubscriptionAccessible(t *testing.T) {
	stub := fakeAzAccountShow(t, []string{"sub-ok"})
	t.Setenv("PATH", stub+string(os.PathListSeparator)+os.Getenv("PATH"))

	ok, err := subscriptionAccessible(context.Background(), "sub-ok")
	if err != nil || !ok {
		t.Errorf("expected subscription 'sub-ok' accessible, got ok=%v err=%v", ok, err)
	}

	ok, err = subscriptionAccessible(context.Background(), "sub-missing")
	if err != nil {
		t.Errorf("expected nil error for missing subscription (needs login), got: %v", err)
	}
	if ok {
		t.Errorf("expected subscription 'sub-missing' to be inaccessible")
	}
}

func TestConfiguredSubscriptionsDedup(t *testing.T) {
	prof := config.Profile{
		Target: config.TargetConfig{
			Insights: config.InsightsConfig{SubscriptionID: "sub-a"},
			Logs:     config.LogsConfig{SubscriptionID: "sub-b"},
		},
	}
	got := configuredSubscriptions(prof)
	if len(got) != 2 || got[0] != "sub-a" || got[1] != "sub-b" {
		t.Errorf("expected [sub-a sub-b], got %v", got)
	}

	same := config.Profile{
		Target: config.TargetConfig{
			Insights: config.InsightsConfig{SubscriptionID: "sub-a"},
			Logs:     config.LogsConfig{SubscriptionID: "sub-a"},
		},
	}
	got = configuredSubscriptions(same)
	if len(got) != 1 || got[0] != "sub-a" {
		t.Errorf("expected deduplicated [sub-a], got %v", got)
	}

	empty := configuredSubscriptions(config.Profile{})
	if len(empty) != 0 {
		t.Errorf("expected no subscriptions, got %v", empty)
	}
}

func TestEnsureSubscriptionSessions(t *testing.T) {
	t.Run("all accessible", func(t *testing.T) {
		stub := fakeAzAccountShow(t, []string{"sub-a", "sub-b"})
		t.Setenv("PATH", stub+string(os.PathListSeparator)+os.Getenv("PATH"))

		prof := config.Profile{Target: config.TargetConfig{
			Insights: config.InsightsConfig{SubscriptionID: "sub-a"},
			Logs:     config.LogsConfig{SubscriptionID: "sub-b"},
		}}
		if err := ensureSubscriptionSessions(prof); err != nil {
			t.Errorf("expected nil error when all subscriptions are in session, got: %v", err)
		}
	})

	t.Run("no subscriptions configured", func(t *testing.T) {
		// Without the stub in PATH: no az invocation must happen at all
		prof := config.Profile{Target: config.TargetConfig{
			Insights: config.InsightsConfig{Name: "app-only"},
		}}
		if err := ensureSubscriptionSessions(prof); err != nil {
			t.Errorf("expected nil error with no configured subscriptions, got: %v", err)
		}
	})

	t.Run("missing subscription on non-tty fails fast with hint", func(t *testing.T) {
		stub := fakeAzAccountShow(t, []string{"sub-a"})
		t.Setenv("PATH", stub+string(os.PathListSeparator)+os.Getenv("PATH"))

		// Test stdin/stdout are pipes (not char devices), so the interactive
		// 'az login' launch must be skipped and the run must fail fast
		prof := config.Profile{Target: config.TargetConfig{
			Insights: config.InsightsConfig{SubscriptionID: "sub-a"},
			Logs:     config.LogsConfig{SubscriptionID: "sub-missing", DirectoryID: "dir-logs"},
		}}
		err := ensureSubscriptionSessions(prof)
		if err == nil {
			t.Fatalf("expected error for subscription missing from session")
		}
		if !strings.Contains(err.Error(), "sub-missing") {
			t.Errorf("expected error to name the missing subscription:\n%s", err)
		}
		if !strings.Contains(err.Error(), "az login --tenant") {
			t.Errorf("expected actionable login hint in error:\n%s", err)
		}
	})
}
