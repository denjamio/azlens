# 🚀 AzLens

> **Does this environment need my attention right now?**

**AzLens** is a simple, opinionated, actionable operational CLI for Azure environments.

AzLens is not a terminal dashboard and does not dump raw telemetry by default. It interrogates **Azure Monitor, Application Insights, and Log Analytics**, interprets the signals, and returns:

$$\text{problem} \rightarrow \text{impact} \rightarrow \text{likely cause} \rightarrow \text{evidence} \rightarrow \text{next action}$$

---

## ⚡ Quick Install

Install the latest release directly to your `PATH`:

```bash
curl -sSL https://raw.githubusercontent.com/denjamio/azlens/main/install.sh | bash
```

Or build locally with Docker (zero host dependencies required):
```bash
make build
```

---

## 🧠 Core Principles

1. **One problem, one story**: Multiple related symptoms collapse into a single operational problem whenever they describe the same user-visible degradation (e.g. an endpoint latency regression caused by a failing downstream dependency is reported as one story, not five disjoint alerts).
2. **Silence is a feature**: If nothing needs attention, AzLens gets out of your way with a concise, clean confirmation (`Everything looks normal.`).
3. **No magic scores**: There is no arbitrary health percentage or metric soup. Environments are strictly **`healthy`**, **`degraded`**, or **`unknown`**.
4. **Actionability over raw data**: Every finding provides an immediate, copy-pasteable next step command.

```text
🚀 azlens: Operational Health Check
 ├──► 🔍 azlens explain   ──  Root cause analysis & evidence
 ├──► 📊 azlens inspect   ──  Deep telemetry investigation
 ├──► 🚀 azlens deploy    ──  Release regression verification
 ├──► 🩺 azlens doctor    ──  Environment coverage & reachability
 └──► ⚙️ azlens config    ──  Team-shared profile management
```

---

## 📋 Command Reference

### 1. Primary Operational Command (`azlens [window] [-s SERVICE]`)

Evaluate current operational health across all configured capabilities:

```bash
# Check current operational status over default window (last 1h)
azlens

# Target a specific microservice with -s / --service
azlens -s checkout
azlens 30m -s checkout

# Target a specific environment profile
azlens -p prod -s checkout
azlens -p staging -s checkout

# Machine-readable output (JSON schema v1 or Markdown)
azlens -o json
azlens -o markdown
```

**Healthy Output (Silence is a feature):**
```text
Production · checkout · last 60m

Everything looks normal.
```

**Degraded Output (Actionable problem story):**
```text
Production · checkout · last 60m

Needs Attention:
[1] POST /api/v1/orders/checkout degraded by api.stripe.com failures
    Impact:       p95 latency increased by 140% (380ms -> 912ms); 4.2% error rate
    Likely cause: HTTP dependency 'api.stripe.com' error rate increased to 5.8% (+5.7pp)
    Evidence:
      • POST /api/v1/orders/checkout p95 latency: 912ms (baseline 380ms, +140%)
      • api.stripe.com dependency errors: 5.8% (baseline 0.1%, +5.7pp)
    Next action:
      -> azlens explain api.stripe.com -s checkout

Worth Watching:
[1] NoMethodError in /api/v1/user/settings (12 occurrences)
    Impact:       Negligible (< 0.05% of request volume)
    Next action:
      -> azlens explain NoMethodError -s checkout
```

---

### 2. Root Cause Analysis (`azlens explain [subject] [window] [-s SERVICE]`)

Explain why an operational problem is happening and show deep supporting evidence:

```bash
# Explain the highest-priority operational problem
azlens explain

# Explain a specific service, endpoint, dependency, or exception
azlens explain checkout -s checkout
azlens explain api.stripe.com -s checkout
azlens explain NpgsqlException 2h -s checkout
```

**Deterministic Ambiguity Protection:**
If a query subject matches multiple components (e.g. `order`), AzLens does not guess:
```text
"order" matches:

  POST /orders/checkout (endpoint)
  sqldb-orders (dependency)

Be more specific.
```

---

### 3. Direct Telemetry Inspection (`azlens inspect <view> [window] [-s SERVICE]`)

