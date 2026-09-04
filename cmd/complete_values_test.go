package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

func TestCompleteServiceValueFromConfig(t *testing.T) {
	resetRootFlags()
	defer resetRootFlags()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "azlens.yaml")
	configYaml := `
version: "1.0"
defaults:
  profile: prod
shared:
  services:
    order:
      role: order-service
      pod: order-app
    billing:
      role: billing-service
      pod: billing-worker
    returns:
      role: returns-service
      pod: returns-app
profiles:
  prod:
    name: "Production"
`
	if err := os.WriteFile(cfgPath, []byte(configYaml), 0644); err != nil {
		t.Fatalf("failed writing config: %v", err)
	}
	configPathFlag = cfgPath

	dummy := &cobra.Command{Use: "dummy"}

	// 1. Services complete with the declared sorted list
	serviceFn := completeServiceValue()
	values, directive := serviceFn(dummy, nil, "")
	if len(values) != 3 || values[0] != "billing" || values[1] != "order" || values[2] != "returns" {
		t.Errorf("expected declared services [billing order returns], got %v", values)
	}
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("expected ShellCompDirectiveNoFileComp, got %v", directive)
	}

	// 2. Prefix filtering applies to the declared services
	if values, _ := serviceFn(dummy, nil, "b"); len(values) != 1 || values[0] != "billing" {
		t.Errorf("expected prefix-filtered service [billing], got %v", values)
	}
}

func TestCompleteServiceValueEmptyWhenNothingDeclared(t *testing.T) {
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

	dummy := &cobra.Command{Use: "dummy"}
	if values, _ := completeServiceValue()(dummy, nil, ""); len(values) != 0 {
		t.Errorf("expected no completions when services are not declared, got %v", values)
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
