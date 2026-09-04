# 🚀 AzLens

**AzLens** is a high-performance, single-binary **Go CLI tool** designed for Azure engineers, developers, and SREs to detect performance regressions, inspect latency bottlenecks, and interrogate telemetry from **Azure Monitor, Application Insights, and Log Analytics**.

---

## ⚡ Quick Install

Install the latest release directly to your `PATH` with a single command:

```bash
curl -sSL https://raw.githubusercontent.com/denjamio/azlens/main/install.sh | bash
```

Or build locally with Docker (zero host dependencies required):
```bash
make build
```

---

## 🧠 Mental Model & Core Flow

AzLens is built around **two primary operational workflows**:

```mermaid
flowchart LR
    A["🚀 azlens"] --> B["⚖️ deploy-check\n(Did my deploy break anything?)"]
    A --> C["🔍 top\n(Where is the bottleneck right now?)"]
    A --> D["🩺 doctor\n(Diagnose Azure auth & config)"]
    A --> E["⚙️ config\n(Profiles & Operational Defaults,\ninit / list / show)"]
    A --> F["🔄 update / version\n(Self-Upgrade & Build Info)"]
    
    B --> G["📊 Universal Output\n(Table | Markdown | JSON)"]
    C --> G
```

1. **`azlens deploy-check`** (*Post-Deploy Regression Quality Gate*): Compare pre- vs post-deploy telemetry (latency percentiles P50-P99, error rates, N+1 SQL queries, and newly introduced exceptions).
2. **`azlens top [endpoints|queries|slow-logs|n-plus-one|breakdown|errors|deprecations]`** (*Live Triage Snapshot*): Instantly surface the slowest API endpoints, heavy database queries, database engine slow logs, error signatures, or framework deprecation warnings.
3. **`azlens doctor`** (*Environment Diagnostics*): Verify the Azure CLI installation, required az extensions, per-subscription session coverage, and profile configuration validity.
4. **`azlens config [list|show|init]`** (*Profile & Defaults Manager*): Inspect configured environments and generate starter configuration files.

---

## 📋 Commands & Usage

### 1. Pre- vs Post-Deploy Regressions (`azlens deploy-check`)
Compare metrics between two time windows to automatically detect latency regressions, N+1 SQL queries, new unindexed queries, and error spikes:

```bash
# Compare the last 30 minutes vs the previous 30 minutes
azlens deploy-check 30m

# Center the comparison on a deploy time: 30m before vs 30m after 14:30
azlens deploy-check 30m --at 14:30

# Deploy that completed 20 minutes ago
azlens deploy-check 30m --at -20m

# Target a specific profile and output as Markdown (ideal for PR comments)
azlens deploy-check 30m -p prod -o markdown

# Offline demo mode (no Azure connection needed)
azlens deploy-check --mock
```

**CI Quality Gate:** `deploy-check` is pipeline-aware through exit codes — `0` when no critical regressions are detected, `1` on error, and `2` when critical regressions are found (after the report is printed). Example GitHub Actions step (note `pipefail`: without it, `tee` would swallow the exit code):

```yaml
- name: Deploy regression quality gate
  shell: bash
  run: set -o pipefail; azlens deploy-check 30m -p prod -o markdown | tee deploy-report.md
```

**Terminal Output Preview:**
```text
═══════════════════════════════════════════════════════════════════════
 🚀 AZLENS DEPLOY REGRESSION REPORT: Production Microservices
═══════════════════════════════════════════════════════════════════════
 Baseline Window: 20:00 to 21:00 | Post-Deploy Window: 21:00 to 22:00
 Overall Status:  ✅ HEALTHY (No significant regressions detected)

+----------------+--------------+--------------+-------------------+--------+
|     METRIC     |   BASELINE   | POST-DEPLOY  |       DELTA       | STATUS |
+----------------+--------------+--------------+-------------------+--------+
| P50 Latency    | 52.00ms      | 54.20ms      | +2.20ms (+4.2%)   | OK     |
| P95 Latency    | 295.00ms     | 302.10ms     | +7.10ms (+2.4%)   | OK     |
| P99 Latency    | 580.00ms     | 595.00ms     | +15.00ms (+2.6%)  | OK     |
| Error Rate     | 1.25%        | 1.28%        | +0.03% (+0.0%)    | OK     |
| Total Requests | 48100.00reqs | 49350.00reqs | +1250reqs (+2.6%) | OK     |
+----------------+--------------+--------------+-------------------+--------+

📌 Per-Endpoint Latency & Error Rate Diff:
+------------------------------+----------+----------+--------+-----------+-----------+--------+
|           ENDPOINT           | BASE P95 | POST P95 | P95 Δ% | BASE ERR% | POST ERR% | STATUS |
+------------------------------+----------+----------+--------+-----------+-----------+--------+
| POST /api/v1/orders/checkout | 820.0ms  | 825.0ms  | +0.6%  | 4.10%     | 4.15%     | OK     |
| GET /api/v1/catalog/search   | 165.0ms  | 168.0ms  | +1.8%  | 0.18%     | 0.18%     | OK     |
| GET /api/v1/users/{id}       | 72.0ms   | 73.5ms   | +2.1%  | 0.05%     | 0.05%     | OK     |
+------------------------------+----------+----------+--------+-----------+-----------+--------+
```