Drill down into inspectable operational evidence, ordered by operational impact:

```bash
# Slowest API endpoints ordered by P95 latency and error rates
azlens inspect endpoints 30m -n 10 -s checkout

# Slow database queries and external HTTP/Redis dependencies
azlens inspect dependencies 1h --type SQL -s checkout
azlens inspect dependencies 1h --type all -s checkout

# Database engine slow query logs ordered by duration descending (MySqlSlowLogs in Log Analytics)
azlens inspect slow-queries 2h -s checkout
# (Supported aliases: azlens inspect queries, azlens inspect slow-logs)

# Detect N+1 queries across endpoints
azlens inspect n-plus-one 1h -s checkout

# Latency breakdown across Database, External APIs, Cache, and App Code
azlens inspect breakdown 2h -s checkout

# Grouped application exceptions and HTTP 5xx errors
azlens inspect errors 1h -s checkout

# Framework, language, and library deprecation warnings
azlens inspect deprecations 24h -s checkout
```

---

### 4. Post-Deploy Regression Verifier (`azlens deploy [window] [--at TIME] [-s SERVICE]`)

Compare telemetry between two equal time windows (before vs after release) to verify deployment safety:

```bash
# Compare the last 30 minutes vs the 30 minutes before
azlens deploy 30m -s checkout

# Center comparison on a deployment timestamp
azlens deploy 30m --at 14:30 -s checkout
azlens deploy 30m --at -20m -s checkout

# Target production profile and output as Markdown for PR / CI
azlens deploy 30m -p prod -s checkout -o markdown
```

**Clean Verdicts:**
- Healthy: `This deploy looks safe.`
- Regressed: `This deploy made POST /orders/checkout worse.` (only changed signals are shown!)
- Insufficient data: `Insufficient baseline data to verify safety.`

---

### 5. Diagnostics & Coverage (`azlens doctor`)

Verify Azure CLI installation, authentication, reachability, and inspect APM capabilities:

```bash
azlens doctor
```

Reports status across the 5 genuine APM capabilities:
- `requests` (Application Insights requests & latency percentiles)
- `dependencies` (SQL, Redis, Cosmos DB, and HTTP calls)
- `exceptions` (Application error signatures and 5xx spikes)
- `availability` (Synthetic availability probe health)
- `database_slow_logs` (Engine slow query logs in Log Analytics)

---

### 6. Supporting Commands

```bash
# Profile and configuration management
azlens config init       # Create starter azlens.yaml with prod, staging, and dev profiles
azlens config list       # List configured profiles and active profile
azlens config profiles   # Compact profile list
azlens config show       # Show active configuration (secrets redacted)
azlens config path       # Print path to active config file

# Self-upgrade
azlens upgrade           # Check and upgrade binary in-place to latest GitHub release
azlens upgrade --check   # Check if a new version is available without installing

# Version
azlens version           # Print build version, commit, and date
```

---

## 🚦 Process Exit Codes

AzLens exit codes are pipeline-aware and designed for CI/CD gates:

| Exit Code | Meaning | Use Case |
| :---: | :--- | :--- |
| **`0`** | **Healthy / Safe** | Observed window has no actionable problems or deploy regressions. |
| **`1`** | **Failure / Error** | Configuration error, query execution failure, or invalid arguments. |
| **`2`** | **Actionable Problem / Regression** | Environment degraded or post-deploy regression detected. |
| **`3`** | **Insufficient Data / Unknown** | Telemetry missing, stale, or baseline insufficient to determine health. |

### Example GitHub Actions Workflow

```yaml
- name: Verify Deployment Health
  shell: bash
  run: |
    set -o pipefail
    azlens deploy 30m -p prod -s checkout -o markdown | tee deploy-verdict.md
```

---

## ⚙️ Configuration & Service Catalog (`azlens.yaml`)

AzLens follows **Convention over Configuration**. The config file is the single source of truth for environments:

