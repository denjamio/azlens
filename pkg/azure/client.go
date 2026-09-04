// Package azure implements the Azure telemetry client layer: routing,
// batched KQL execution against the az CLI, and the offline mock provider.
package azure

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/denjamio/azlens/pkg/config"
	"github.com/denjamio/azlens/pkg/kql"
	"github.com/denjamio/azlens/pkg/model"
)

type baselineContextKey struct{}

// WithBaseline marks the context as querying the baseline time window
func WithBaseline(ctx context.Context) context.Context {
	return context.WithValue(ctx, baselineContextKey{}, true)
}

// IsBaseline returns true if the context is for querying the baseline window
func IsBaseline(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, _ := ctx.Value(baselineContextKey{}).(bool)
	return v
}

// AzureClient defines the operations needed by azlens
type AzureClient interface {
	QueryRequestsSummary(ctx context.Context, start, end time.Time) (model.RequestMetric, error)
	QueryEndpoints(ctx context.Context, start, end time.Time, topN int) ([]model.RequestMetric, error)
	QuerySlowDependencies(ctx context.Context, start, end time.Time, depType string, topN int) ([]model.DependencyMetric, error)
	QueryExceptions(ctx context.Context, start, end time.Time, topN int) ([]model.ErrorSummary, error)
	QueryFanout(ctx context.Context, start, end time.Time, topN int) ([]model.FanoutMetric, error)
	QueryLatencyBreakdown(ctx context.Context, start, end time.Time, topN int) ([]model.LatencyBreakdown, error)
	QueryDeprecations(ctx context.Context, start, end time.Time, topN int) ([]model.DeprecationSummary, error)
	QueryMySQLSlowLogs(ctx context.Context, start, end time.Time, dbName string, topN int) ([]model.SlowLogEntry, error)
	QueryMySQLSlowLogsGrouped(ctx context.Context, start, end time.Time, dbName string, topN int) ([]model.SlowLogGroup, error)
	QueryWindowMetrics(ctx context.Context, start, end time.Time, topN int) (model.WindowMetrics, error)
	GetProfile() config.Profile
}

// ClientOptions holds runtime flags and profile configuration
type ClientOptions struct {
	Profile        config.Profile
	IsMock         bool
	PrintQuery     bool
	OnAuthRequired func(tenant string) error
}

// AzTableColumn defines column metadata in a query result table
type AzTableColumn struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// AzQueryTable is a single result table returned by the query API
type AzQueryTable struct {
	Name    string          `json:"name"`
	Columns []AzTableColumn `json:"columns"`
	Rows    [][]interface{} `json:"rows"`
}

// AzQueryResult is the JSON output structure returned by az monitor ... query
type AzQueryResult struct {
	Tables []AzQueryTable `json:"tables"`
}

// AzCliClient implements AzureClient by executing the `az` CLI
type AzCliClient struct {
	opts               ClientOptions
	mu                 sync.Mutex
	activeSubscription string
}

// NewClient returns a new AzureClient instance (real or mock)
func NewClient(opts ClientOptions) AzureClient {
	if opts.IsMock {
		return NewMockClient(opts)
	}
	return &AzCliClient{opts: opts}
}

func (c *AzCliClient) GetProfile() config.Profile {
	return c.opts.Profile
}

// AzureConfigDir returns the azlens-managed az CLI configuration directory for
// an Entra directory (tenant) ID: a fully isolated az profile — accounts, token
// caches and defaults — driven by the documented AZURE_CONFIG_DIR mechanism.
// The user's main az profile is never touched.
func AzureConfigDir(directoryID string) (string, error) {
	directoryID = strings.TrimSpace(directoryID)
	if directoryID == "" {
		return "", nil
	}
	// Prefer XDG_CONFIG_HOME or ~/.config to avoid spaces in paths on macOS
	// (~/Library/Application Support), which breaks shell hints and subshell tools.
	var base string
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		base = xdg
	} else if home, err := os.UserHomeDir(); err == nil && home != "" {
		base = filepath.Join(home, ".config")
	} else {
		var err error
		base, err = os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("failed resolving the user config dir: %w", err)
		}
	}
	return filepath.Join(base, "azlens", "azure", strings.ToLower(directoryID)), nil
}

// AzureExtensionDir returns the directory where the user's primary az CLI extensions
// are installed (defaults to ~/.azure/cliextensions). When AZURE_CONFIG_DIR is isolated,
// pointing AZURE_EXTENSION_DIR to this shared directory ensures extensions like
// log-analytics and application-insights remain accessible without reinstallation.
func AzureExtensionDir() string {
	if dir := os.Getenv("AZURE_EXTENSION_DIR"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".azure", "cliextensions")
}

