# Changelog

All notable changes to **AzLens** will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- **CI Quality Gate Exit Codes**: `deploy-check` now exits with `2` when critical regressions are detected (report still printed first), `1` on runtime/configuration errors, and `0` on success — making it directly usable as a pipeline quality gate.
- **Query Resilience**: Automatic retry with exponential backoff (3 attempts) for transient `az` CLI failures (timeouts, 5xx blips) — permanent failures (auth, missing resources) still fail fast.
- **Supply-Chain Verification**: `azlens update` now verifies the SHA256 checksum of every downloaded asset against the release `checksums.txt` (constant-time comparison, hard fail on mismatch or missing checksums) before installing. `install.sh` performs the same verification and supports pinning a version via `AZLENS_VERSION=vX.Y.Z`.
- **Validation Warnings Visible**: Profile validation warnings (missing `cloud_RoleName` isolation, probe noise, placeholder UUIDs) are now surfaced on stderr at startup — previously computed but never shown.
- **Strict Config Fail-Fast**: `-c/--config` pointing to a non-existent file now errors immediately instead of silently falling back to built-in demo defaults.

### Changed
- **az CLI Noise Isolation**: The `az` CLI is now invoked with `--only-show-errors` so stderr warnings/telemetry notices can no longer corrupt the JSON query output.
- **CI Hardening**: Unit tests now run with the race detector (`go test -race`), and a dedicated golangci-lint job (`govet`, `staticcheck`, `errcheck`, `ineffassign`, `misspell`, `bodyclose`, `copyloopvar`, `gofmt`) gates every push/PR.
- **Idiomatic Naming**: Go identifiers normalized per effective Go style (`MySQL`, `SQL`, `HTTP`, `DB`, `API` casing) without changing any JSON/YAML wire format.
- **Architecture Cleanup**: Removed package-level global state in `cmd` (runtime injected through the command context), collapsed the six duplicated `top` subcommand skeletons into a single generic `runTopQuery` helper, unified output rendering in `reporter.Render`, split the monolithic `analyzer.Compare` (210 lines) into six isolated analysis phases, and removed dead exported code (`ExpandQuery`, `CompareWindows`).

### Removed
- **`scope.custom_filters`**: The arbitrary raw-KQL injection escape hatch has been removed. Custom scoping should be expressed through the supported scope fields (`role_name`, `role_instance`, `namespace`, `resource_id`, `custom_dimensions`); this closes the path where a shared or generated config could execute arbitrary KQL against a workspace.
- **`azlens deploy-check`**: Automated regression quality gate comparing pre- and post-deploy telemetry (P50, P75, P90, P95, P99 latency percentiles, error rates, N+1 SQL queries, new unindexed database calls, and newly introduced exceptions). All window telemetry is fetched in a **single batched KQL request** (one `az` CLI invocation per window, both windows in parallel) with instant fast-fail cancellation.
- **`azlens top`**: Real-time live triage snapshot with 6 orthogonal subcommands:
  - `endpoints`: Identify slowest API routes ordered by P95/P99 latency and error rates.
  - `queries`: Surface slowest queries across SQL, Redis, CosmosDB, and Azure MySQL slow logs (`MySqlSlowLogs`).
  - `n-plus-one` (`n+1`): Detect endpoints with excessive database fan-out per request.
  - `breakdown`: Granular latency percentage attribution (% Database, % External APIs, % Cache, % App Code).
  - `errors`: Grouped exceptions and HTTP 5xx errors with dynamic ID and UUID normalization.
  - `deprecations`: Framework, language, and library deprecation warnings for safe upgrades.
- **`azlens config`**: Local environment and profile manager (`list`, `show`, `use`, `init`).
- **`azlens update`**: In-place atomic self-upgrade mechanism from GitHub Releases.
- **Multi-Subscription Support**: `insights` and `logs` targets each carry their own `subscription` and optional `tenant`; every query is routed statelessly with `--subscription` (no global `az account` switching). Query batches never cross backends.
- **Noise Cancellation & Smart Normalization**:
  - Exclude non-actionable client aborts (`ClientClosedRequest`, `broken pipe`, `context canceled`).
  - Filter out bot crawlers and 404 routing errors (`ActionController::RoutingError`, `NotFoundHttpException`).
  - Dynamic ID normalization (`<ID>` and `<UUID>`) to consolidate fragmented error messages into true counts.
  - Callsite line number normalization (`:<line>`) in deprecation warnings.
- **Local-First Configuration (`.azlens.yaml`)**: The config file is the **single source of truth** — Azure targets, per-directory subscriptions, scopes, quality-gate thresholds (including `min_sample_calls`), and operational defaults. Minimal CLI surface: `-c/--config`, `-p/--profile`, `-o/--output`, `--mock`, and positional durations.
- **Multi-Format Output**: Terminal tables with smart truncation, GitHub/GitLab Markdown for PR comments, and raw JSON.
- **Offline Mock Telemetry (`--mock`)**: Full simulation mode for rapid testing and demonstrations without Azure credentials.

### Fixed
- **Windows Self-Update**: `os.Rename` over an existing executable fails on Windows — the updater now removes the old binary first as a platform fallback (Unix keeps the atomic rename path).
- **Self-Update Context Awareness**: Downloads and API calls now honor context cancellation (Ctrl+C no longer leaves downloads running).
- **install.sh**: Fixed the suggested smoke-test command (`deploy-check` was referenced by a stale name) and enabled `pipefail` for safer pipelines.

### Removed
- Global resource/scope override flags (`--app`, `--workspace`, `--subscription`, `--app-subscription`, `--workspace-subscription`, `--role`, `--pod`, `--namespace`, `--resource-id`, `--db`, `--exclude-probes`): use profiles in `.azlens.yaml`.
- Threshold override flags on `deploy-check` (`--p95-warn-pct`, `--p95-crit-pct`, `--err-warn-delta`, `--err-crit-delta`): configure them per profile under `thresholds`.
- Configurable query timeout (`defaults.timeout` and `--timeout` flag): queries use a fixed internal budget.
- Legacy flat config aliases (`default_profile`, `app_insights`, `app_subscription`, `workspace`, `workspace_id`, `role`, `namespace`, `db`, `pod_name`, ...) in favor of the nested `insights` / `logs` blocks.
