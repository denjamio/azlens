# Changelog

All notable changes to **AzLens** will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.0.1] - 2026-09-04

### Fixed

- **CI & Linter Gates**: Fixed 20 `golangci-lint` issues (`errcheck`, `gofmt`, `staticcheck`, and `unused`) in new operational packages.
- **README Architecture Flowchart**: Fixed GitHub Mermaid parser failure by replacing unclosed HTML tags and formatting line breaks with `<br/>`.

## [1.0.0] - 2026-09-04

### Added

- **AzLens Operational CLI Specification (v0.1)**: Transformed AzLens into a simple, opinionated, actionable operational CLI answering: *"Does this environment need my attention right now?"*
- **Primary Operational Command (`azlens [window]`)**: Evaluates operational health across all configured capabilities. Silence is a feature: outputs `Everything looks normal.` when healthy.
- **Analytical Engine & 13 Detectors (`pkg/analysis/`)**: Implemented source-neutral analytical engine with 13 detectors: `RequestLatencyRegression`, `RequestErrorRegression`, `NewException`, `ExceptionRegression`, `DependencyLatencyRegression`, `DependencyErrorRegression`, `DependencyFanoutRegression`, `WorkloadUnavailable`, `RestartBurst`, `OOMKilled`, `ResourceSaturation`, `TelemetryStale`, `AvailabilityFailure`.
- **Symptom Correlation & Ranking (`correlator.go`, `ranking.go`)**: Collapses multiple related symptoms into a single problem story (e.g. endpoint latency regressed by downstream dependency; request errors caused by new exceptions; container OOM kills and workload unavailability). Demotes low-impact items to "Worth Watching". Caps visible output at 3 "Needs Attention" problems and 3 "Worth Watching" items.
- **Root Cause Analysis (`azlens explain [subject] [window]`)**: Explains operational problems and provides supporting evidence with deterministic ambiguity matching protection.
- **Telemetry Inspection (`azlens inspect <view> [window]`)**: Direct visibility into operational evidence: `endpoints`, `dependencies`, `queries`, `errors`, and `runtime`.
- **Deployment Safety Verification (`azlens deploy [window] [--at TIME]`)**: Compares telemetry across before vs after release windows, showing only changed signals.
- **Environment Diagnostics (`azlens doctor`)**: Diagnostics covering Azure authentication, backend reachability, and 8-capability operational coverage.
- **Canonical Self-Updater (`azlens upgrade`)**: Direct in-place upgrade with SHA256 checksum verification.
- **JSON Schema v1 (`pkg/domain/`)**: Unified machine-readable JSON format across operational commands.
- **Pipeline-Aware Process Exit Codes**: `0` (healthy / safe), `1` (failure / error), `2` (actionable problem / regression), `3` (insufficient data / unknown).
- **Profile Resolution Precedence**: Strict 5-level precedence: `--profile/-p` > `AZLENS_PROFILE` > `defaults.profile` > single profile > error.

### Deprecated

- **`azlens top`**: Deprecated in favor of `azlens inspect`.
- **`azlens deploy-check`**: Deprecated in favor of `azlens deploy`.
- **`azlens update`**: Deprecated in favor of `azlens upgrade`.

## [0.4.15] - 2026-09-04

### Fixed

- **`top errors` missed HTTP 5xx failures without exception telemetry**: The command documented "exceptions and HTTP 5xx errors" but queried only the `exceptions` table, returning nothing for services that respond 500/503 without throwing tracked exceptions (typical of OpenTelemetry-instrumented apps where the exception event never reaches App Insights). The query now unions both sources — real exceptions plus `requests` rows with `success == false` and `resultCode >= 500`, synthesized as `HTTP <code>` signatures (or `HTTP 5xx` when the code is unknown) with the endpoint as message context. Both branches carry identical partition-pruned scoping (time first, resource, roles, pods, synthetic/probe exclusions per table), and grouping, noise filters, ID normalization, affected-path attribution, and the parser are unchanged. A single round trip covers both sources: when no exceptions exist, the requests branch supplies the errors with no client-side fallback.

### Optimized

