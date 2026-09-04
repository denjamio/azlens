package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/denjamio/azlens/pkg/config"
)

func resetRootFlags() {
	configPathFlag = ""
	profileFlag = ""
	outputFlag = config.DefaultOutput
	colorModeFlag = "auto"
	mockFlag = false
	printQueryFlag = false
	serviceFlag = ""
	inspectLimit = config.DefaultLimit
	inspectDepType = "all"
	inspectSlowLogsGrouped = false
	deployAtTimeFlag = ""
	RootCmd.SetArgs(nil)
	RootCmd.SetOut(nil)
	RootCmd.SetErr(nil)
	if f := RootCmd.Flags().Lookup("version"); f != nil {
		_ = f.Value.Set("false")
		f.Changed = false
	}
	if f := RootCmd.Flags().Lookup("mock"); f != nil {
		_ = f.Value.Set("false")
		f.Changed = false
	}
	if f := RootCmd.Flags().Lookup("profile"); f != nil {
		_ = f.Value.Set("")
		f.Changed = false
	}
	if f := RootCmd.Flags().Lookup("output"); f != nil {
		_ = f.Value.Set("table")
		f.Changed = false
	}
}

func TestRootVersionFlag(t *testing.T) {
	resetRootFlags()
	defer resetRootFlags()

	buf := new(bytes.Buffer)
	RootCmd.SetOut(buf)
	RootCmd.SetArgs([]string{"--version"})

	err := RootCmd.Execute()
	if err != nil {
		t.Fatalf("expected --version to succeed, got: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "azlens version") {
		t.Errorf("expected version output on root, got: %s", out)
	}
}

func TestConfigInitCreatesTemplate(t *testing.T) {
	resetRootFlags()
	defer resetRootFlags()

	dir := t.TempDir()
	origWd, _ := os.Getwd()
	defer func() { _ = os.Chdir(origWd) }()
	_ = os.Chdir(dir)

	RootCmd.SetArgs([]string{"config", "init"})
	err := RootCmd.Execute()
	if err != nil {
		t.Fatalf("config init failed: %v", err)
	}

	// The team-shared, committed file is azlens.yaml (no leading dot)
	cfgPath := filepath.Join(dir, "azlens.yaml")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("failed reading created azlens.yaml: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "prod:") || !strings.Contains(content, "staging:") || !strings.Contains(content, "dev:") {
		t.Errorf("expected 3 environments in starter template, got:\n%s", content)
	}
	if !strings.Contains(content, "profile: prod") {
		t.Errorf("expected prod as default profile, got:\n%s", content)
	}
	if strings.Contains(content, "app-prod-ecommerce") {
		t.Errorf("template should not contain fake ecommerce placeholders:\n%s", content)
	}
}

func TestDoctorRegistration(t *testing.T) {
	// doctor must be registered under root
	foundUnderRoot := false
	for _, c := range RootCmd.Commands() {
		if c.Name() == "doctor" {
			foundUnderRoot = true
			break
		}
	}
	if !foundUnderRoot {
		t.Errorf("expected 'doctor' to be registered under RootCmd")
	}

	// doctor must NOT be registered under config
	for _, c := range configCmd.Commands() {
		if c.Name() == "doctor" {
			t.Errorf("'doctor' should not be registered under configCmd; expected only under root")
		}
	}
}

func TestResolveInspectLimit(t *testing.T) {
	cmd := inspectEndpointsCmd
	rt := &appRuntime{
		EffectiveDefaults: config.Defaults{
			Limit: 42,
		},
		Resolver: NewDefaultResolver(config.Defaults{Limit: 42}),
	}
	cmd.SetContext(context.WithValue(context.Background(), runtimeContextKey{}, rt))

	f := cmd.Flag("limit")
	if f == nil {
		t.Fatalf("expected 'limit' flag to be found on inspectEndpointsCmd via parent")
	}

	// 1. Without flag changed, takes EffectiveDefaults
	f.Changed = false
	inspectLimit = 15
	if got := resolveInspectLimit(cmd); got != 42 {
		t.Errorf("expected 42 from EffectiveDefaults, got %d", got)
	}

	// 2. When flag is explicitly changed, flag wins
	f.Changed = true
	inspectLimit = 7
	if got := resolveInspectLimit(cmd); got != 7 {
		t.Errorf("expected 7 from explicit flag, got %d", got)
	}

	// Reset
	f.Changed = false
	inspectLimit = 15
}

func TestBrokenConfigDoesNotBlockIndependentCommands(t *testing.T) {
	resetRootFlags()
	defer resetRootFlags()

	dir := t.TempDir()
	brokenCfg := filepath.Join(dir, "azlens.yaml")
	// Malformed YAML
	if err := os.WriteFile(brokenCfg, []byte("invalid_yaml: [unclosed"), 0644); err != nil {
		t.Fatalf("failed creating broken config: %v", err)
	}

	// 1. Version command succeeds despite broken config
	buf := new(bytes.Buffer)
	RootCmd.SetOut(buf)
	RootCmd.SetArgs([]string{"--config", brokenCfg, "version"})
	if err := RootCmd.Execute(); err != nil {
		t.Errorf("expected 'version' to succeed with broken config, got: %v", err)
	}

	// 2. Completion bash command succeeds despite broken config
	buf.Reset()
	RootCmd.SetArgs([]string{"--config", brokenCfg, "completion", "bash"})
	if err := RootCmd.Execute(); err != nil {
		t.Errorf("expected 'completion bash' to succeed with broken config, got: %v", err)
	}

	// 3. Telemetry command (inspect) properly fails loading broken config
	buf.Reset()
	RootCmd.SetArgs([]string{"--config", brokenCfg, "inspect", "endpoints", "--mock"})
	if err := RootCmd.Execute(); err == nil {
		t.Errorf("expected 'inspect' to fail with broken config, got nil error")
	}
}

func TestInspectSlowLogsCommand(t *testing.T) {
	resetRootFlags()
	defer resetRootFlags()

	// slow-logs must be registered directly under inspectCmd as its own verb
	found := false
	for _, c := range inspectCmd.Commands() {
		if c.Name() == "slow-logs" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected 'slow-logs' to be a subcommand of 'inspect'")
	}

	buf := new(bytes.Buffer)
	RootCmd.SetOut(buf)
	RootCmd.SetArgs([]string{"inspect", "slow-logs", "1h", "--mock"})
	if err := RootCmd.Execute(); err != nil {
		t.Fatalf("expected 'azlens inspect slow-logs 1h --mock' to succeed, got: %v", err)
	}
}

func TestDefaultResolver(t *testing.T) {
	eff := config.Defaults{
		Profile: "prod",
		Window:  "2h",
		Since:   "45m",
		Limit:   25,
		Output:  "json",
	}
	resolver := NewDefaultResolver(eff)

	// Test ResolveOutput
	dummyCmd := &cobra.Command{Use: "dummy"}
	dummyCmd.Flags().StringP("output", "o", "table", "")

	// 1. Unchanged flag -> falls back to eff.Output
	if got := resolver.ResolveOutput(dummyCmd, "table"); got != "json" {
		t.Errorf("expected 'json' from effective defaults, got %s", got)
	}

	// 2. Changed flag -> flag wins
	_ = dummyCmd.Flags().Set("output", "markdown")
	if got := resolver.ResolveOutput(dummyCmd, "markdown"); got != "markdown" {
		t.Errorf("expected 'markdown' from changed flag, got %s", got)
	}

	// Test ResolveLimit
	limitCmd := &cobra.Command{Use: "limitCmd"}
	limitCmd.Flags().IntP("limit", "n", 15, "")
	if got := resolver.ResolveLimit(limitCmd, 15); got != 25 {
		t.Errorf("expected 25 from effective defaults, got %d", got)
	}
	_ = limitCmd.Flags().Set("limit", "5")
	if got := resolver.ResolveLimit(limitCmd, 5); got != 5 {
		t.Errorf("expected 5 from changed flag, got %d", got)
	}

	// Test ResolveWindow
	start, end, err := resolver.ResolveWindow("")
	if err != nil {
		t.Fatalf("failed resolving window: %v", err)
	}
	if dur := end.Sub(start); dur != 2*time.Hour {
		t.Errorf("expected 2h duration, got %v", dur)
	}

	start, end, err = resolver.ResolveWindow("30m")
	if err != nil {
		t.Fatalf("failed resolving explicit window: %v", err)
	}
	if dur := end.Sub(start); dur != 30*time.Minute {
		t.Errorf("expected 30m duration, got %v", dur)
	}

	// Test ResolveSince
	sinceDur, err := resolver.ResolveSince("")
	if err != nil {
		t.Fatalf("failed resolving since: %v", err)
	}
	if sinceDur != 45*time.Minute {
		t.Errorf("expected 45m since, got %v", sinceDur)
	}

	sinceDur, err = resolver.ResolveSince("15m")
	if err != nil {
		t.Fatalf("failed resolving explicit since: %v", err)
	}
	if sinceDur != 15*time.Minute {
		t.Errorf("expected 15m since, got %v", sinceDur)
	}
}

func TestTimeParsingHelpers(t *testing.T) {
	// parseRelativeDuration
	d1, err := parseRelativeDuration("3d")
	if err != nil || d1 != 72*time.Hour {
		t.Errorf("expected 72h for 3d, got %v (%v)", d1, err)
	}

	d2, err := parseRelativeDuration("15m")
	if err != nil || d2 != 15*time.Minute {
		t.Errorf("expected 15m, got %v (%v)", d2, err)
	}

	if _, err := parseRelativeDuration("invalid"); err == nil {
		t.Errorf("expected error for invalid duration")
	}

	// parseDurationWindow
	s, e, err := parseDurationWindow("")
	if err != nil || e.Sub(s) != time.Hour {
		t.Errorf("expected default 1h for empty window, got %v (%v)", e.Sub(s), err)
	}

	// firstArg
	if got := firstArg([]string{"a", "b"}); got != "a" {
		t.Errorf("expected 'a', got %q", got)
	}
	if got := firstArg(nil); got != "" {
		t.Errorf("expected '', got %q", got)
	}
}

func TestAllInspectSubcommandsWithMock(t *testing.T) {
	subcommands := [][]string{
		{"inspect", "endpoints", "30m", "--mock"},
		{"inspect", "dependencies", "30m", "--mock"},
		{"inspect", "queries", "30m", "--mock"},
		{"inspect", "slow-logs", "30m", "--mock"},
		{"inspect", "slow-logs", "30m", "--grouped", "--mock"},
		{"inspect", "n-plus-one", "30m", "--mock"},
		{"inspect", "breakdown", "30m", "--mock"},
		{"inspect", "errors", "30m", "--mock"},
		{"inspect", "deprecations", "30m", "--mock"},
		{"deploy", "30m", "--mock"},
	}

	for _, args := range subcommands {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			resetRootFlags()
			defer resetRootFlags()

			buf := new(bytes.Buffer)
			RootCmd.SetOut(buf)
			RootCmd.SetArgs(args)
			if err := RootCmd.Execute(); err != nil {
				// deploy --mock compares two healthy windows, so the quality
				// gate must exit 0; any error here is a mock wiring regression
				t.Fatalf("command failed: %v", err)
			}
		})
	}
}

func TestMockDeployVerdictIsHealthy(t *testing.T) {
	resetRootFlags()
	defer resetRootFlags()

	dir := t.TempDir()
	origWd, _ := os.Getwd()
	defer func() { _ = os.Chdir(origWd) }()
	_ = os.Chdir(dir)

	RootCmd.SetArgs([]string{"deploy", "30m", "--mock", "-o", "json"})
	// A non-nil error here would mean the mock data drifted into regression
	// territory (exit code 2), breaking the offline first-run experience
	if err := RootCmd.Execute(); err != nil {
		t.Fatalf("expected healthy mock deploy (exit 0), got: %v", err)
	}
}