// routeForTargetQuery selects the target backend (App Insights or Log Analytics)
// and returns the az CLI arguments, target subscription, and directory ID.
func routeForTargetQuery(p config.Profile, tq kql.TargetQuery) ([]string, string, string, error) {
	var args []string
	var targetSub string
	var targetDirectory string

	switch tq.Backend {
	case kql.BackendLogAnalytics:
		if p.Target.Logs.WorkspaceID == "" {
			return nil, "", "", fmt.Errorf("target.logs.workspace_id must be configured in the active profile for Log Analytics queries")
		}
		args = []string{"monitor", "log-analytics", "query", "--workspace", p.Target.Logs.WorkspaceID, "--analytics-query", tq.Query, "-o", "json"}
		targetSub = p.Target.Logs.SubscriptionID
		targetDirectory = p.Target.Logs.DirectoryID
	case kql.BackendAppInsights:
		if p.Target.Insights.Name != "" {
			args = []string{"monitor", "app-insights", "query", "--app", p.Target.Insights.Name, "--analytics-query", tq.Query, "-o", "json"}
			if p.Target.Insights.ResourceGroup != "" {
				args = append(args, "--resource-group", p.Target.Insights.ResourceGroup)
			}
			targetSub = p.Target.Insights.SubscriptionID
			targetDirectory = p.Target.Insights.DirectoryID
		} else if p.Target.Logs.WorkspaceID != "" {
			args = []string{"monitor", "log-analytics", "query", "--workspace", p.Target.Logs.WorkspaceID, "--analytics-query", tq.Query, "-o", "json"}
			targetSub = p.Target.Logs.SubscriptionID
			targetDirectory = p.Target.Logs.DirectoryID
		} else {
			return nil, "", "", fmt.Errorf("either target.insights.name or target.logs.workspace_id must be configured in the active profile")
		}
	default:
		return nil, "", "", fmt.Errorf("unknown query backend: %s", tq.Backend)
	}

	if targetSub != "" {
		args = append(args, "--subscription", targetSub)
	}

	return args, targetSub, targetDirectory, nil
}

// azExtensionForArgs maps the az command groups azlens invokes to the Azure CLI
// extension that provides them: 'az monitor log-analytics query' and
// 'az monitor app-insights query' are extension commands, not core CLI commands.
// Without the extension installed, az reports the group as "misspelled or not
// recognized by the system" (az misspells "misspelled" in that message, so the
// client matches both spellings of its output). The interactive install prompt
// is unavailable in non-interactive subprocesses.
func azExtensionForArgs(args []string) string {
	if len(args) >= 2 && args[0] == "monitor" {
		switch args[1] {
		case "log-analytics":
			return "log-analytics"
		case "app-insights":
			return "application-insights"
		}
	}
	return ""
}

// Query retry policy: transient failures from the Log Analytics / App Insights
// endpoints (timeouts, 502/504, throttling blips) are retried with exponential
// backoff; classified permanent failures (auth, missing resources) fail fast.
const maxQueryAttempts = 3

// retryBaseDelay is the base delay for the exponential backoff (package variable
// so tests can shorten it)
var retryBaseDelay = 750 * time.Millisecond

// permanentQueryErrorMarkers classify az CLI failures that will never succeed
// on retry, so the client fails fast instead of wasting the query budget
var permanentQueryErrorMarkers = []string{
	"azure authentication failed",
	"azure subscription not found",
	"azure resource not found",
	"azure cli command not recognized",
	"mispelled",  //nolint:misspell // az's genuine typo in "'X' is mispelled or not recognized by the system"
	"misspelled", // the correctly spelled variant, in case az fixes its message
}