- **Single-pass percentiles in `deploy-check` overall, `top endpoints`, and `top queries`**: Replaced N separate `percentile()` aggregations (one histogram scan each) with one `percentiles()` call expanded back to scalar columns. Identical values and identical output column order; fewer aggregation passes over the scanned data.
- **Top-N heap instead of full sort in every ranked query**: `order by … desc | take N` replaced with the equivalent `top N by …` across `top endpoints`, `top queries`, `top errors`, `top n-plus-one`, `top breakdown`, `top deprecations`, `top slow-logs`, and grouped slow logs — Kusto can stream a top-N heap instead of fully sorting each result set. Same rows, same order.

## [0.4.14] - 2026-09-04

### Added

- **Industry-anchored severity bands (prod-ready color standard)**: Centralized every color threshold in `pkg/reporter/thresholds.go` with documented bases: error rate 1%/5% (SRE error budget), API latency 300ms/1s (1-second rule), DB statements 100ms/1s (`long_query_time`), cache ops 5ms/25ms, scan ratio 100×/1000× (Percona missing-index heuristic), N+1 5/15 and regression deltas 15%/30% (analyzer defaults). Latency coloring is applied per dependency class, endpoint latency columns are now banded, and slow-logs `Rows Examined` is colored by examined/returned ratio instead of absolute counts. Documented in a README severity reference table.

### Changed

- **Robust SQL fingerprint pipeline (fixes near-duplicate groups in `--grouped`)**: The fingerprint normalization is now a single RE2 step pipeline defined once in Go (`pkg/kql/fingerprint.go`) and rendered 1:1 into the KQL query, so local and server-side fingerprints cannot drift. On top of literal masking it now strips `/* */` and `--` comments (sqlcommenter/driver hints), normalizes casing (`SELECT` vs `select`), collapses IN-list and tuple lengths (`IN (?, ?, ?)` → `(?)`), handles hex literals and escaped/doubled quotes, and trims/collapses whitespace — same-shape queries collapse into one group. Covered by unit tests that mirror the KQL pipeline byte-for-byte.

### Fixed

- **`top errors` never showed affected endpoints**: The exceptions query returns `AffectedPaths` (`make_set(operation_Name, 10)`), but the parser discarded the column, leaving `ErrorSummary.AffectedPaths` empty in every real run (only mock data showed endpoints). It is now parsed, restoring endpoint attribution in the output and the deploy-check root-cause correlation that depends on it.
- **CI golangci-lint failure**: Removed an unused header heuristic and corrected staticcheck naming (ST1003) flagged by the release gate.

## [0.4.13] - 2026-09-04

### Added

- **`azlens top slow-logs --grouped` (query shape aggregation)**: New flag aggregating MySQL slow query logs by a normalized SQL fingerprint, reporting per shape: `Executions`, `Avg`, `Max`, and `Total Time` durations, `Rows Examined (avg)`, and `Last Seen`, ordered by total accumulated duration (highest overall impact first). Implemented as server-side KQL aggregation with mock parity and terminal/markdown/json renderers.
- **Modern width-aware table engine**: Replaced `tablewriter` with a purpose-built UTF-8 renderer (rounded borders, cyan headers, dim frames). Tables now adapt to the terminal width — flexible text columns shrink proportionally (never truncating numeric measurements or headers) instead of overflowing on narrow screens. Piped/redirected output is not truncated; `$COLUMNS` overrides detection. Terminal size is probed via `TIOCGWINSZ` (Linux/macOS) and `GetConsoleScreenBufferInfo` (Windows) — everything compiles statically into the binary, no runtime dependencies.
- **`--color auto|always|never` global flag**: Controls output colorization. `auto` (default) colors only on a TTY honoring `NO_COLOR`; `always`/`never` force the mode. Severity-based cell coloring across every table: error rates, slow-log durations, regression deltas, N+1 spikes, and deploy verdict statuses.
- **Instrumentation noise classification (`top errors`, `deploy-check`)**: Exceptions like `ModuleNotFoundError` with message `Exception occurred when instrumenting: fastapi` are now recognized as auto-instrumentation SDK noise (the SDK failing to hook a framework), annotated with an explanatory hint in table and markdown output, and surfaced as a `[INSTRUMENTATION NOISE]` root-cause hint in deploy reports instead of being misread as an application exception.

