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
	QueryNPlusOneCandidates(ctx context.Context, start, end time.Time, topN int) ([]model.NPlusOneCandidate, error)
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
	Debug          bool
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

// routeForTargetQuery selects the target backend (App Insights or Log Analytics)
// and returns the az CLI arguments, target subscription, and directory ID.
func routeForTargetQuery(p config.Profile, tq kql.TargetQuery) ([]string, string, string, error) {
	var args []string
	var targetSub string
	var targetDirectory string

	switch tq.Backend {
	case kql.BackendLogAnalytics:
		if p.Target.Logs.WorkspaceID == "" {
			return nil, "", "", fmt.Errorf("logs.workspace_id must be configured in the active profile for Log Analytics queries")
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
			return nil, "", "", fmt.Errorf("either insights.name or logs.workspace_id must be configured in the active profile")
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

// isMisspelledCommandError reports whether az CLI returned its characteristic
// "misspelled or not recognized" error (including az CLI's historic typo)
func isMisspelledCommandError(msg string) bool {
	return strings.Contains(msg, "mispelled") || //nolint:misspell // az's genuine typo in "'X' is mispelled or not recognized by the system"
		strings.Contains(msg, "misspelled") ||
		strings.Contains(msg, "not recognized by the system")
}

// permanentQueryErrorMarkers classify az CLI failures that will never succeed
// on retry, so the client fails fast instead of wasting the query budget
var permanentQueryErrorMarkers = []string{
	"azure authentication failed",
	"azure subscription not found",
	"azure resource not found",
	"azure cli command not recognized",
}

// isPermanentQueryError reports whether the error is deterministic and must not be retried
func isPermanentQueryError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, exec.ErrNotFound) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	msg := strings.ToLower(err.Error())
	if isMisspelledCommandError(msg) {
		return true
	}
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
		if c.opts.Debug {
			fmt.Fprintf(os.Stderr, "[azlens:debug] Azure CLI error: %v\n[azlens:debug] Raw CLI output:\n%s\n", err, outStr)
		}
		// Missing az CLI extension: az reports the command group as unknown when
		// the extension providing it is not installed. Guide with the exact fix.
		if isMisspelledCommandError(outStr) {
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
		if strings.Contains(outStr, "ResourceNotFound") || strings.Contains(outStr, "ResourceGroupNotFound") || strings.Contains(outStr, "resource not found") || strings.Contains(outStr, "could not be found") || strings.Contains(outStr, "was not found") {
			return nil, fmt.Errorf("azure resource not found: %s\n💡 Hint: For App Insights, verify 'insights.name' in azlens.yaml — if using a component name, specify 'insights.resource_group' (or use the App ID GUID from portal API Access, or full resource ID). For workspace-based App Insights, leave 'insights.name' empty to query 'logs.workspace_id' directly", outStr)
		}
		return nil, fmt.Errorf("azure cli query failed: %w (output: %s)", err, outStr)
	}

	res, err := parseAzQueryOutput(out)
	if err != nil {
		if c.opts.Debug {
			fmt.Fprintf(os.Stderr, "[azlens:debug] Failed to parse Azure CLI JSON output: %v\n[azlens:debug] Raw CLI output:\n%s\n", err, string(out))
		}
		return nil, err
	}
	return res, nil
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
	if tq.Err != nil {
		return nil, tq.Err
	}
	if c.opts.PrintQuery || c.opts.Debug {
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
		if q.Err != nil {
			return nil, q.Err
		}
		if q.Backend != targetBackend {
			return nil, fmt.Errorf("mixed backend batch is not supported: all queries must target the same backend (%s vs %s)", targetBackend, q.Backend)
		}
		rawQueries[i] = q.Query
	}

	batchedQuery := strings.Join(rawQueries, ";\n\n")
	if c.opts.PrintQuery || c.opts.Debug {
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
// KQL request (overall, endpoints, dependencies, exceptions, fan-out, n+1), which keeps
// az CLI process overhead to one spawn per window
func (c *AzCliClient) QueryWindowMetrics(ctx context.Context, start, end time.Time, topN int) (model.WindowMetrics, error) {
	p := c.opts.Profile
	qOverall := kql.BuildRequestsSummaryQuery(start, end, p.Target)
	qEndpoints := kql.BuildEndpointsSummaryQuery(start, end, p.Target, topN)
	qDeps := kql.BuildSlowDependenciesQuery(start, end, p.Target, "", topN)
	qExceptions := kql.BuildExceptionsSummaryQuery(start, end, p.Target, topN)
	qFanout := kql.BuildFanoutSummaryQuery(start, end, p.Target, topN)
	qNPlusOne := kql.BuildNPlusOneCandidateQuery(start, end, p.Target, topN)

	tables, err := c.executeKQLBatch(ctx, []kql.TargetQuery{
		qOverall,
		qEndpoints,
		qDeps,
		qExceptions,
		qFanout,
		qNPlusOne,
	})
	if err != nil {
		return model.WindowMetrics{}, err
	}

	if len(tables) < 5 {
		return model.WindowMetrics{}, fmt.Errorf("expected at least 5 tables from batched query, got %d", len(tables))
	}

	wm := model.WindowMetrics{
		Overall:   parseOverallRequestTable(&tables[0], p.Name),
		Endpoints: parseEndpointsTable(&tables[1]),
		Deps:      parseDepsTable(&tables[2]),
		Errors:    parseExceptionsTable(&tables[3]),
		Fanout:    parseFanoutTable(&tables[4]),
	}
	if len(tables) >= 6 {
		wm.NPlusOne = parseNPlusOneTable(&tables[5])
	}
	return wm, nil
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

func (c *AzCliClient) QueryNPlusOneCandidates(ctx context.Context, start, end time.Time, topN int) ([]model.NPlusOneCandidate, error) {
	p := c.opts.Profile
	res, err := c.executeKQL(ctx, kql.BuildNPlusOneCandidateQuery(start, end, p.Target, topN))
	if err != nil {
		return nil, err
	}
	if len(res.Tables) == 0 {
		return nil, nil
	}
	return parseNPlusOneTable(&res.Tables[0]), nil
}

func (c *AzCliClient) QueryLatencyBreakdown(ctx context.Context, start, end time.Time, topN int) ([]model.LatencyBreakdown, error) {
	p := c.opts.Profile
	res, err := c.executeKQL(ctx, kql.BuildLatencyBreakdownQuery(start, end, p.Target, topN))
	if err != nil {
		return nil, err
	}
	if len(res.Tables) == 0 {
		return nil, nil
	}
	return parseLatencyBreakdownTable(&res.Tables[0]), nil
}

func (c *AzCliClient) QueryDeprecations(ctx context.Context, start, end time.Time, topN int) ([]model.DeprecationSummary, error) {
	p := c.opts.Profile
	res, err := c.executeKQL(ctx, kql.BuildDeprecationsQuery(start, end, p.Target, topN))
	if err != nil {
		return nil, err
	}
	if len(res.Tables) == 0 {
		return nil, nil
	}
	return parseDeprecationsTable(&res.Tables[0]), nil
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

// tableDecoder provides fast, safe, case-insensitive column-based row decoding
// eliminating positional index coupling while retaining safe backward-compatible fallbacks
type tableDecoder struct {
	colMap map[string]int
}

func newTableDecoder(t *AzQueryTable) tableDecoder {
	if t == nil {
		return tableDecoder{colMap: make(map[string]int)}
	}
	m := make(map[string]int, len(t.Columns))
	for i, col := range t.Columns {
		m[strings.ToLower(strings.TrimSpace(col.Name))] = i
	}
	return tableDecoder{colMap: m}
}

// col returns the index of the first matching column name, or -1 if not found
func (d tableDecoder) col(names ...string) int {
	for _, n := range names {
		if idx, ok := d.colMap[strings.ToLower(n)]; ok {
			return idx
		}
	}
	return -1
}

// stringVal extracts a string from row by column index, with optional fallback index
func (d tableDecoder) stringVal(row []interface{}, colIdx int, fallbackIdx int) string {
	if colIdx >= 0 && colIdx < len(row) && row[colIdx] != nil {
		return fmt.Sprintf("%v", row[colIdx])
	}
	if fallbackIdx >= 0 && fallbackIdx < len(row) && row[fallbackIdx] != nil {
		return fmt.Sprintf("%v", row[fallbackIdx])
	}
	return ""
}

// int64Val extracts an int64 from row by column index, with optional fallback index
func (d tableDecoder) int64Val(row []interface{}, colIdx int, fallbackIdx int) int64 {
	if colIdx >= 0 && colIdx < len(row) && row[colIdx] != nil {
		return toInt64(row[colIdx])
	}
	if fallbackIdx >= 0 && fallbackIdx < len(row) && row[fallbackIdx] != nil {
		return toInt64(row[fallbackIdx])
	}
	return 0
}

// floatVal extracts a float64 from row by column index, with optional fallback index
func (d tableDecoder) floatVal(row []interface{}, colIdx int, fallbackIdx int) float64 {
	if colIdx >= 0 && colIdx < len(row) && row[colIdx] != nil {
		return toFloat(row[colIdx])
	}
	if fallbackIdx >= 0 && fallbackIdx < len(row) && row[fallbackIdx] != nil {
		return toFloat(row[fallbackIdx])
	}
	return 0.0
}

// boolVal extracts a bool from row by column index, with optional fallback index
func (d tableDecoder) boolVal(row []interface{}, colIdx int, fallbackIdx int) bool {
	var raw interface{}
	if colIdx >= 0 && colIdx < len(row) {
		raw = row[colIdx]
	} else if fallbackIdx >= 0 && fallbackIdx < len(row) {
		raw = row[fallbackIdx]
	}
	if raw == nil {
		return false
	}
	switch b := raw.(type) {
	case bool:
		return b
	case string:
		return strings.EqualFold(b, "true") || b == "1"
	case float64:
		return b > 0
	case int64:
		return b > 0
	case int:
		return b > 0
	}
	return false
}

// timeVal extracts a time.Time from row by column index, with optional fallback index
func (d tableDecoder) timeVal(row []interface{}, colIdx int, fallbackIdx int) time.Time {
	if colIdx >= 0 && colIdx < len(row) && row[colIdx] != nil {
		return parseTime(row[colIdx])
	}
	if fallbackIdx >= 0 && fallbackIdx < len(row) && row[fallbackIdx] != nil {
		return parseTime(row[fallbackIdx])
	}
	return time.Time{}
}

// stringSliceVal extracts a []string from row by column index, with optional fallback index
func (d tableDecoder) stringSliceVal(row []interface{}, colIdx int, fallbackIdx int) []string {
	var raw interface{}
	if colIdx >= 0 && colIdx < len(row) {
		raw = row[colIdx]
	} else if fallbackIdx >= 0 && fallbackIdx < len(row) {
		raw = row[fallbackIdx]
	}
	if raw == nil {
		return nil
	}
	switch s := raw.(type) {
	case []interface{}:
		res := make([]string, 0, len(s))
		for _, item := range s {
			res = append(res, fmt.Sprintf("%v", item))
		}
		return res
	case []string:
		return s
	}
	return nil
}

func parseLatencyBreakdownTable(t *AzQueryTable) []model.LatencyBreakdown {
	if t == nil || len(t.Rows) == 0 {
		return nil
	}
	dec := newTableDecoder(t)
	idxEndpoint := dec.col("endpoint")
	idxAvgTotal := dec.col("avgtotalms", "avgtotal", "duration")
	idxDB := dec.col("pctdatabase", "pctdb")
	idxExt := dec.col("pctexternalapi", "pctext")
	idxCache := dec.col("pctcache")
	idxRes := dec.col("pctresidual", "pctappcode")
	idxOverlap := dec.col("hasoverlap")

	results := make([]model.LatencyBreakdown, 0, len(t.Rows))
	for _, row := range t.Rows {
		if len(row) < 2 {
			continue
		}
		resVal := dec.floatVal(row, idxRes, 5)
		results = append(results, model.LatencyBreakdown{
			Endpoint:       dec.stringVal(row, idxEndpoint, 0),
			AvgDurationMs:  dec.floatVal(row, idxAvgTotal, 1),
			PctDatabase:    dec.floatVal(row, idxDB, 2),
			PctExternalAPI: dec.floatVal(row, idxExt, 3),
			PctCache:       dec.floatVal(row, idxCache, 4),
			PctResidual:    resVal,
			PctAppCode:     resVal,
			HasOverlap:     dec.boolVal(row, idxOverlap, 6),
		})
	}
	return results
}

func parseDeprecationsTable(t *AzQueryTable) []model.DeprecationSummary {
	if t == nil || len(t.Rows) == 0 {
		return nil
	}
	dec := newTableDecoder(t)
	idxMsg := dec.col("message", "samplemessage")
	idxCount := dec.col("count")
	idxFirst := dec.col("firstseen")
	idxLast := dec.col("lastseen")
	idxEndpoints := dec.col("affectedendpoints", "endpoints")

	results := make([]model.DeprecationSummary, 0, len(t.Rows))
	for _, row := range t.Rows {
		if len(row) < 2 {
			continue
		}
		results = append(results, model.DeprecationSummary{
			Message:           dec.stringVal(row, idxMsg, 0),
			Count:             dec.int64Val(row, idxCount, 1),
			FirstSeen:         dec.timeVal(row, idxFirst, 2),
			LastSeen:          dec.timeVal(row, idxLast, 3),
			AffectedEndpoints: dec.stringSliceVal(row, idxEndpoints, 4),
		})
	}
	return results
}

func parseSlowLogsTable(t *AzQueryTable) []model.SlowLogEntry {
	if t == nil || len(t.Rows) == 0 {
		return nil
	}
	dec := newTableDecoder(t)
	idxTime := dec.col("timegenerated", "timestamp")
	idxDurS := dec.col("duration_s")
	idxDurMs := dec.col("querydurationms", "duration_ms")
	idxExamined := dec.col("rowsexamined", "rows_examined")
	idxSent := dec.col("rowssent", "rows_sent")
	idxSQL := dec.col("sqltext", "sql_text")

	entries := make([]model.SlowLogEntry, 0, len(t.Rows))
	for _, row := range t.Rows {
		durMs := dec.floatVal(row, idxDurMs, -1)
		durS := dec.floatVal(row, idxDurS, -1)
		if durS == 0 && durMs > 0 {
			durS = durMs / 1000.0
		}
		entries = append(entries, model.SlowLogEntry{
			Timestamp:    dec.timeVal(row, idxTime, 0),
			DurationMs:   durMs,
			DurationSec:  durS,
			RowsExamined: dec.int64Val(row, idxExamined, -1),
			RowsSent:     dec.int64Val(row, idxSent, -1),
			SQLText:      dec.stringVal(row, idxSQL, -1),
		})
	}
	return entries
}

// parseSlowLogsGroupTable parses slow log rows aggregated by SQL fingerprint
func parseSlowLogsGroupTable(t *AzQueryTable) []model.SlowLogGroup {
	if t == nil || len(t.Rows) == 0 {
		return nil
	}
	dec := newTableDecoder(t)
	idxFingerprint := dec.col("sqlfingerprint")
	idxExecutions := dec.col("executions")
	idxAvgMs := dec.col("avgms")
	idxMaxMs := dec.col("maxms")
	idxTotalMs := dec.col("totalms")
	idxRows := dec.col("avgrowsexamined")
	idxLastSeen := dec.col("lastseen")

	groups := make([]model.SlowLogGroup, 0, len(t.Rows))
	for _, row := range t.Rows {
		groups = append(groups, model.SlowLogGroup{
			Fingerprint:     dec.stringVal(row, idxFingerprint, 0),
			Executions:      dec.int64Val(row, idxExecutions, 1),
			AvgMs:           dec.floatVal(row, idxAvgMs, 2),
			MaxMs:           dec.floatVal(row, idxMaxMs, 3),
			TotalMs:         dec.floatVal(row, idxTotalMs, 4),
			AvgRowsExamined: dec.floatVal(row, idxRows, 5),
			LastSeen:        dec.timeVal(row, idxLastSeen, 6),
		})
	}
	return groups
}

// parseOverallRequestTable parses the single-row overall requests summary table
func parseOverallRequestTable(t *AzQueryTable, name string) model.RequestMetric {
	if t == nil || len(t.Rows) == 0 {
		return model.RequestMetric{Name: name}
	}
	row := t.Rows[0]
	dec := newTableDecoder(t)

	idxTotal := dec.col("totalcalls", "count")
	idxFailed := dec.col("failedcalls", "failed")
	idxAvg := dec.col("avgduration", "avg")
	idxMin := dec.col("minduration", "min")
	idxMax := dec.col("maxduration", "max")
	idxP50 := dec.col("p50")
	idxP75 := dec.col("p75")
	idxP90 := dec.col("p90")
	idxP95 := dec.col("p95")
	idxP99 := dec.col("p99")
	idxErr := dec.col("errorrate", "errrate")
	idx2xx := dec.col("http_2xx", "http2xx")
	idx4xx := dec.col("http_4xx", "http4xx")
	idx5xx := dec.col("http_5xx", "http5xx")

	return model.RequestMetric{
		Name:        name,
		TotalCalls:  dec.int64Val(row, idxTotal, 0),
		FailedCalls: dec.int64Val(row, idxFailed, 1),
		ErrorRate:   dec.floatVal(row, idxErr, 10),
		Latency: model.LatencyPercentiles{
			Avg: dec.floatVal(row, idxAvg, 2),
			Min: dec.floatVal(row, idxMin, 3),
			Max: dec.floatVal(row, idxMax, 4),
			P50: dec.floatVal(row, idxP50, 5),
			P75: dec.floatVal(row, idxP75, 6),
			P90: dec.floatVal(row, idxP90, 7),
			P95: dec.floatVal(row, idxP95, 8),
			P99: dec.floatVal(row, idxP99, 9),
		},
		HTTP2xx: dec.int64Val(row, idx2xx, 11),
		HTTP4xx: dec.int64Val(row, idx4xx, 12),
		HTTP5xx: dec.int64Val(row, idx5xx, 13),
	}
}

// parseEndpointsTable parses per-endpoint request percentile rows
func parseEndpointsTable(t *AzQueryTable) []model.RequestMetric {
	if t == nil || len(t.Rows) == 0 {
		return nil
	}
	dec := newTableDecoder(t)
	idxName := dec.col("name", "endpoint", "operation_name")
	idxTotal := dec.col("totalcalls", "count")
	idxFailed := dec.col("failedcalls", "failed")
	idxAvg := dec.col("avgduration", "avg")
	idxMin := dec.col("minduration", "min")
	idxMax := dec.col("maxduration", "max")
	idxP50 := dec.col("p50")
	idxP75 := dec.col("p75")
	idxP90 := dec.col("p90")
	idxP95 := dec.col("p95")
	idxP99 := dec.col("p99")
	idxErr := dec.col("errorrate", "errrate")

	results := make([]model.RequestMetric, 0, len(t.Rows))
	for _, row := range t.Rows {
		if len(row) < 3 {
			continue
		}
		results = append(results, model.RequestMetric{
			Name:        dec.stringVal(row, idxName, 0),
			TotalCalls:  dec.int64Val(row, idxTotal, 1),
			FailedCalls: dec.int64Val(row, idxFailed, 2),
			ErrorRate:   dec.floatVal(row, idxErr, 11),
			Latency: model.LatencyPercentiles{
				Avg: dec.floatVal(row, idxAvg, 3),
				Min: dec.floatVal(row, idxMin, 4),
				Max: dec.floatVal(row, idxMax, 5),
				P50: dec.floatVal(row, idxP50, 6),
				P75: dec.floatVal(row, idxP75, 7),
				P90: dec.floatVal(row, idxP90, 8),
				P95: dec.floatVal(row, idxP95, 9),
				P99: dec.floatVal(row, idxP99, 10),
			},
		})
	}
	return results
}

// parseDepsTable parses slow dependency rows (SQL, HTTP, Redis, ...)
func parseDepsTable(t *AzQueryTable) []model.DependencyMetric {
	if t == nil || len(t.Rows) == 0 {
		return nil
	}
	dec := newTableDecoder(t)
	idxType := dec.col("type")
	idxTarget := dec.col("target")
	idxName := dec.col("dependency", "name")
	idxTotal := dec.col("totalcalls", "count")
	idxFailed := dec.col("failedcalls", "failed")
	idxAvg := dec.col("avgduration", "avg")
	idxMin := dec.col("minduration", "min")
	idxMax := dec.col("maxduration", "max")
	idxP50 := dec.col("p50")
	idxP90 := dec.col("p90")
	idxP95 := dec.col("p95")
	idxP99 := dec.col("p99")
	idxErr := dec.col("errorrate", "errrate")

	results := make([]model.DependencyMetric, 0, len(t.Rows))
	for _, row := range t.Rows {
		if len(row) < 5 {
			continue
		}
		results = append(results, model.DependencyMetric{
			Type:        dec.stringVal(row, idxType, 0),
			Target:      dec.stringVal(row, idxTarget, 1),
			Name:        dec.stringVal(row, idxName, 2),
			TotalCalls:  dec.int64Val(row, idxTotal, 3),
			FailedCalls: dec.int64Val(row, idxFailed, 4),
			ErrorRate:   dec.floatVal(row, idxErr, 12),
			Latency: model.LatencyPercentiles{
				Avg: dec.floatVal(row, idxAvg, 5),
				Min: dec.floatVal(row, idxMin, 6),
				Max: dec.floatVal(row, idxMax, 7),
				P50: dec.floatVal(row, idxP50, 8),
				P90: dec.floatVal(row, idxP90, 9),
				P95: dec.floatVal(row, idxP95, 10),
				P99: dec.floatVal(row, idxP99, 11),
			},
		})
	}
	return results
}

// parseExceptionsTable parses grouped exception rows: type, source, normalized message,
// count, first/last seen, and the affected operation paths returned by the query
func parseExceptionsTable(t *AzQueryTable) []model.ErrorSummary {
	if t == nil || len(t.Rows) == 0 {
		return nil
	}
	dec := newTableDecoder(t)
	idxType := dec.col("type")
	idxSource := dec.col("source")
	idxMsg := dec.col("message", "samplemessage", "cleanmessage")
	idxCount := dec.col("count")
	idxFirst := dec.col("firstseen")
	idxLast := dec.col("lastseen")
	idxPaths := dec.col("affectedpaths")

	results := make([]model.ErrorSummary, 0, len(t.Rows))
	for _, row := range t.Rows {
		if len(row) < 3 {
			continue
		}
		results = append(results, model.ErrorSummary{
			Type:          dec.stringVal(row, idxType, 0),
			Source:        dec.stringVal(row, idxSource, -1),
			Message:       dec.stringVal(row, idxMsg, 1),
			Count:         dec.int64Val(row, idxCount, 2),
			FirstSeen:     dec.timeVal(row, idxFirst, 3),
			LastSeen:      dec.timeVal(row, idxLast, 4),
			AffectedPaths: dec.stringSliceVal(row, idxPaths, 5),
		})
	}
	return results
}

// parseFanoutTable parses database fan-out rows
func parseFanoutTable(t *AzQueryTable) []model.FanoutMetric {
	if t == nil || len(t.Rows) == 0 {
		return nil
	}
	dec := newTableDecoder(t)
	idxEndpoint := dec.col("endpoint")
	idxRequests := dec.col("totalrequests", "requests")
	idxAvgSQL := dec.col("avgsqlcalls")
	idxP50 := dec.col("p50_calls", "p50")
	idxP75 := dec.col("p75_calls", "p75")
	idxP90 := dec.col("p90_calls", "p90")
	idxP95 := dec.col("p95_calls", "p95")
	idxP99 := dec.col("p99_calls", "p99")
	idxMaxSQL := dec.col("maxsqlcalls")
	idxAvgSQLDur := dec.col("avgsqlduration")
	idxAvgEpDur := dec.col("avgendpointduration")

	results := make([]model.FanoutMetric, 0, len(t.Rows))
	for _, row := range t.Rows {
		if len(row) < 3 {
			continue
		}
		results = append(results, model.FanoutMetric{
			Endpoint:              dec.stringVal(row, idxEndpoint, 0),
			TotalRequests:         dec.int64Val(row, idxRequests, 1),
			AvgSQLCalls:           dec.floatVal(row, idxAvgSQL, 2),
			P50Calls:              dec.floatVal(row, idxP50, -1),
			P75Calls:              dec.floatVal(row, idxP75, -1),
			P90Calls:              dec.floatVal(row, idxP90, -1),
			P95Calls:              dec.floatVal(row, idxP95, -1),
			P99Calls:              dec.floatVal(row, idxP99, -1),
			MaxSQLCalls:           dec.int64Val(row, idxMaxSQL, 3),
			AvgSQLDurationMs:      dec.floatVal(row, idxAvgSQLDur, 4),
			AvgEndpointDurationMs: dec.floatVal(row, idxAvgEpDur, 5),
		})
	}
	return results
}

// parseNPlusOneTable parses deterministic N+1 candidate rows with repeated query shape evidence
func parseNPlusOneTable(t *AzQueryTable) []model.NPlusOneCandidate {
	if t == nil || len(t.Rows) == 0 {
		return nil
	}
	dec := newTableDecoder(t)
	idxEndpoint := dec.col("endpoint")
	idxRequests := dec.col("totalrequests")
	idxAvgSQL := dec.col("avgsqlcalls")
	idxMaxSQL := dec.col("maxsqlcalls")
	idxAvgRepeated := dec.col("avgrepeatedcalls")
	idxMaxShape := dec.col("maxrepeatedshape")
	idxAvgRatio := dec.col("avgrepeatedratio")
	idxSampleShape := dec.col("samplerepeatedshape")
	idxAvgRepeatedDur := dec.col("avgrepeatedduration")
	idxAvgEpDur := dec.col("avgendpointduration")

	results := make([]model.NPlusOneCandidate, 0, len(t.Rows))
	for _, row := range t.Rows {
		if len(row) < 3 {
			continue
		}
		results = append(results, model.NPlusOneCandidate{
			Endpoint:              dec.stringVal(row, idxEndpoint, 0),
			TotalRequests:         dec.int64Val(row, idxRequests, 1),
			AvgSQLCalls:           dec.floatVal(row, idxAvgSQL, 2),
			MaxSQLCalls:           dec.int64Val(row, idxMaxSQL, 3),
			AvgRepeatedCalls:      dec.floatVal(row, idxAvgRepeated, -1),
			MaxRepeatedShape:      dec.int64Val(row, idxMaxShape, -1),
			AvgRepeatedRatio:      dec.floatVal(row, idxAvgRatio, -1),
			SampleRepeatedShape:   dec.stringVal(row, idxSampleShape, -1),
			AvgRepeatedDurationMs: dec.floatVal(row, idxAvgRepeatedDur, -1),
			AvgEndpointDurationMs: dec.floatVal(row, idxAvgEpDur, -1),
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
		// KQL NULL (e.g. percentiles over an empty set)
		return 0
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
		// KQL NULL (e.g. percentiles over an empty set)
		return 0.0
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
