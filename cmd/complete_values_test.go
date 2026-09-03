package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

func TestCompleteTelemetryValueFromConfig(t *testing.T) {
	resetRootFlags()
	defer resetRootFlags()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "azlens.yaml")
	configYaml := `
version: "1.0"
defaults:
  profile: prod
shared:
  roles: [order-service, billing-service, returns-service]
  pods: [order-service, billing-worker]
profiles:
  prod:
    name: "Production"
`
	if err := os.WriteFile(cfgPath, []byte(configYaml), 0644); err != nil {
		t.Fatalf("failed writing config: %v", err)
	}
	configPathFlag = cfgPath

	dummy := &cobra.Command{Use: "dummy"}

	// 1. Roles complete with the declared list
	roleFn := completeTelemetryValue("role")
	values, directive := roleFn(dummy, nil, "")
	if len(values) != 3 || values[0] != "order-service" {
		t.Errorf("expected declared roles, got %v", values)
	}
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("expected ShellCompDirectiveNoFileComp, got %v", directive)
	}

	// 2. Pods complete with the declared list
	podFn := completeTelemetryValue("pod")
	if values, _ := podFn(dummy, nil, ""); len(values) != 2 {
		t.Errorf("expected declared pods, got %v", values)
	}

	// 3. Prefix filtering applies to the declared values
	if values, _ := roleFn(dummy, nil, "b"); len(values) != 1 || values[0] != "billing-service" {
		t.Errorf("expected prefix-filtered roles, got %v", values)
	}
}

func TestCompleteTelemetryValueEmptyWhenNothingDeclared(t *testing.T) {
	resetRootFlags()
	defer resetRootFlags()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "azlens.yaml")
	configYaml := `
version: "1.0"
defaults:
  profile: prod
profiles:
  prod:
    name: "Production"
`
	if err := os.WriteFile(cfgPath, []byte(configYaml), 0644); err != nil {
		t.Fatalf("failed writing config: %v", err)
	}
	configPathFlag = cfgPath

	// Nothing declared: nothing completed (single path, no live discovery)
	dummy := &cobra.Command{Use: "dummy"}
	if values, _ := completeTelemetryValue("role")(dummy, nil, ""); values != nil {
		t.Errorf("expected no completions when roles are not declared, got %v", values)
	}
	if values, _ := completeTelemetryValue("pod")(dummy, nil, ""); values != nil {
		t.Errorf("expected no completions when pods are not declared, got %v", values)
	}
}

func TestPrefixFilter(t *testing.T) {
	values := []string{"order-service", "billing-service", "returns-service"}
	got := prefixFilter(values, "o")
	if len(got) != 1 || got[0] != "order-service" {
		t.Errorf("expected [order-service], got %v", got)
	}
	if got := prefixFilter(values, ""); len(got) != 3 {
		t.Errorf("expected all values on empty prefix, got %v", got)
	}
	if got := prefixFilter(values, "nope"); got != nil {
		t.Errorf("expected nil on no match, got %v", got)
	}
}
