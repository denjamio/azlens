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

// fakeAzScript writes an az replacement that fails (exit 1) for the first
// `failTimes` invocations with the given message, then emits the payload
func fakeAzFlakyScript(t *testing.T, counterPath, failMessage, payload string, failTimes int) string {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\n" +
		"n=$(cat " + counterPath + " 2>/dev/null || echo 0)\n" +
		"n=$((n+1))\n" +
		"echo $n > " + counterPath + "\n" +
		"if [ $n -le " + itoa(failTimes) + " ]; then echo '" + failMessage + "' >&2; exit 1; fi\n" +
		"echo '" + payload + "'\n"
	path := filepath.Join(dir, "az")
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("failed writing fake az script: %v", err)
	}
	return dir
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}

func TestRunAzQueryRetriesTransientFailures(t *testing.T) {
	oldDelay := retryBaseDelay
	retryBaseDelay = time.Millisecond
	defer func() { retryBaseDelay = oldDelay }()

	counter := filepath.Join(t.TempDir(), "count")
	payload := `{"tables":[{"name":"PrimaryResult","columns":[{"name":"c0","type":"real"}],"rows":[[100,2,50,40,200,60,80,120,180,300,1.2,98,1,1]]}]}`
	t.Setenv("PATH", fakeAzFlakyScript(t, counter, "502 Bad Gateway", payload, 2)+string(os.PathListSeparator)+os.Getenv("PATH"))

	client := &AzCliClient{opts: ClientOptions{Profile: config.Profile{Target: config.TargetConfig{InsightsName: "app", RoleName: "order-service"}}}}
	now := time.Now()

	metric, err := client.QueryRequestsSummary(context.Background(), now.Add(-time.Hour), now)
	if err != nil {
		t.Fatalf("expected transient failures to be retried, got: %v", err)
	}
	if metric.TotalCalls != 100 {
		t.Errorf("expected 100 calls, got %d", metric.TotalCalls)
	}

	invocations, err := os.ReadFile(counter)
	if err != nil || strings.TrimSpace(string(invocations)) != "3" {
		t.Errorf("expected exactly 3 az invocations (2 transient + 1 success), got: %s (%v)", invocations, err)
	}
}

func TestRunAzQueryFailsFastOnPermanentError(t *testing.T) {
	oldDelay := retryBaseDelay
	retryBaseDelay = time.Millisecond
	defer func() { retryBaseDelay = oldDelay }()

	counter := filepath.Join(t.TempDir(), "count")
	t.Setenv("PATH", fakeAzFlakyScript(t, counter, "ERROR: Please run az login", "unused", 99)+string(os.PathListSeparator)+os.Getenv("PATH"))

	client := &AzCliClient{opts: ClientOptions{Profile: config.Profile{Target: config.TargetConfig{InsightsName: "app", RoleName: "order-service"}}}}
	now := time.Now()

	_, err := client.QueryRequestsSummary(context.Background(), now.Add(-time.Hour), now)
	if err == nil || !strings.Contains(err.Error(), "azure authentication failed") {
		t.Fatalf("expected permanent auth error without retries, got: %v", err)
	}

	invocations, err := os.ReadFile(counter)
	if err != nil || strings.TrimSpace(string(invocations)) != "1" {
		t.Errorf("permanent errors must not be retried, got %s invocations (%v)", invocations, err)
	}
}