### Changed

- **Human-friendly generic tables**: Arbitrary query results (and `azlens query`-style output) now humanize headers (`TotalCalls` → `Total Calls`, `TimeGenerated` → `Time`, `QueryDurationMs` → `Query Duration (ms)`, `operation_Name` → `Operation`) and normalize cell values: local-time timestamps, thousands-separated counts, humanized durations/percentages, joined KQL arrays, and `NULL` → `-`. Applied consistently to terminal and markdown output.
- **Clarified slow-logs row metrics**: `azlens top slow-logs` headers renamed for clarity: `Examined` → `Rows Examined` and `Sent` → `Rows Returned` (`RowsSent` counts the rows the query returned to the client, not executions). Markdown output uses the same wording.
- **Humanized deploy-check summary**: Latency amounts render compactly (`45ms`, not `45.00ms`), request counts with separators (`45,200 reqs`), and deltas signed with human units (`+12ms (+6.7%)`).
- **Dependency diet**: Removed `github.com/olekukonko/tablewriter`; promoted already-vendored `github.com/mattn/go-runewidth` and `golang.org/x/sys` to direct (both pure Go, statically linked).

## [0.4.12] - 2026-09-04

### Fixed

- **Resolved "invalid properties" in `top breakdown`**: Fixed KQL BadArgumentError in `BuildLatencyBreakdown`. Kusto does not permit arithmetic between aggregation functions inside `summarize` (e.g. `round(100.0 * avg(SqlTime) / avg(duration), 1)`). Separated scalar aggregation (`AvgTotal`, `AvgSql`, `AvgHttp`, `AvgRedis`, `AvgApp`) into `summarize` and subsequent percentage calculations into `extend`.
- **Eliminated App Insights telemetry starvation from `logs.namespace`**: Removed inadvertent `logs.namespace` filter from Application Insights base clauses (`requests`, `dependencies`, `exceptions`). In standard App Insights SDKs, telemetry items do not carry Kubernetes namespace in `customDimensions`, causing queries to match 0 rows across all timeframes. Application Insights custom dimensions can still be explicitly targeted via `custom_dimensions`.

## [0.4.11] - 2026-09-04

### Added

- **First-class slow logs model & reporters**: Introduced `model.SlowLogEntry` with typed duration, row metrics, and SQL text. Added dedicated `reporter.PrintSlowLogsTable` and `reporter.PrintSlowLogsMarkdown` for `azlens top slow-logs`.
- **Key MySQL performance indicators (`RowsExamined` & `RowsSent`)**: `azlens top slow-logs` now queries and reports `RowsExamined` (scanned rows, essential for finding missing indexes and table scans) alongside `RowsSent` (returned rows) with thousands-separator formatting.
- **Normalized human-readable duration**: Slow log durations are normalized and formatted intuitively (e.g. `14.52s`, `850ms`) rather than displaying raw milliseconds.

### Fixed

- **Eliminated spurious `batch result table "overall" not found` warnings**: Removed fragile `| as <name>` KQL table renaming and matching. Azure Monitor KQL returns batch statement results in strictly guaranteed positional order (`PrimaryResult`, `PrimaryResult_1`, etc.); `QueryWindowMetrics` now directly maps tables by position without false warning noise.

## [0.4.10] - 2026-09-04

### Changed

- **Modern MySQL Flexible Server slow logs schema**: Streamlined `BuildMySQLSlowLogsQuery` to exclusively target the modern Azure MySQL Flexible Server `MySqlSlowLogs` schema (`TimeGenerated`, `QueryDurationMs`, `SqlText`, `Db`), dropping legacy Single Server fallbacks (`AzureDiagnostics`, `query_time_d`, `QueryTime_s`, `sql_text_s`, etc.) for optimal KQL compilation and query performance.

## [0.4.9] - 2026-09-04

### Fixed

- **Azure CLI JSON array unmarshaling**: Resolved `failed to parse az cli json output: cannot unmarshal array into Go value of type azure.AzQueryResult`. Added flexible multi-format output parsing in `parseAzQueryOutput` supporting standard `{"tables": [...]}`, direct table arrays `[{"name": "...", "columns": [...], "rows": [...]}]`, empty arrays `[]`, and key-value record maps `[{"col": val}]`.
- **Informative query error reporting**: Truncates and presents raw CLI output in error diagnostics when JSON responses cannot be unmarshaled.

