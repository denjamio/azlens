package azure

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/denjamio/azlens/pkg/config"
)

// fakeAzMissingExtension builds an az stub that reproduces the real CLI
// behavior when the extension providing a command group is not installed:
// az reports the group as unknown with its own misspelling of "misspelled"
// (single "s") — the exact string below matches az's real output.
func fakeAzMissingExtension(t *testing.T, capturePath string) (binDir string) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\n" +
		"echo \"$@\" >> " + capturePath + "\n" +
		"echo \"az monitor $2 query: '$2' is mispelled or not recognized by the system.\" >&2\n" + //nolint:misspell // verbatim az output
		"exit 2\n"
	path := filepath.Join(dir, "az")
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("failed writing fake az script: %v", err)
	}
	return dir
}

func TestMissingExtensionErrorGuidesWithHint(t *testing.T) {
	cases := []struct {
		name     string
		profile  config.Profile
		run      func(c *AzCliClient, ctx context.Context) error
		wantHint string
	}{
		{
			name: "log analytics",
			profile: config.Profile{
				Target: config.TargetConfig{
					Logs: config.LogsConfig{WorkspaceID: "ws-guid"},
				},
			},
			run: func(c *AzCliClient, ctx context.Context) error {
				_, err := c.QueryMySQLSlowLogs(ctx, time.Now().Add(-time.Hour), time.Now(), "db", 5)
				return err
			},
			wantHint: "az extension add --name log-analytics",
		},
		{
			name: "app insights",
			profile: config.Profile{
				Target: config.TargetConfig{
					InsightsName: "app-shared-prod",
					RoleName:     "order-service",
				},
			},
			run: func(c *AzCliClient, ctx context.Context) error {
				_, err := c.QueryRequestsSummary(ctx, time.Now().Add(-time.Hour), time.Now())
				return err
			},
			wantHint: "az extension add --name application-insights",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			capture := filepath.Join(t.TempDir(), "args.txt")
			t.Setenv("PATH", fakeAzMissingExtension(t, capture)+string(os.PathListSeparator)+os.Getenv("PATH"))

			client := &AzCliClient{opts: ClientOptions{Profile: tc.profile}}
			err := tc.run(client, context.Background())
			if err == nil {
				t.Fatalf("expected missing-extension error, got nil")
			}
			msg := err.Error()
			if !strings.Contains(msg, tc.wantHint) {
				t.Errorf("expected actionable hint %q in error:\n%s", tc.wantHint, msg)
			}
			if !strings.Contains(msg, "azure cli command not recognized") {
				t.Errorf("expected classified 'azure cli command not recognized' error:\n%s", msg)
			}
		})
	}
}

func TestMissingExtensionFailsFastWithoutRetry(t *testing.T) {
	capture := filepath.Join(t.TempDir(), "args.txt")
	t.Setenv("PATH", fakeAzMissingExtension(t, capture)+string(os.PathListSeparator)+os.Getenv("PATH"))

	client := &AzCliClient{opts: ClientOptions{Profile: config.Profile{
		Target: config.TargetConfig{
			Logs: config.LogsConfig{WorkspaceID: "ws-guid"},
		},
	}}}
	_, err := client.QueryMySQLSlowLogs(context.Background(), time.Now().Add(-time.Hour), time.Now(), "db", 5)
	if err == nil {
		t.Fatalf("expected missing-extension error, got nil")
	}

	data, readErr := os.ReadFile(capture)
	if readErr != nil {
		t.Fatalf("failed reading capture file: %v", readErr)
	}
	// One '--only-show-errors' occurrence per az invocation (KQL arguments
	// contain embedded newlines, so line counting would overcount)
	invocations := strings.Count(string(data), "--only-show-errors")
	if invocations != 1 {
		t.Errorf("expected fail-fast after 1 az invocation (no retries), got %d", invocations)
	}
}

func TestAzExtensionForArgs(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"monitor", "log-analytics", "query"}, "log-analytics"},
		{[]string{"monitor", "app-insights", "query"}, "application-insights"},
		{[]string{"monitor", "metrics", "list"}, ""},
		{[]string{"account", "show"}, ""},
		{[]string{"monitor"}, ""},
	}
	for _, tc := range cases {
		if got := azExtensionForArgs(tc.args); got != tc.want {
			t.Errorf("azExtensionForArgs(%v) = %q, want %q", tc.args, got, tc.want)
		}
	}
}