```yaml
# ─────────────────────────────────────────────────────────────────────────────
# AzLens Configuration Reference (azlens.yaml)
# Explanatory comments are consolidated in this header block.
# - defaults: operational defaults (profile, service, window, limit, output)
# - shared: targets, credentials, services, and policies declared once
# - shared.services.<service>.database: MySQL slow query logs tenant database (required per service)
# - shared.services: maps service names to role_name (cloud_RoleName) and database
# - profiles.*.insights.name: App Insights resource name or App ID GUID
# - profiles.*.logs.workspace_id: Log Analytics workspace Customer ID GUID
# ─────────────────────────────────────────────────────────────────────────────

version: "1.0"

defaults:
  profile: prod
  service: checkout
  window: "1h"
  limit: 15
  output: "table"

# Shared targets: declared ONCE, inherited by all profiles
shared:
  insights:
    resource_group: "rg-shared-prod"
    directory_id: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
    subscription_id: "11111111-aaaa-bbbb-cccc-111111111111"
  logs:
    directory_id: "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
    subscription_id: "22222222-dddd-eeee-ffff-222222222222"

  # Service catalog: maps logical services to physical telemetry targets
  services:
    checkout:
      role_name: checkout-service
      database: checkout_db
    orders:
      role_name: order-service
      database: orders_db
    billing:
      role_name: billing-service
      database: billing_db

  thresholds:
    p95_latency_warn_pct: 15.0
    p95_latency_crit_pct: 30.0
    error_rate_warn_delta: 1.0
    error_rate_crit_delta: 3.0
    min_sample_calls: 5

profiles:
  prod:
    name: "Production"
    insights:
      name: "app-insights-prod"
    logs:
      workspace_id: "33333333-hhhh-iiii-jjjj-333333333333"

  staging:
    name: "Staging"
    insights:
      name: "app-insights-staging"
    logs:
      workspace_id: "44444444-aaaa-bbbb-cccc-444444444444"
```

### Singular Service Flag (`-s` / `--service`)
- Target any service by name: `-s <name>` or `--service <name>`.
- The CLI automatically looks up the service in `shared.services` to apply the exact `cloud_RoleName` and target `database` filters.
- **Ad-hoc fallback**: If you pass a service name not present in the catalog (`-s custom-job`), AzLens uses `custom-job` for `role_name` matching.

### Dual-Layer Tenancy Enforcement
To guarantee multi-tenant safety and prevent unscoped cross-tenant queries:
1. **Config Validation**: `service.database` (`shared.services.<service>.database`) and an active service/role_name are mandatory (`SeverityError`). Newly installed or non-telemetry commands (`--help`, `version`, `config init`, `config profiles`, `config path`) remain lazy and non-blocking.
2. **KQL Query Firewall**: In `pkg/kql/builder.go`, application queries (`requests`, `dependencies`, `exceptions`, `traces`) refuse to build if no role filter is present (`ErrMissingRole`), and database slow log queries (`MySqlSlowLogs`) refuse to build without a database filter (`ErrMissingDatabase`).

### Profile Resolution Precedence
1. Explicit CLI flag: `--profile <name>` / `-p <name>`
2. Environment variable: `AZLENS_PROFILE`
3. Config defaults: `defaults.profile`
4. Single profile fallback: If exactly one profile exists in `azlens.yaml`
5. Fast error: Clear message with available profiles

---

## 🔐 Multi-Tenant & Multi-Subscription Azure Auth

AzLens integrates directly with the official Azure CLI (`az`):
- **Zero credential storage**: AzLens never stores tokens or passwords on disk.
- **Shared session (`~/.azure`)**: Automatically leverages existing terminal credentials.
- **Cross-tenant support**: Sets subscription context seamlessly via `az account set --subscription <id>`.
- **Interactive login**: Launches `az login --tenant <directory_id>` when sessions require re-authentication.

---

## ⚡ High-Performance KQL Engine

1. **Batched KQL Invocations**: Parallel single-request telemetry bundles minimize Azure CLI round trips.
2. **Noise Filtering**: Automatically drops client aborts (`ClientClosedRequest`), bot scans, and health probes.
3. **Partition Pruning**: Pushes time-range boundaries to root subqueries to avoid scanning full tables.
4. **Token Indexing**: Uses KQL `has` / `has_any` to exploit Azure's inverted term index.
5. **Strict Sanitization**: Neutralizes quote injection, escapes statement separators, and validates table whitelist.