## [0.4.8] - 2026-09-04

### Optimized

- **Partition-pruned subqueries in joins (`top n-plus-one` and `top breakdown`)**: Injected timestamp partition filters (`where timestamp between (...)`) into inner `dependencies` join subqueries. Ensures Azure Monitor prunes partitions across both legs of the join, significantly reducing scanned data volume, query execution time, and Azure query consumption on high-throughput services.

## [0.4.7] - 2026-09-04

### Fixed

- **Resolved "invalid properties" (`BadArgumentError`) in `top endpoints`, `triage`, and `deploy-check`**: Replaced illegal property dereferencing on `percentiles` (`P = percentiles(...)` + `P.percentile_duration_50`) with direct scalar percentile aggregations (`P50 = percentile(duration, 50)`, `P95 = percentile(duration, 95)`, etc.) in `BuildRequestsSummary`, `BuildEndpointsSummary`, and `BuildDependenciesSummary`.
- **Dynamic message resolution in `top errors`**: Fixed query failures on non-existent `innermostMessage` and `message` columns in the `exceptions` table by introducing a safe `column_ifexists` cascade (`outerMessage` -> `message` -> `innermostMessage`).
- **Resilient synthetic and probe filtering in base clauses**: Scoped synthetic traffic filters (`operation_SyntheticSource`) strictly to `requests` and `dependencies` wrapped in `column_ifexists`, and guarded `customDimensions` lookups with `column_ifexists('customDimensions', dynamic(null))` to prevent scalar expression resolution failures on tables without these columns.

## [0.4.6] - 2026-09-04

### Fixed

- **KQL parser syntax error on `string(null)`**: Fixed `cannot parse string on line [8, 50]` syntax error in `BuildMySQLSlowLogsQuery`. KQL string scalar type does not have a `string(null)` literal; string columns now use valid empty string `""` as `column_ifexists` default and a robust `iff(isnotempty(...))` cascade for SQL query text resolution.

## [0.4.5] - 2026-09-04

### Fixed

- **Dynamic schema resilience for MySQL slow logs (`QueryDurationMs`)**: Resolved query failures in Azure Log Analytics environments using resource-specific schemas. The query now uses safe `column_ifexists()` lookups for `QueryDurationMs`, `query_duration_ms`, `query_time_d`, and `QueryTime_s`, ensuring compatibility across both modern MySQL Flexible Server and legacy AzureDiagnostics schemas without failing on non-existent columns.
- **Safe SQL text resolution**: Wrapped `SqlText`, `sql_text_s`, `SqlText_s`, `Query_s`, and `Message` in `column_ifexists()` to prevent scalar expression resolution errors.

## [0.4.4] - 2026-09-04

### Fixed

- **MySQL slow query logs filter**: Removed non-existent column `DatabaseName_s` from `MySqlSlowLogs` query filter in Log Analytics; the query now strictly checks `Db =~ '<name>'`.
- **Inherently ordered `top slow-logs`**: Removed the redundant `--slowest` flag. `azlens top slow-logs` now inherently orders query results by execution duration descending (`QueryDurationMs desc`), naturally presenting the slowest queries first.
- **Application Insights `resource_group` support**: Added `resource_group` to `insights` target configuration (`target.insights.resource_group` and `shared.insights.resource_group`). Azure CLI requires `--resource-group` when looking up Application Insights by component name rather than App ID GUID.
- **Clarified App Insights resolution guidance**: Improved actionable error messages when an App Insights component is not found, providing instructions for using App ID GUID, Resource ID, `insights.resource_group`, or workspace fallback.

## [0.4.3] - 2026-09-04

### Fixed

- **Azure CLI query and extension flag order**: Moved `--only-show-errors` to the end of command arguments in `runAzQueryOnce` and `missingAzExtensions`. Placing global flags ahead of extension command groups (`az --only-show-errors monitor log-analytics query`) causes the Azure CLI / Knack argument parser to fail with `'log-analytics' is misspelled or not recognized by the system`, which previously resulted in false-positive "extension not installed" errors even when the extension was already present.
- **Improved error diagnostics**: Included raw Azure CLI stderr/stdout output in extension discovery errors for faster troubleshooting.