---

### 2. Live System Triage (`azlens top`)
Inspect current latency bottlenecks, N+1 queries, latency breakdown, and errors:

```bash
# Slowest API endpoints ordered by P95 latency (positional duration or default 1h)
azlens top endpoints 30m -n 10
azlens top endpoints

# Detect N+1 queries across endpoints
azlens top n-plus-one 1h
azlens top n-plus-one 1h -o markdown

# Break down where time is spent (% Database, % External APIs, % Cache, % App Code)
azlens top breakdown 2h
azlens top breakdown -o markdown

# Slow database queries (SQL, Redis, Cosmos DB) & remote HTTP dependencies
azlens top queries 2h --type SQL
azlens top queries --type all -o markdown

# MySQL engine slow query logs (MySqlSlowLogs table in Log Analytics)
azlens top slow-logs 2h
azlens top slow-logs 2h -o markdown

# Slow logs grouped by normalized SQL fingerprint: execution count,
# avg/max/total duration, and rows examined per query shape (literals
# masked, ordered by total accumulated duration — highest impact first)
azlens top slow-logs 2h --grouped
azlens top slow-logs 2h --grouped -o markdown

# Grouped exceptions and HTTP 5xx errors
azlens top errors 1h

# Framework and library deprecation warnings (for runtime & library upgrades)
azlens top deprecations 24h
azlens top deprecations -o markdown
```

**Latency Breakdown Preview (`azlens top breakdown`):**
```text
+------------------------------+-----------+------------+------------+---------+------------+
|           ENDPOINT           | AVG TOTAL | % DATABASE | % EXT APIS | % CACHE | % APP CODE |
+------------------------------+-----------+------------+------------+---------+------------+
| POST /api/v1/orders/checkout | 480.0ms   | 12.5%      | 78.5%      | 2.0%    | 7.0%       |
| GET /api/v1/orders           | 320.0ms   | 82.0%      | 0.0%       | 4.5%    | 13.5%      |
| GET /api/v1/catalog/search   | 85.0ms    | 25.0%      | 0.0%       | 65.0%   | 10.0%      |
+------------------------------+-----------+------------+------------+---------+------------+
```

---

## ⏱️ Default Timeframes (Convention over Configuration)

AzLens uses sensible defaults out of the box:

| Command | Default Time Window | Override |
| :--- | :--- | :--- |
| **`azlens top`** (`endpoints`, `queries`, `slow-logs`, `n-plus-one`, `breakdown`, `errors`, `deprecations`) | Last hour | Positional duration (e.g. `30m`, `2h`) or `defaults.window` |
| **`azlens deploy-check`** (baseline vs post-deploy) | Last hour vs the hour before it | Positional duration or `defaults.since`; center on a deploy time with `--at` |

### 🎨 Terminal Output & Colors

Tables are rendered with a width-aware engine: they adapt to your terminal (shrinking flexible text columns instead of overflowing on narrow screens), right-align numeric columns, humanize generic headers (`TotalCalls` → `Total Calls`, `QueryDurationMs` → `Query Duration (ms)`), and colorize results by severity. When output is piped or redirected, colors turn off automatically and no truncation is applied.

```bash
azlens top endpoints 1h --color auto    # default: color only on a TTY (honors NO_COLOR)
azlens top slow-logs 2h --color always  # force ANSI colors (e.g. when piping through less -R)
azlens deploy-check 2h --color never    # plain output for logs/CI
```

### 🚦 Severity & Color Reference

Every colored value maps to one standardized band (single source of truth in `pkg/reporter/thresholds.go`), anchored on industry standards so a color means the same thing in every table:

