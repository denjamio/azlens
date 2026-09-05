package kql

// Query ID constants defining the canonical identifier for each query family (Section 14, 17)
const (
	QueryIDRequestsSummary      = "requests.summary"
	QueryIDRequestsEndpoints    = "requests.endpoints"
	QueryIDDependenciesSQL      = "dependencies.sql"
	QueryIDDependenciesHTTP     = "dependencies.http"
	QueryIDDependenciesRedis    = "dependencies.redis"
	QueryIDDependenciesCosmos   = "dependencies.cosmos"
	QueryIDDependenciesAll      = "dependencies.all"
	QueryIDExceptionsSummary    = "exceptions.summary"
	QueryIDFanoutSQL            = "fanout.sql"
	QueryIDNPlusOneCandidate    = "nplusone.candidate"
	QueryIDLatencyBreakdown     = "latency.breakdown"
	QueryIDTracesDeprecations   = "traces.deprecations"
	QueryIDMySQLSlowLogs        = "mysql.slow"
	QueryIDMySQLSlowLogsGrouped = "mysql.slow.grouped"
)

// CurrentSchemaVersion defines the active contract schema version
const CurrentSchemaVersion = "v1"

// QueryContract defines the schema, column metadata, units, and minimum sample
// guarantees for a query family (Section 14).
type QueryContract struct {
	QueryID       string            `json:"query_id"`
	SchemaVersion string            `json:"schema_version"`
	Columns       []string          `json:"columns"`
	Units         map[string]string `json:"units,omitempty"`
	Semantics     string            `json:"semantics"`
	MinimumSample int               `json:"minimum_sample"`
}

// registry holds the documented public query contracts
var registry = map[string]QueryContract{
	QueryIDRequestsSummary: {
		QueryID:       QueryIDRequestsSummary,
		SchemaVersion: CurrentSchemaVersion,
		Columns:       []string{"TotalCalls", "FailedCalls", "AvgDuration", "MinDuration", "MaxDuration", "P50", "P75", "P90", "P95", "P99", "ErrorRate", "Count2xx", "Count4xx", "Count5xx"},
		Units:         map[string]string{"duration": "ms", "error_rate": "percentage"},
		Semantics:     "Aggregates overall throughput, duration percentiles, and error rate for a service",
		MinimumSample: 10,
	},
	QueryIDRequestsEndpoints: {
		QueryID:       QueryIDRequestsEndpoints,
		SchemaVersion: CurrentSchemaVersion,
		Columns:       []string{"Endpoint", "TotalCalls", "FailedCalls", "AvgDuration", "MinDuration", "MaxDuration", "P50", "P75", "P90", "P95", "P99", "ErrorRate"},
		Units:         map[string]string{"duration": "ms", "error_rate": "percentage"},
		Semantics:     "Per-endpoint latency percentiles and error rates ranked by P95 latency",
		MinimumSample: 10,
	},
	QueryIDDependenciesAll: {
		QueryID:       QueryIDDependenciesAll,
		SchemaVersion: CurrentSchemaVersion,
		Columns:       []string{"Type", "Target", "Dependency", "TotalCalls", "FailedCalls", "AvgDuration", "MinDuration", "MaxDuration", "P50", "P90", "P95", "P99", "ErrorRate"},
		Units:         map[string]string{"duration": "ms", "error_rate": "percentage"},
		Semantics:     "Remote dependencies (SQL, HTTP, Redis, Cosmos) ranked by P95 duration",
		MinimumSample: 10,
	},
	QueryIDExceptionsSummary: {
		QueryID:       QueryIDExceptionsSummary,
		SchemaVersion: CurrentSchemaVersion,
		Columns:       []string{"Type", "Source", "Message", "Count", "FirstSeen", "LastSeen", "AffectedPaths"},
		Units:         map[string]string{"count": "integer"},
		Semantics:     "Grouped exception signatures and synthesized HTTP 5xx errors with source metadata",
		MinimumSample: 1,
	},
	QueryIDFanoutSQL: {
		QueryID:       QueryIDFanoutSQL,
		SchemaVersion: CurrentSchemaVersion,
		Columns:       []string{"Endpoint", "TotalRequests", "P50_Calls", "P75_Calls", "P90_Calls", "P95_Calls", "P99_Calls", "MaxSqlCalls", "AvgSQLDuration", "AvgEndpointDuration"},
		Units:         map[string]string{"duration": "ms", "calls": "integer"},
		Semantics:     "SQL fan-out metrics measuring the distribution of database calls per request",
		MinimumSample: 5,
	},
	QueryIDNPlusOneCandidate: {
		QueryID:       QueryIDNPlusOneCandidate,
		SchemaVersion: CurrentSchemaVersion,
		Columns:       []string{"Endpoint", "TotalRequests", "AvgSqlCalls", "MaxSqlCalls", "AvgRepeatedCalls", "MaxRepeatedShape", "AvgRepeatedRatio", "SampleRepeatedShape", "AvgRepeatedDuration", "AvgEndpointDuration"},
		Units:         map[string]string{"duration": "ms", "ratio": "percentage"},
		Semantics:     "Deterministic N+1 candidates with repeated query shape evidence per request",
		MinimumSample: 5,
	},
	QueryIDLatencyBreakdown: {
		QueryID:       QueryIDLatencyBreakdown,
		SchemaVersion: CurrentSchemaVersion,
		Columns:       []string{"Endpoint", "AvgTotalMs", "PctDatabase", "PctExternalApi", "PctCache", "PctResidual", "HasOverlap"},
		Units:         map[string]string{"percentages": "percentage", "duration": "ms"},
		Semantics:     "Request duration attribution across dependencies and residual request time with concurrency overlap flag",
		MinimumSample: 5,
	},
	QueryIDTracesDeprecations: {
		QueryID:       QueryIDTracesDeprecations,
		SchemaVersion: CurrentSchemaVersion,
		Columns:       []string{"Deprecation", "Count", "FirstSeen", "LastSeen", "AffectedEndpoints"},
		Units:         map[string]string{"count": "integer"},
		Semantics:     "Normalized language and framework deprecation warnings from traces and exceptions",
		MinimumSample: 1,
	},
	QueryIDMySQLSlowLogs: {
		QueryID:       QueryIDMySQLSlowLogs,
		SchemaVersion: CurrentSchemaVersion,
		Columns:       []string{"TimeGenerated", "QueryDurationMs", "RowsExamined", "RowsSent", "SqlText"},
		Units:         map[string]string{"duration": "ms", "rows": "integer"},
		Semantics:     "Raw MySQL slow query logs ordered by execution duration descending",
		MinimumSample: 1,
	},
	QueryIDMySQLSlowLogsGrouped: {
		QueryID:       QueryIDMySQLSlowLogsGrouped,
		SchemaVersion: CurrentSchemaVersion,
		Columns:       []string{"SqlFingerprint", "Executions", "AvgMs", "MaxMs", "TotalMs", "AvgRowsExamined", "LastSeen"},
		Units:         map[string]string{"duration": "ms", "rows": "integer"},
		Semantics:     "Grouped slow queries aggregated by normalized SQL fingerprint",
		MinimumSample: 1,
	},
}

// GetQueryContract retrieves the documented contract for a given query family
func GetQueryContract(queryID string) (QueryContract, bool) {
	c, ok := registry[queryID]
	return c, ok
}