## [0.4.2] - 2026-09-04

### Fixed

- **Azure CLI `account set` syntax and flag order**: Corrected argument ordering to `account set --subscription <id> --only-show-errors`. Placing `--only-show-errors` before positional command tokens caused Azure CLI / knack argument parser to fail with `'set' is misspelled or not recognized by the system`.
- **Removed unsupported `--tenant` argument**: Removed `--tenant` from `az account set` invocations. In Azure CLI, `az account set` only accepts `--subscription` / `-s` (tenant mapping is automatically resolved by Azure CLI from the subscription ID).
- **Expanded auth error detection**: Added detection for `"doesn't exist"` / `"does not exist"` error outputs from Azure CLI when a subscription is hosted in a tenant the user has not yet authenticated to, triggering seamless on-demand `az login --tenant <id>` fallback.

## [0.4.1] - 2026-09-04

### Fixed

- **Multi-tenant login loop**: Removed `ensureSubscriptionSessions` from pre-flight diagnostics in `PersistentPreRunE`. Each query backend now independently manages and switches its own subscription and tenant context just-in-time (`az account set --subscription <id>`), resolving the infinite login loop across separate Entra directories.
- **On-demand auth handling**: Integrated JIT interactive login fallback directly into client context switching when `account set` requires directory authentication.

## [0.4.0] - 2026-09-04

### Changed

- **Seamless Azure CLI multi-tenant and multi-subscription architecture**:
  - Removed artificial `AZURE_CONFIG_DIR` profile isolation; azlens now operates directly in the user's standard `~/.azure` session, naturally sharing authenticated accounts and installed extensions (`log-analytics`, `application-insights`).
  - Added automatic subscription context switching via `az account set --subscription <id> [--tenant <tenant>]` before query executions, ensuring data-plane queries acquire the right token for the right directory.
  - Replaced restrictive `az account show` pre-flight checks with non-blocking `az account list --all --query "[].id" -o tsv` verification.
  - Simplified login flow and hints to standard `az login --tenant <tenant>` commands.

## [0.3.0] - 2026-09-04

### Added

- **Shared quality-gate policy (`shared.thresholds`)**: thresholds move into the
  shared section with the same field-by-field inheritance as the rest of the
  shared target — profiles override only what differs (e.g. staging looser than
  production).
- **Directory-aware sessions and query routing (`directory_id`)**: each backend
  can declare the Entra directory ID hosting it. azlens uses it two ways, both
  side-effect free:
  - **Session automation**: when a configured subscription is not in the active
    az session, azlens launches `az login --tenant <directory_id>` for the exact
    directory that owns it (interactive, with the v2 account-picker experience
    disabled for a direct flow) and re-verifies. CI runs fail fast with the
    exact per-subscription login commands.
  - **Per-query token routing**: every `az` query runs against an isolated
    az CLI profile per directory (`AZURE_CONFIG_DIR`, the documented isolation
    mechanism), so data-plane tokens are issued by the right directory even in
    multi-directory setups — without `az account set` and without ever
    touching the user's main az profile.
- **Config key renames for precision**: `subscription` → `subscription_id`
  (it is an ID), `tenant` → `directory_id`; `directory_id` sits above
  `subscription_id` in `insights` / `logs`.

## [0.2.0] - 2026-09-04

Config schema v2 — a coordinated breaking overhaul of the configuration file,
target routing, and session management. Migrate with `azlens config init` and
compare against `azlens.example.yaml`.

### Breaking Changes

- **Config file renamed to `azlens.yaml`** (no leading dot, meant to be committed
  and shared by the team). `.azlens.yaml` is no longer read; the search path is
  now `azlens.yaml` → `~/.config/azlens/azlens.yaml`, plus `-c <path>` for an
  explicit file. `azlens config init` creates `azlens.yaml`.
- **`tenant` removed** from `target.insights` / `target.logs`: the subscription
  determines the directory (tenant) of each query; azlens never sets
  `AZURE_TENANT_ID` or touches tokens.
