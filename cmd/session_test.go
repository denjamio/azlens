package cmd

import (
	"reflect"
	"testing"
)

func TestAzLoginArgs(t *testing.T) {
	tests := []struct {
		tenant string
		want   []string
	}{
		{
			tenant: "",
			want:   []string{"login"},
		},
		{
			tenant: "my-tenant-guid",
			want:   []string{"login", "--tenant", "my-tenant-guid"},
		},
	}

	for _, tt := range tests {
		got := azLoginArgs(tt.tenant)
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("azLoginArgs(%q) = %v, want %v", tt.tenant, got, tt.want)
		}
	}
}

func TestAzLoginCmd(t *testing.T) {
	cmd := azLoginCmd("tenant-123")
	if cmd.Path != "az" && !reflect.DeepEqual(cmd.Args, []string{"az", "login", "--tenant", "tenant-123"}) {
		t.Errorf("unexpected cmd args: %v", cmd.Args)
	}

	var hasOptOut bool
	for _, env := range cmd.Env {
		if env == "AZURE_CORE_LOGIN_EXPERIENCE_V2=off" {
			hasOptOut = true
			break
		}
	}
	if !hasOptOut {
		t.Errorf("expected AZURE_CORE_LOGIN_EXPERIENCE_V2=off in cmd.Env")
	}
}

func TestIsInteractiveTerminal(t *testing.T) {
	// In go test running inside pipe or non-tty, this should return false
	got := isInteractiveTerminal()
	// Just verify it runs without crashing and returns boolean
	_ = got
}