| Indicator | 🟢 Healthy | 🟡 Review | 🔴 Critical | Basis |
| :--- | :--- | :--- | :--- | :--- |
| Error rate (5xx / exceptions) | < 1% | 1 – 5% | ≥ 5% | SRE error budget (99% SLO tier) + APM alert defaults |
| API / gRPC latency | < 300ms | 300ms – 1s | ≥ 1s | API latency tiers, universal 1-second rule |
| DB statement duration | < 100ms | 100ms – 1s | ≥ 1s | MySQL `long_query_time` analysis standard |
| Cache ops (Redis/Memcached) | < 5ms | 5 – 25ms | ≥ 25ms | Sub-millisecond round-trip budget |
| Scan ratio (rows examined / returned) | < 100× | 100 – 1000× | ≥ 1000× | EXPLAIN / Percona missing-index heuristic |
| SQL calls per request (N+1) | < 5 | 5 – 15 | ≥ 15 | azlens analyzer fan-out defaults |
| Latency regression Δ% | ≤ −15% (improved) | +15% | +30% | azlens analyzer regression thresholds |
| N+1 spike Δ% | — | +40% | +100% | azlens analyzer fan-out regression thresholds |

Status columns additionally carry the verdict colors: `OK`/`IMPROVED` green, `WARNING` yellow, `CRITICAL` red. Latency bands are applied per dependency class (`top queries`): SQL → DB band, Redis → cache band, HTTP/gRPC → API band. Colors are indicators, not gates — always pair them with counts and the `json` output for alerting.

---

## ⚙️ Configuration & Project Scoping (`azlens.yaml`)

Following **Convention over Configuration**, the config file is the **single source of truth** for Azure targets, scopes, thresholds, and operational defaults — and AzLens still works out of the box without one (built-in defaults). The CLI surface stays minimal (`-p` to switch profiles, `-o` to change output format, positional durations, and `-c` to point at an explicit config file — which must exist, there is no silent fallback).

To configure project-specific scopes, Azure IDs, and operational defaults, create **`azlens.yaml`** in your project root (clear naming, no leading dot — meant to be **committed** and shared by the team):

```bash
azlens config init
```

### Configuration Structure (`version`, `defaults`, `shared`, `profiles`)

Declare **once** in `shared` everything that does **not** vary across environments; each profile only declares what differs (typically `insights.name` and `logs.workspace_id`). Any shared field can be overridden per profile when needed.

```yaml
version: "1.0"

# Operational defaults (inherited by all profiles)
defaults:
  profile: prod          # Active default profile (override with --profile / -p)
  window: "1h"           # Default timeframe for 'azlens top' (override with positional arg)
  since: "1h"            # Default comparison duration for 'azlens deploy-check' (override with positional arg)
  limit: 15              # Default number of rows returned in tables (override with --limit / -n)
  output: "table"        # Output format: table | markdown | json (override with --output / -o)

# Shared target: set once, inherited by every profile
shared:
  insights:
    directory_id: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa" # Entra directory hosting App Insights (login + per-query token routing)
    subscription_id: "11111111-aaaa-bbbb-cccc-111111111111" # Subscription hosting App Insights (routes the query)
  logs:
    directory_id: "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb" # Entra directory hosting Log Analytics
    subscription_id: "22222222-dddd-eeee-ffff-222222222222" # Subscription hosting Log Analytics (routes the query)
    namespace: "ecommerce"         # Kubernetes namespace
    database: "backend_ror"        # Database name for slow query logs (MySqlSlowLogs)
  roles: ""                      # cloud_RoleName(s): EXACT microservice names — scalar or list (empty = all services)
  pods: ""                       # Pod names WITHOUT the deployment hash, token-matched — scalar or list (empty = all pods)
  exclude_synthetic: true
  exclude_probes: true

  # Quality gate policy shared by every profile (per-profile overrides allowed)
  thresholds:
    p95_latency_warn_pct: 15.0
    p95_latency_crit_pct: 30.0
    error_rate_warn_delta: 1.0
    error_rate_crit_delta: 3.0
    min_sample_calls: 5

# Environment targets: only what differs per environment
profiles:
  prod:
    name: "Production Microservices"
    target:
      insights:
        name: "app-shared-prod"  # Resource name from the portal URL (or App ID GUID)
      logs:
        workspace_id: "33333333-hhhh-iiii-jjjj-333333333333" # Log Analytics workspace Customer ID GUID
    # prod inherits the shared thresholds — declare nothing here

  staging:
    name: "Staging Environment"
    target:
      insights:
        name: "app-shared-staging"
      logs:
        workspace_id: "44444444-aaaa-bbbb-cccc-444444444444"
      # Optional per-environment overrides of any shared field:
      # roles: billing-service
    defaults:
      window: "30m"
      since: "15m"
    # Looser quality gate for staging: overrides only what differs
    thresholds:
      p95_latency_warn_pct: 25.0
      p95_latency_crit_pct: 50.0
      error_rate_warn_delta: 2.0
      error_rate_crit_delta: 5.0

  dev:
    name: "Development Environment"
    target:
      insights:
        name: "app-shared-dev"
      logs:
        workspace_id: "55555555-eeee-ffff-dddd-555555555555"
```