// isPermanentQueryError reports whether the error is deterministic and must not be retried
func isPermanentQueryError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, exec.ErrNotFound) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	msg := err.Error()
	for _, marker := range permanentQueryErrorMarkers {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

// ensureActiveSubscription switches the active subscription and tenant context in
// the Azure CLI using 'az account set --subscription <id>' when necessary.
// Switching the active subscription updates the local az profile context in milliseconds
// without network overhead, ensuring extensions like log-analytics and app-insights
// acquire access tokens for the correct directory.
func (c *AzCliClient) ensureActiveSubscription(ctx context.Context, subID, tenantID string) error {
	subID = strings.TrimSpace(subID)
	if subID == "" {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if strings.EqualFold(c.activeSubscription, subID) {
		return nil
	}

	args := []string{"account", "set", "--subscription", subID, "--only-show-errors"}

	cmd := exec.CommandContext(ctx, "az", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		outStr := strings.TrimSpace(string(out))
		if c.opts.OnAuthRequired != nil && (strings.Contains(outStr, "login") || strings.Contains(outStr, "AADSTS") || strings.Contains(outStr, "not found") || strings.Contains(outStr, "SubscriptionNotFound") || strings.Contains(outStr, "doesn't exist") || strings.Contains(outStr, "does not exist")) {
			if loginErr := c.opts.OnAuthRequired(tenantID); loginErr == nil {
				cmdRetry := exec.CommandContext(ctx, "az", args...)
				retryOut, retryErr := cmdRetry.CombinedOutput()
				if retryErr == nil {
					c.activeSubscription = subID
					return nil
				}
				outStr = strings.TrimSpace(string(retryOut))
			}
		}
		if tenantID != "" {
			return fmt.Errorf("failed to set active subscription to '%s' (tenant '%s'): %w (output: %s)\n💡 Hint: Run 'az login --tenant %s'", subID, tenantID, err, outStr, tenantID)
		}
		return fmt.Errorf("failed to set active subscription to '%s': %w (output: %s)\n💡 Hint: Run 'az login' to authenticate with Azure", subID, err, outStr)
	}

	c.activeSubscription = subID
	return nil
}

// runAzQuery executes the az CLI with retry and exponential backoff, and parses
// the JSON output. Both windows and individual queries share this budget.
func (c *AzCliClient) runAzQuery(ctx context.Context, args []string) (*AzQueryResult, error) {
	var lastErr error
	for attempt := 1; attempt <= maxQueryAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		res, err := c.runAzQueryOnce(ctx, args)
		if err == nil {
			return res, nil
		}
		lastErr = err

		if isPermanentQueryError(err) || ctx.Err() != nil {
			return nil, err
		}
		if attempt < maxQueryAttempts {
			delay := retryBaseDelay * time.Duration(1<<(attempt-1))
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}
	}
	return nil, lastErr
}

// runAzQueryOnce performs a single az CLI invocation and parses the JSON output
func (c *AzCliClient) runAzQueryOnce(ctx context.Context, args []string) (*AzQueryResult, error) {
	// Suppress az warnings/telemetry notices on stderr so they cannot pollute
	// the JSON output (stderr and stdout are captured together).
	// Global flags like --only-show-errors must be appended after subcommands so
	// Azure CLI / Knack can properly resolve extension command groups (e.g. monitor log-analytics).
	cmdArgs := append(append([]string{}, args...), "--only-show-errors")

	cmd := exec.CommandContext(ctx, "az", cmdArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		outStr := strings.TrimSpace(string(out))
		// Missing az CLI extension: az reports the command group as unknown when
		// the extension providing it is not installed. Guide with the exact fix.
		if strings.Contains(outStr, "mispelled") || //nolint:misspell // az's genuine typo in its output
			strings.Contains(outStr, "misspelled") ||
			strings.Contains(outStr, "not recognized by the system") {
			if ext := azExtensionForArgs(args); ext != "" {
				return nil, fmt.Errorf("azure cli command not recognized (output: %s): 'az %s %s' is provided by the '%s' extension, which is not installed.\n💡 Hint: Run 'az extension add --name %s' and retry", outStr, args[0], args[1], ext, ext)
			}
			return nil, fmt.Errorf("azure cli command not recognized: %s\n💡 Hint: Update the Azure CLI with 'az upgrade' — this command group does not exist in the installed version", outStr)
		}
		if strings.Contains(outStr, "az login") || strings.Contains(outStr, "AADSTS") || strings.Contains(outStr, "expired") {
			return nil, fmt.Errorf("azure authentication failed: session expired or not logged in.\n💡 Hint: Run 'az login --tenant <directory-id>' to authenticate an additional directory — azlens does not store tokens, sessions stay in the az CLI")
		}
		if strings.Contains(outStr, "The subscription of") || strings.Contains(outStr, "SubscriptionNotFound") {
			return nil, fmt.Errorf("azure subscription not found in active account.\n💡 Hint: If App Insights and Log Analytics live in different directories, authenticate to both via 'az login --tenant <tenant-id>' and configure 'insights.subscription_id' and 'logs.subscription_id' (plus 'directory_id') in azlens.yaml")
		}
		if strings.Contains(outStr, "ResourceNotFound") || strings.Contains(outStr, "not found") {
			return nil, fmt.Errorf("azure resource not found: %s\n💡 Hint: For App Insights, verify 'insights.name' in azlens.yaml — if using a component name, specify 'insights.resource_group' (or use the App ID GUID from portal API Access, or full resource ID). For workspace-based App Insights, leave 'insights.name' empty to query 'logs.workspace_id' directly", outStr)
		}
		return nil, fmt.Errorf("azure cli query failed: %w (output: %s)", err, outStr)
	}

	return parseAzQueryOutput(out)
}

// parseAzQueryOutput parses Azure CLI JSON output into an AzQueryResult.
// It flexibly handles:
// 1. Standard object: {"tables": [...]}
// 2. Direct array of tables: [{"name": "...", "columns": [...], "rows": [...]}]
// 3. Array of key-value maps: [{"col1": val1, ...}]
// 4. Empty array: []
func parseAzQueryOutput(out []byte) (*AzQueryResult, error) {
	trimmed := bytes.TrimSpace(out)
	if len(trimmed) == 0 {
		return &AzQueryResult{Tables: []AzQueryTable{}}, nil
	}

	// 1. Standard object: {"tables": [...]}
	var res AzQueryResult
	if err := json.Unmarshal(trimmed, &res); err == nil && res.Tables != nil {
		return &res, nil
	}

	// 2. Direct array of tables: [{"name": "...", "columns": [...], "rows": [...]}] or []
	var tables []AzQueryTable
	if err := json.Unmarshal(trimmed, &tables); err == nil {
		if len(tables) == 0 {
			return &AzQueryResult{Tables: tables}, nil
		}
		hasTableStructure := false
		for _, t := range tables {
			if len(t.Columns) > 0 || len(t.Rows) > 0 {
				hasTableStructure = true
				break
			}
		}
		if hasTableStructure {
			return &AzQueryResult{Tables: tables}, nil
		}
	}

	// 3. Array of key-value maps: [{"col1": val1, ...}]
	var records []map[string]interface{}
	if err := json.Unmarshal(trimmed, &records); err == nil {
		if len(records) == 0 {
			return &AzQueryResult{Tables: []AzQueryTable{}}, nil
		}

		var colNames []string
		seen := make(map[string]bool)
		for _, rec := range records {
			for k := range rec {
				if !seen[k] {
					seen[k] = true
					colNames = append(colNames, k)
				}
			}
		}
		sort.Strings(colNames)

		cols := make([]AzTableColumn, len(colNames))
		for i, col := range colNames {
			cols[i] = AzTableColumn{Name: col, Type: "dynamic"}
		}

		rows := make([][]interface{}, len(records))
		for rIdx, rec := range records {
			row := make([]interface{}, len(colNames))
			for cIdx, col := range colNames {
				row[cIdx] = rec[col]
			}
			rows[rIdx] = row
		}

		return &AzQueryResult{
			Tables: []AzQueryTable{
				{
					Name:    "PrimaryResult",
					Columns: cols,
					Rows:    rows,
				},
			},
		}, nil
	}

	return nil, fmt.Errorf("failed to parse az cli json output: %s", truncateOutput(string(trimmed), 150))
}

func truncateOutput(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}

func (c *AzCliClient) executeKQL(ctx context.Context, tq kql.TargetQuery) (*AzQueryResult, error) {
	if c.opts.PrintQuery {
		fmt.Fprintf(os.Stderr, "\n[azlens:query] Backend: %s\n------------------------------------------------------------\n%s\n------------------------------------------------------------\n", tq.Backend, tq.Query)
	}
	args, targetSub, directoryID, err := routeForTargetQuery(c.opts.Profile, tq)
	if err != nil {
		return nil, err
	}
	if err := c.ensureActiveSubscription(ctx, targetSub, directoryID); err != nil {
		return nil, err
	}
	return c.runAzQuery(ctx, args)
}

// executeKQLBatch runs multiple self-contained KQL statements in a single az CLI
// invocation (semicolon-separated) and returns one result table per statement.
// All statements must target the same backend (single subscription/directory).
func (c *AzCliClient) executeKQLBatch(ctx context.Context, queries []kql.TargetQuery) ([]AzQueryTable, error) {
	if len(queries) == 0 {
		return nil, fmt.Errorf("empty query batch")
	}

	targetBackend := queries[0].Backend
	rawQueries := make([]string, len(queries))
	for i, q := range queries {
		if q.Backend != targetBackend {
			return nil, fmt.Errorf("mixed backend batch is not supported: all queries must target the same backend (%s vs %s)", targetBackend, q.Backend)
		}
		rawQueries[i] = q.Query
	}

	batchedQuery := strings.Join(rawQueries, ";\n\n")
	if c.opts.PrintQuery {
		fmt.Fprintf(os.Stderr, "\n[azlens:batch-query] Backend: %s (%d statements)\n------------------------------------------------------------\n%s\n------------------------------------------------------------\n", targetBackend, len(queries), batchedQuery)
	}

	batchTarget := kql.TargetQuery{
		Query:   batchedQuery,
		Backend: targetBackend,
	}

	args, targetSub, directoryID, err := routeForTargetQuery(c.opts.Profile, batchTarget)
	if err != nil {
		return nil, err
	}

	if err := c.ensureActiveSubscription(ctx, targetSub, directoryID); err != nil {
		return nil, err
	}

	res, err := c.runAzQuery(ctx, args)
	if err != nil {
		return nil, err
	}

	if len(res.Tables) != len(queries) {
		return nil, fmt.Errorf("batch query returned %d tables, expected %d", len(res.Tables), len(queries))
	}

	return res.Tables, nil
}

func (c *AzCliClient) QueryRequestsSummary(ctx context.Context, start, end time.Time) (model.RequestMetric, error) {
	p := c.opts.Profile
	res, err := c.executeKQL(ctx, kql.BuildRequestsSummaryQuery(start, end, p.Target))
	if err != nil {
		return model.RequestMetric{}, err
	}

	if len(res.Tables) == 0 {
		return model.RequestMetric{Name: p.Name}, nil
	}
	return parseOverallRequestTable(&res.Tables[0], p.Name), nil
}

// QueryWindowMetrics fetches all telemetry for one time window in a single batched
// KQL request (overall, endpoints, dependencies, exceptions, fan-out), which keeps
// az CLI process overhead to one spawn per window
func (c *AzCliClient) QueryWindowMetrics(ctx context.Context, start, end time.Time, topN int) (model.WindowMetrics, error) {
	p := c.opts.Profile
	qOverall := kql.BuildRequestsSummaryQuery(start, end, p.Target)
	qEndpoints := kql.BuildEndpointsSummaryQuery(start, end, p.Target, topN)
	qDeps := kql.BuildSlowDependenciesQuery(start, end, p.Target, "", topN)
	qExceptions := kql.BuildExceptionsSummaryQuery(start, end, p.Target, topN)
	qFanout := kql.BuildFanoutSummaryQuery(start, end, p.Target, topN)

	tables, err := c.executeKQLBatch(ctx, []kql.TargetQuery{
		qOverall,
		qEndpoints,
		qDeps,
		qExceptions,
		qFanout,
	})
	if err != nil {
		return model.WindowMetrics{}, err
	}

	if len(tables) < 5 {
		return model.WindowMetrics{}, fmt.Errorf("expected 5 tables from batched query, got %d", len(tables))
	}

	return model.WindowMetrics{
		Overall:   parseOverallRequestTable(&tables[0], p.Name),
		Endpoints: parseEndpointsTable(&tables[1]),
		Deps:      parseDepsTable(&tables[2]),
		Errors:    parseExceptionsTable(&tables[3]),
		Fanout:    parseFanoutTable(&tables[4]),
	}, nil
}

func (c *AzCliClient) QueryEndpoints(ctx context.Context, start, end time.Time, topN int) ([]model.RequestMetric, error) {
	p := c.opts.Profile
	res, err := c.executeKQL(ctx, kql.BuildEndpointsSummaryQuery(start, end, p.Target, topN))
	if err != nil {
		return nil, err
	}

	if len(res.Tables) == 0 {
		return nil, nil
	}
	return parseEndpointsTable(&res.Tables[0]), nil
}

func (c *AzCliClient) QuerySlowDependencies(ctx context.Context, start, end time.Time, depType string, topN int) ([]model.DependencyMetric, error) {
	p := c.opts.Profile
	res, err := c.executeKQL(ctx, kql.BuildSlowDependenciesQuery(start, end, p.Target, depType, topN))
	if err != nil {
		return nil, err
	}

	if len(res.Tables) == 0 {
		return nil, nil
	}
	return parseDepsTable(&res.Tables[0]), nil
}

func (c *AzCliClient) QueryExceptions(ctx context.Context, start, end time.Time, topN int) ([]model.ErrorSummary, error) {
	p := c.opts.Profile
	res, err := c.executeKQL(ctx, kql.BuildExceptionsSummaryQuery(start, end, p.Target, topN))
	if err != nil {
		return nil, err
	}

	if len(res.Tables) == 0 {
		return nil, nil
	}
	return parseExceptionsTable(&res.Tables[0]), nil
}

func (c *AzCliClient) QueryFanout(ctx context.Context, start, end time.Time, topN int) ([]model.FanoutMetric, error) {
	p := c.opts.Profile
	res, err := c.executeKQL(ctx, kql.BuildFanoutSummaryQuery(start, end, p.Target, topN))
	if err != nil {
		return nil, err
	}

	if len(res.Tables) == 0 {
		return nil, nil
	}
	return parseFanoutTable(&res.Tables[0]), nil
}

func (c *AzCliClient) QueryLatencyBreakdown(ctx context.Context, start, end time.Time, topN int) ([]model.LatencyBreakdown, error) {
	p := c.opts.Profile
	res, err := c.executeKQL(ctx, kql.BuildLatencyBreakdownQuery(start, end, p.Target, topN))
	if err != nil {
		return nil, err
	}

	var results []model.LatencyBreakdown
	if len(res.Tables) == 0 {
		return results, nil
	}

	for _, row := range res.Tables[0].Rows {
		if len(row) < 6 {
			continue
		}
		results = append(results, model.LatencyBreakdown{
			Endpoint:       fmt.Sprintf("%v", row[0]),
			AvgDurationMs:  toFloat(row[1]),
			PctDatabase:    toFloat(row[2]),
			PctExternalAPI: toFloat(row[3]),
			PctCache:       toFloat(row[4]),
			PctAppCode:     toFloat(row[5]),
		})
	}
	return results, nil
}

func (c *AzCliClient) QueryDeprecations(ctx context.Context, start, end time.Time, topN int) ([]model.DeprecationSummary, error) {
	p := c.opts.Profile
	res, err := c.executeKQL(ctx, kql.BuildDeprecationsQuery(start, end, p.Target, topN))
	if err != nil {
		return nil, err
	}

	var results []model.DeprecationSummary
	if len(res.Tables) == 0 {
		return results, nil
	}

	for _, row := range res.Tables[0].Rows {
		if len(row) < 5 {
			continue
		}
		var endpoints []string
		if eps, ok := row[4].([]interface{}); ok {
			for _, ep := range eps {
				endpoints = append(endpoints, fmt.Sprintf("%v", ep))
			}
		}

		results = append(results, model.DeprecationSummary{
			Message:           fmt.Sprintf("%v", row[0]),
			Count:             toInt64(row[1]),
			FirstSeen:         parseTime(row[2]),
			LastSeen:          parseTime(row[3]),
			AffectedEndpoints: endpoints,
		})
	}
	return results, nil
}

func (c *AzCliClient) QueryMySQLSlowLogs(ctx context.Context, start, end time.Time, dbName string, topN int) ([]model.SlowLogEntry, error) {
	tq := kql.BuildMySQLSlowLogsQuery(start, end, dbName, topN)
	res, err := c.executeKQL(ctx, tq)
	if err != nil {
		return nil, err
	}
	if len(res.Tables) == 0 {
		return nil, nil
	}
	return parseSlowLogsTable(&res.Tables[0]), nil
}

// QueryMySQLSlowLogsGrouped aggregates slow query logs by normalized SQL
// fingerprint: execution count, duration statistics, and rows examined
func (c *AzCliClient) QueryMySQLSlowLogsGrouped(ctx context.Context, start, end time.Time, dbName string, topN int) ([]model.SlowLogGroup, error) {
	tq := kql.BuildMySQLSlowLogsGroupedQuery(start, end, dbName, topN)
	res, err := c.executeKQL(ctx, tq)
	if err != nil {
		return nil, err
	}
	if len(res.Tables) == 0 {
		return nil, nil
	}
	return parseSlowLogsGroupTable(&res.Tables[0]), nil
}

func parseSlowLogsTable(t *AzQueryTable) []model.SlowLogEntry {
	if len(t.Rows) == 0 {
		return nil
	}
	colMap := make(map[string]int, len(t.Columns))
	for i, col := range t.Columns {
		colMap[strings.ToLower(col.Name)] = i
	}

	getIdx := func(names ...string) int {
		for _, n := range names {
			if idx, ok := colMap[strings.ToLower(n)]; ok {
				return idx
			}
		}
		return -1
	}

	idxTime := getIdx("timegenerated", "timestamp")
	idxDurS := getIdx("duration_s")
	idxDurMs := getIdx("querydurationms", "duration_ms")
	idxExamined := getIdx("rowsexamined", "rows_examined")
	idxSent := getIdx("rowssent", "rows_sent")
	idxSQL := getIdx("sqltext", "sql_text")

	entries := make([]model.SlowLogEntry, 0, len(t.Rows))
	for _, row := range t.Rows {
		entry := model.SlowLogEntry{}
		if idxTime >= 0 && idxTime < len(row) && row[idxTime] != nil {
			str := fmt.Sprintf("%v", row[idxTime])
			if ts, err := time.Parse(time.RFC3339Nano, str); err == nil {
				entry.Timestamp = ts
			} else if ts, err := time.Parse(time.RFC3339, str); err == nil {
				entry.Timestamp = ts
			}
		}
		if idxDurMs >= 0 && idxDurMs < len(row) && row[idxDurMs] != nil {
			entry.DurationMs = toFloat(row[idxDurMs])
		}
		if idxDurS >= 0 && idxDurS < len(row) && row[idxDurS] != nil {
			entry.DurationSec = toFloat(row[idxDurS])
		} else if entry.DurationMs > 0 {
			entry.DurationSec = entry.DurationMs / 1000.0
		}
		if idxExamined >= 0 && idxExamined < len(row) && row[idxExamined] != nil {
			entry.RowsExamined = toInt64(row[idxExamined])
		}
		if idxSent >= 0 && idxSent < len(row) && row[idxSent] != nil {
			entry.RowsSent = toInt64(row[idxSent])
		}
		if idxSQL >= 0 && idxSQL < len(row) && row[idxSQL] != nil {
			entry.SQLText = fmt.Sprintf("%v", row[idxSQL])
		}
		entries = append(entries, entry)
	}
	return entries
}

// parseSlowLogsGroupTable parses slow log rows aggregated by SQL fingerprint
func parseSlowLogsGroupTable(t *AzQueryTable) []model.SlowLogGroup {
	if len(t.Rows) == 0 {
		return nil
	}
	colMap := make(map[string]int, len(t.Columns))
	for i, col := range t.Columns {
		colMap[strings.ToLower(col.Name)] = i
	}

	getIdx := func(names ...string) int {
		for _, n := range names {
			if idx, ok := colMap[strings.ToLower(n)]; ok {
				return idx
			}
		}
		return -1
	}

	idxFingerprint := getIdx("sqlfingerprint")
	idxExecutions := getIdx("executions")
	idxAvgMs := getIdx("avgms")
	idxMaxMs := getIdx("maxms")
	idxTotalMs := getIdx("totalms")
	idxRows := getIdx("avgrowsexamined")
	idxLastSeen := getIdx("lastseen")

	groups := make([]model.SlowLogGroup, 0, len(t.Rows))
	for _, row := range t.Rows {
		group := model.SlowLogGroup{}
		if idxFingerprint >= 0 && idxFingerprint < len(row) && row[idxFingerprint] != nil {
			group.Fingerprint = fmt.Sprintf("%v", row[idxFingerprint])
		}
		if idxExecutions >= 0 && idxExecutions < len(row) {
			group.Executions = toInt64(row[idxExecutions])
		}
		if idxAvgMs >= 0 && idxAvgMs < len(row) {
			group.AvgMs = toFloat(row[idxAvgMs])
		}
		if idxMaxMs >= 0 && idxMaxMs < len(row) {
			group.MaxMs = toFloat(row[idxMaxMs])
		}
		if idxTotalMs >= 0 && idxTotalMs < len(row) {
			group.TotalMs = toFloat(row[idxTotalMs])
		}
		if idxRows >= 0 && idxRows < len(row) {
			group.AvgRowsExamined = toFloat(row[idxRows])
		}
		if idxLastSeen >= 0 && idxLastSeen < len(row) {
			group.LastSeen = parseTime(row[idxLastSeen])
		}
		groups = append(groups, group)
	}
	return groups
}

// Table parsing helpers shared by single-query and batched flows

// parseOverallRequestTable parses the single-row overall requests summary table
func parseOverallRequestTable(t *AzQueryTable, name string) model.RequestMetric {
	if len(t.Rows) == 0 {
		return model.RequestMetric{Name: name}
	}

	row := t.Rows[0]
	if len(row) < 11 {
		return model.RequestMetric{Name: name}
	}

	metric := model.RequestMetric{
		Name:        name,
		TotalCalls:  toInt64(row[0]),
		FailedCalls: toInt64(row[1]),
		ErrorRate:   toFloat(row[10]),
		Latency: model.LatencyPercentiles{
			Avg: toFloat(row[2]),
			Min: toFloat(row[3]),
			Max: toFloat(row[4]),
			P50: toFloat(row[5]),
			P75: toFloat(row[6]),
			P90: toFloat(row[7]),
			P95: toFloat(row[8]),
			P99: toFloat(row[9]),
		},
	}
	if len(row) >= 14 {
		metric.HTTP2xx = toInt64(row[11])
		metric.HTTP4xx = toInt64(row[12])
		metric.HTTP5xx = toInt64(row[13])
	}
	return metric
}

// parseEndpointsTable parses per-endpoint request percentile rows
func parseEndpointsTable(t *AzQueryTable) []model.RequestMetric {
	var results []model.RequestMetric
	for _, row := range t.Rows {
		if len(row) < 12 {
			continue
		}
		results = append(results, model.RequestMetric{
			Name:        fmt.Sprintf("%v", row[0]),
			TotalCalls:  toInt64(row[1]),
			FailedCalls: toInt64(row[2]),
			ErrorRate:   toFloat(row[11]),
			Latency: model.LatencyPercentiles{
				Avg: toFloat(row[3]),
				Min: toFloat(row[4]),
				Max: toFloat(row[5]),
				P50: toFloat(row[6]),
				P75: toFloat(row[7]),
				P90: toFloat(row[8]),
				P95: toFloat(row[9]),
				P99: toFloat(row[10]),
			},
		})
	}
	return results
}

// parseDepsTable parses slow dependency rows (SQL, HTTP, Redis, ...)
func parseDepsTable(t *AzQueryTable) []model.DependencyMetric {
	var results []model.DependencyMetric
	for _, row := range t.Rows {
		if len(row) < 13 {
			continue
		}
		results = append(results, model.DependencyMetric{
			Type:        fmt.Sprintf("%v", row[0]),
			Target:      fmt.Sprintf("%v", row[1]),
			Name:        fmt.Sprintf("%v", row[2]),
			TotalCalls:  toInt64(row[3]),
			FailedCalls: toInt64(row[4]),
			ErrorRate:   toFloat(row[12]),
			Latency: model.LatencyPercentiles{
				Avg: toFloat(row[5]),
				Min: toFloat(row[6]),
				Max: toFloat(row[7]),
				P50: toFloat(row[8]),
				P90: toFloat(row[9]),
				P95: toFloat(row[10]),
				P99: toFloat(row[11]),
			},
		})
	}
	return results
}

// parseExceptionsTable parses grouped exception rows
func parseExceptionsTable(t *AzQueryTable) []model.ErrorSummary {
	var results []model.ErrorSummary
	for _, row := range t.Rows {
		if len(row) < 5 {
			continue
		}
		results = append(results, model.ErrorSummary{
			Type:      fmt.Sprintf("%v", row[0]),
			Message:   fmt.Sprintf("%v", row[1]),
			Count:     toInt64(row[2]),
			FirstSeen: parseTime(row[3]),
			LastSeen:  parseTime(row[4]),
		})
	}
	return results
}

// parseFanoutTable parses N+1 / database fan-out rows
func parseFanoutTable(t *AzQueryTable) []model.FanoutMetric {
	var results []model.FanoutMetric
	for _, row := range t.Rows {
		if len(row) < 6 {
			continue
		}
		results = append(results, model.FanoutMetric{
			Endpoint:              fmt.Sprintf("%v", row[0]),
			TotalRequests:         toInt64(row[1]),
			AvgSQLCalls:           toFloat(row[2]),
			MaxSQLCalls:           toInt64(row[3]),
			AvgSQLDurationMs:      toFloat(row[4]),
			AvgEndpointDurationMs: toFloat(row[5]),
		})
	}
	return results
}

// warnUnexpectedValue surfaces non-numeric cell values instead of silently
// coercing them to zero (anti-silent fallback, consistent with parseTime)
func warnUnexpectedValue(v interface{}) {
	fmt.Fprintf(os.Stderr, "⚠️  Warning: expected numeric value, got %T (%v); using 0\n", v, v)
}

func toInt64(v interface{}) int64 {
	switch val := v.(type) {
	case float64:
		return int64(val)
	case int64:
		return val
	case int:
		return int64(val)
	case string:
		if n, err := strconv.ParseInt(strings.TrimSpace(val), 10, 64); err == nil {
			return n
		}
		warnUnexpectedValue(v)
		return 0
	case nil:
		return 0 // KQL NULL (e.g. percentiles over an empty set)
	default:
		warnUnexpectedValue(v)
		return 0
	}
}

func toFloat(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(val), 64); err == nil {
			return f
		}
		warnUnexpectedValue(v)
		return 0.0
	case nil:
		return 0.0 // KQL NULL (e.g. percentiles over an empty set)
	default:
		warnUnexpectedValue(v)
		return 0.0
	}
}

func parseTime(v interface{}) time.Time {
	if v == nil {
		return time.Time{}
	}
	s, ok := v.(string)
	if !ok {
		fmt.Fprintf(os.Stderr, "⚠️  Warning: expected string timestamp, got %T (%v)\n", v, v)
		return time.Time{}
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.999999999Z07:00",
		"2006-01-02T15:04:05.999999999",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t
		}
	}
	fmt.Fprintf(os.Stderr, "⚠️  Warning: failed to parse timestamp %q (anti-silent fallback: invalid time format)\n", s)
	return time.Time{}
}