- **`target.role` / `target.pod` → plural `target.roles` / `target.pods`**:
  scalar or list (e.g. `roles: [order-service, billing-service]`). KQL: single
  value uses `=`~`/`has`, multiple use `in~`/`has_any`.
- **`target.namespace` / `target.database` → `target.logs.namespace` /
  `target.logs.database`**: Log Analytics-scoped filters now live under `logs`.

### Added

- **Shared target (`shared:`)**: declare once every target value that does not
  vary across environments (subscriptions, filters, exclusion flags); profiles
  inherit field-by-field and override only what differs. Boolean filters are
  tri-state (`nil` inherits) so a profile can explicitly override shared flags.
- **Subscription session management**: pre-flight on `top` / `deploy-check`
  verifies each configured subscription is in the active az session; on a TTY,
  azlens launches interactive `az login` and re-verifies. Non-interactive runs
  fail fast with an actionable hint instead of hanging. `azlens doctor` reports
  per-subscription session coverage. azlens never stores or refreshes tokens.
- **Per-run filter overrides**: `--role` / `--pod` (repeatable or
  comma-separated) replace the configured lists for a single run.
- **Shell completion for `--role` / `--pod`** sourced from the config file
  (nothing declared → nothing completed).
- **Missing az extension detection**: `az monitor log-analytics query` and
  `az monitor app-insights query` are extension commands; azlens now surfaces
  `az extension add --name <ext>` hints instead of the cryptic az "mispelled"
  error, both at query time and in `azlens doctor` / pre-flight.

### Changed

- `azlens doctor` verifies the `application-insights` and `log-analytics`
  extensions in addition to CLI installation, login session, and profile
  validation.

## [0.1.0] - 2026-09-03

First public release of **AzLens** — actionable observability and deploy-regression
analysis for Azure Application Insights and Log Analytics, driven entirely by a
local `.azlens.yaml` config file.

### Added

- **`azlens deploy-check`**: Automated regression quality gate comparing pre- and
  post-deploy telemetry (P50, P75, P90, P95, P99 latency percentiles, error rates,
  N+1 SQL queries, new unindexed database calls, and newly introduced exceptions).
  All window telemetry is fetched in a **single batched KQL request** (one `az` CLI
  invocation per window, both windows in parallel) with instant fast-fail
  cancellation. `--at` compares against an arbitrary past deploy timestamp.
- **Quality-Gate Exit Codes**: `deploy-check` exits with `2` when critical
  regressions are detected (report still printed first), `1` on runtime or
  configuration errors, and `0` on success — making it directly usable as a
  pipeline quality gate.
- **`azlens top`**: Real-time live triage snapshot with 7 orthogonal subcommands:
  - `endpoints`: slowest API routes ordered by P95/P99 latency and error rates.
  - `queries`: slowest queries across SQL, Redis, CosmosDB, and external HTTP dependencies.
  - `slow-logs`: database engine slow query logs (`MySqlSlowLogs`).
  - `n-plus-one` (`n+1`): endpoints with excessive database fan-out per request.
  - `breakdown`: granular latency attribution (% Database, % External APIs, % Cache, % App Code).
  - `errors`: grouped exceptions and HTTP 5xx errors with dynamic ID and UUID normalization.
  - `deprecations`: framework, language, and library deprecation warnings for safe upgrades.
- **`azlens doctor`**: Diagnostics for Azure CLI authentication, workspace access,
  and `azlens` configuration health.
- **`azlens config`**: Local environment and profile manager (`list`, `show`, `init`).
- **`azlens update`**: In-place atomic self-upgrade from GitHub Releases with
  **SHA256 checksum verification** (constant-time comparison, hard fail on mismatch
  or missing checksums). Downloads honor context cancellation, and a platform
  fallback handles the Windows `os.Rename` limitation.
- **Supply-Chain Verification**: `install.sh` verifies the SHA256 checksum of every
  downloaded asset against the release `checksums.txt` and supports pinning a
  version via `AZLENS_VERSION=vX.Y.Z`.
- **Multi-Subscription Support**: `insights` and `logs` targets each carry their own
  `subscription` and optional `tenant`; every query is routed statelessly with
  `--subscription` (no global `az account` switching). Query batches never cross backends.
- **Noise Cancellation & Smart Normalization**:
  - Exclude non-actionable client aborts (`ClientClosedRequest`, `broken pipe`, `context canceled`).
  - Filter out bot crawlers and 404 routing errors (`ActionController::RoutingError`, `NotFoundHttpException`).
  - Dynamic ID normalization (`<ID>` and `<UUID>`) to consolidate fragmented error messages into true counts.
  - Callsite line number normalization (`:<line>`) in deprecation warnings.
- **Local-First Configuration (`.azlens.yaml`)**: The config file is the **single
  source of truth** — Azure targets, per-directory subscriptions, scopes,
  quality-gate thresholds (including `min_sample_calls`), and operational defaults.
  Minimal CLI surface: `-c/--config`, `-p/--profile`, `-o/--output`, `--mock`, and
  positional durations. `-c/--config` pointing to a missing file fails fast instead
  of silently falling back to demo defaults.
- **Multi-Format Output**: Terminal tables with smart truncation, GitHub/GitLab
  Markdown for PR comments, and raw JSON.
- **Offline Mock Telemetry (`--mock`)**: Full simulation mode with a deterministic,
  healthy baseline for rapid testing and demonstrations without Azure credentials.
- **Query Resilience**: Automatic retry with exponential backoff (3 attempts) for
  transient `az` CLI failures (timeouts, 5xx blips) — permanent failures (auth,
  missing resources) still fail fast.
- **Validation Warnings Visible**: Profile validation warnings (missing
  `cloud_RoleName` isolation, probe noise, placeholder UUIDs) are surfaced on
  stderr at startup.

### Changed

- **az CLI Noise Isolation**: The `az` CLI is invoked with `--only-show-errors` so
  stderr warnings and telemetry notices can no longer corrupt JSON query output.
- **CI Hardening**: Unit tests run with the race detector (`go test -race`), and a
  dedicated golangci-lint job (`govet`, `staticcheck`, `errcheck`, `ineffassign`,
  `misspell`, `bodyclose`, `copyloopvar`, `gofmt`) gates every push and PR.

### Fixed

- **Windows Self-Update**: `os.Rename` over an existing executable fails on Windows —
  the updater removes the old binary first as a platform fallback (Unix keeps the
  atomic rename path).
- **Self-Update Context Awareness**: Downloads and API calls honor context
  cancellation (Ctrl+C no longer leaves downloads running).
- **install.sh**: The suggested smoke-test command references the correct
  subcommand, and `pipefail` is enabled for safer pipelines.

### Removed

The following pre-release escape hatches and legacy flags were dropped during
hardening and are not part of the public 0.1.0 surface:

- **`scope.custom_filters`**: Arbitrary raw-KQL injection is not supported. Custom
  scoping must be expressed through the supported scope fields (`role_name`,
  `role_instance`, `namespace`, `resource_id`, `custom_dimensions`), closing the
  path where a shared or generated config could execute arbitrary KQL against a workspace.
- Global resource/scope override flags (`--app`, `--workspace`, `--subscription`,
  `--app-subscription`, `--workspace-subscription`, `--role`, `--pod`, `--namespace`,
  `--resource-id`, `--db`, `--exclude-probes`): use profiles in `.azlens.yaml`.
- Threshold override flags on `deploy-check` (`--p95-warn-pct`, `--p95-crit-pct`,
  `--err-warn-delta`, `--err-crit-delta`): configure them per profile under `thresholds`.
- Configurable query timeout (`defaults.timeout` and `--timeout` flag): queries use
  a fixed internal budget.
- Legacy flat config aliases (`default_profile`, `app_insights`, `app_subscription`,
  `workspace`, `workspace_id`, `role`, `namespace`, `db`, `pod_name`, ...) in favor
  of the nested `insights` / `logs` blocks.

[1.0.1]: https://github.com/denjamio/azlens/releases/tag/v1.0.1
[1.0.0]: https://github.com/denjamio/azlens/releases/tag/v1.0.0
[0.1.0]: https://github.com/denjamio/azlens/releases/tag/v0.1.0