> **Note:** `target.insights.name` accepts the App Insights resource name (as seen in the portal URL) or its App ID GUID, but `target.logs.workspace_id` requires the Log Analytics workspace **Customer ID (GUID)** — the "Workspace ID" shown in Portal → Log Analytics workspace → Overview. Retrieve it with `az monitor log-analytics workspace show -g <rg> -n <name> --query customerId -o tsv`.

### Target Filters — What Each Option Actually Filters

| Option | KQL column filtered | Applies to |
| :--- | :--- | :--- |
| `roles` | `cloud_RoleName` — **exact** microservice names, scalar or list (single: `=~`, multiple: `in~`) | App Insights: requests, dependencies, exceptions, traces |
| `pods` | `cloud_RoleInstance` — token match on the pod name **without** the deployment hash, scalar or list (single: `has`, multiple: `has_any`) | App Insights + container logs |
| `logs.namespace` | `customDimensions['Kubernetes.Namespace']` / `PodNamespace` | Log Analytics (+ App Insights customDimensions) |
| `logs.database` | `Db` / `DatabaseName_s` | `MySqlSlowLogs` (Log Analytics) |
| `resource_id` | `_ResourceId` | Log Analytics multi-resource workspaces |
| `exclude_synthetic` | `operation_SyntheticSource` / `syntheticSource` | App Insights (availability tests) |
| `exclude_probes` | `kube-probe` User-Agent + `/healthz`-style routes | App Insights |
| `custom_dimensions` | `customDimensions['<key>'] =~ '<value>'` | App Insights |

Without `roles`, queries scan telemetry from **all** microservices reporting to the App Insights resource — azlens warns about this at startup.

Both YAML forms are equivalent — scalar or list:

```yaml
shared:
  roles: order-service                     # single microservice (exact name)
  # or
  roles: [order-service, billing-service]  # several microservices
  pods: order-service                      # matches order-service-7c9d, order-service-8f2e, ... (no hash needed)
```

Filter by one of the configured roles/pods — or any other — for a single run without editing the config: `--role` / `--pod` **replace** the configured list for that run (repeatable or comma-separated):

```bash
# Only ONE of the several configured roles
azlens top endpoints 1h --role billing-service

# Only ONE pod (or a specific one)
azlens top endpoints 1h --pod order-service

# Several at once
azlens top endpoints 1h --role order-service,billing-service
azlens top errors 24h --role order-service --role billing-service

# Compare deploy health for a single microservice
azlens deploy-check 4h --role order-service
```

### Profile Management Commands
```bash
# List all configured profiles and see which one is active
azlens config list

# Show active profile configuration details
azlens config show

# Override the profile on the fly for any single command
azlens top endpoints -p staging
azlens deploy-check -p staging
```

### Environment & Authentication Diagnostics (`azlens doctor`)
```bash
# Verify Azure CLI installation, extensions (application-insights, log-analytics),
# login session, and validate the active profile configuration
azlens doctor
```

---

## 🔄 Self-Update

Keep `azlens` up to date with the built-in update command:

```bash
# Check for updates and automatically upgrade the binary in-place
azlens update

# Only check if a new version is available
azlens update --check

# Check current installed version
azlens version
```

---

## 🔐 Multi-Tenant & Multi-Subscription Setups (`directory_id`)

For cross-tenant setups (App Insights in tenant A, Log Analytics in tenant B), configure each backend's `directory_id` (and its `subscription_id`) once in `shared`. azlens handles multi-tenant workflows seamlessly following official Azure CLI practices:

- **Single shared az session (`~/.azure`)**: your normal terminal logins and installed extensions (`log-analytics`, `application-insights`) are directly shared without duplicating credentials or profiles.
- **Seamless context switching (`az account set`)**: before querying each backend, azlens ensures the active subscription context in the Azure CLI is aligned (`az account set --subscription <id>`), guaranteeing that extension data-plane queries acquire the right token for the right tenant.
- **Session coverage pre-flight**: checks that all configured subscriptions are available in your Azure session (`az account list --all`).
- **Interactive login on demand**: on a terminal, if a subscription is missing from your session, azlens launches `az login --tenant <directory_id>` and continues seamlessly.
- **CI / non-interactive runs**: fails fast with `subscription(s) not in the active az session: <id>` and clean, actionable `az login --tenant ...` commands.
- **`azlens doctor`**: reports Azure CLI status, extension availability, and per-subscription session coverage (✓ / ✗).

To authenticate additional tenants in your normal terminal at any time:

```bash
az login --tenant <directory-id>
```

> **Note on query cost:** AzLens queries scan the Log Analytics / App Insights tables you point them at. Large time windows (e.g. `deploy-check 24h`) and workspaces on pay-as-you-go plans incur real query costs; prefer the smallest window that answers your question.

---

## 🧯 Troubleshooting

| Symptom | Likely Cause | Fix |
| :--- | :--- | :--- |
| `'log-analytics' is mispelled or not recognized by the system` (same for `app-insights`) | The az CLI **extension** providing the query command is not installed | `az extension add --name log-analytics` and/or `az extension add --name application-insights` (azlens pre-flight and `doctor` detect this and print the same hint) |
| `azure authentication failed: session expired or not logged in` | `az` session expired or directory never authenticated | Re-run the command — azlens launches `az login --tenant <directory_id>` for you; or run `azlens doctor` for per-subscription coverage |
| `subscription(s) not in the active az session: <id>` | A configured subscription belongs to a directory not yet authenticated in your az session | On a terminal azlens launches the login automatically; otherwise run `az login --tenant <directory_id>` printed in the hint |
| `azure subscription not found in active account` | Target lives in another subscription/tenant | Set `target.insights.subscription_id` / `target.logs.subscription_id` (+ `directory_id`) in `azlens.yaml`; `az login --tenant` for the other directory |
| `azure resource not found` | Wrong `target.insights.name`, missing `resource_group`, or wrong `logs.workspace_id` | `target.logs.workspace_id` must be the **Customer ID (GUID)**; `target.insights.name` can be the App ID (GUID from portal API Access), full resource ID, or component name with `target.insights.resource_group` (or omit for workspace-based App Insights) |
| 403 / authorization errors on queries | Your identity lacks Reader roles on the resource | One-time, admin-only: ask the directory admin to grant **Monitoring Reader** (App Insights) / **Log Analytics Reader** (workspace) to the identity shown by `azlens doctor` |
| Empty tables for `top queries` | `--type` value not matching your dependency `type` | Use `SQL`, `HTTP`, `Redis`, `Cosmos`, or `all` (case-insensitive) |
| `MySqlSlowLogs` returns nothing | MySQL Flexible Server diagnostic settings not enabled | Enable the `MySqlSlowLogs` diagnostic category on the MySQL server; also set `target.logs.database` to filter |
| Queries return telemetry from all microservices | `target.roles` not set | Set `cloud_RoleName` isolation via `target.roles` (AzLens warns about this at startup) |

---

## ⚡ High-Performance & Safe KQL Engine

1. **Batched KQL Requests & Fast-Fail**: `deploy-check` fetches a full window (overall, endpoints, dependencies, exceptions, fan-out) in a **single KQL request** — 2 `az` CLI invocations instead of 10 — with both windows running in parallel and instant fast-fail cancellation on error.
2. **Noise Cancellation & Smart Grouping**: Automatically filters out client aborts (`ClientClosedRequest`), bot 404 scans, and health probes. Normalizes dynamic IDs (`<ID>`, `<UUID>`) and line numbers (`:<line>`) so telemetry groups into true counts.
3. **Partition Pruning**: Time-range filters (`timestamp between ...`) are placed at the root of every subquery to prevent full table scans.
4. **Token-Indexed Search**: Uses KQL `has` / `has_any` to leverage Azure's inverted term indexes, performing up to 10x faster than full string scans.
5. **Multi-Percentile Efficiency**: Uses native KQL `percentiles(duration, 50, 75, 90, 95, 99)` for fast single-pass evaluation.
6. **Universal Formatting**: First-class support for Terminal Tables with smart truncation, GitHub/GitLab Markdown, and raw JSON across every single command.
7. **Strict Parameter Sanitization**: Escapes quotes, strips newlines and statement separators, and enforces a table-name whitelist to keep generated KQL safe.

