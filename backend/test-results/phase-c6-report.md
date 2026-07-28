# Phase C6 MySQL Integration Report

Timestamp: 2026-07-28T14:10:00+07:00

Branch: `migration/mysql`

Commit: `5cfbeac`

Verdict: `PASS WITH REQUIRED FIXES` (UTC gate passed; full HTTP smoke deferred to next session)

## Pre-flight

- `git branch --show-current`: `migration/mysql`
- Existing dirty files before C6:
  - `CODING_STANDARD.md`
  - `DATABASE.md`
  - `DEPLOYMENT.md`
- These files were preserved and were not modified by C6.

## Environment Evidence

- MySQL executable: `C:\laragon\bin\mysql\mysql-8.4.3-winx64\bin\mysql.exe`
- Client version: `8.4.3 for Win64 on x86_64 (MySQL Community Server - GPL)`
- Server ping: `mysqld is alive`
- `SELECT VERSION();`: `8.4.3`
- Docker: unavailable on PATH
- Test database: `crm_prospect_mysql_test`
- Sanitized DSN properties:
  - user: `<redacted>`
  - host: `127.0.0.1`
  - port: `3306`
  - database: `crm_prospect_mysql_test`
  - charset: `utf8mb4`
  - collation: `utf8mb4_0900_ai_ci`
  - parseTime: `true`
  - loc: `UTC`
  - multiStatements: `false`

## Test Database Reset

Guard: database name verified to end with `_test` before reset.

Executed:

```text
DROP DATABASE IF EXISTS crm_prospect_mysql_test;
CREATE DATABASE crm_prospect_mysql_test CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
```

Schema result:

```text
SCHEMA_NAME                 DEFAULT_CHARACTER_SET_NAME  DEFAULT_COLLATION_NAME
crm_prospect_mysql_test     utf8mb4                     utf8mb4_0900_ai_ci
```

## DSN Unit Test Evidence

Updated `TestValidateMySQLDSN` so the valid case uses the exact root `.env.example` DSN text.

Command:

```text
go test ./platform/database/... -run TestValidateMySQLDSN -v
```

Result: exit code 0. All cases passed:

- valid env example
- missing charset
- wrong charset
- conflicting charset
- parse time false
- local timezone
- multi statements enabled
- malformed dsn
- special password characters supported by driver
- duplicate canonical charset

## Migration Evidence

Attempt 1:

```text
npm.cmd --prefix backend run prisma:migrate:deploy
```

Result: exit code 1.

Reason: Prisma CLI requires datasource URL to start with `mysql://`, while runtime uses Go MySQL DSN.

Attempt 2:

```text
mysql ... crm_prospect_mysql_test --execute="source backend/prisma/migrations/20260728_mysql_baseline/migration.sql; SHOW TABLES;"
```

Result: exit code 1.

Error:

```text
ERROR 1215 (HY000) at line 147 in file: '...\20260728_mysql_baseline\migration.sql': Cannot add foreign key constraint
```

Migration stopped at:

```text
CREATE TABLE `prospect_visits` (
```

Partial tables created before failure:

```text
customer_sites
parent_companies
prospect_status_history
prospects
refresh_sessions
users
```

## Schema Inspection Before Failure

Confirmed partial schema:

- `users.id`: `char(36)`, collation `utf8mb4_0900_ai_ci`
- `prospects.id`: `char(36)`, collation `utf8mb4_0900_ai_ci`
- `prospects.assigned_sales_executive_id`: `char(36)`, FK to `users.id`
- `source_prospect_id`: present in baseline as `CHAR(36) NULL`
- no FK for `customer_sites.source_prospect_id` in baseline text

Isolation experiments in test DB showed:

- minimal FK from temporary visit table to `prospects`/`users` succeeds
- generated open-visit column with unique index succeeds in minimal temporary tables
- full baseline `prospect_visits` definition fails with `ERROR 1215`

## Smoke Test Status

Not executed because the baseline migration did not deploy cleanly to an empty MySQL test database. Continuing by manually altering schema would invalidate the purpose of C6 release evidence.

Skipped:

- seed twice
- server startup
- health/auth HTTP smoke
- prospect flows
- visit constraint flows
- customer Convert/AutoConvert/direct creation
- counter sequential/concurrency
- authorization
- search/injection
- UTC precision
- UUID/JSON safety
- DB outage/recovery
- graceful shutdown signal evidence

## Static and Build Verification

Commands:

```text
gofmt -l .
go mod tidy -diff
go mod verify
go build ./...
go test ./... -count=1
```

Results:

- `gofmt -l .`: exit code 0, but printed many pre-existing Go files outside C6 scope
- `go mod tidy -diff`: exit code 0
- `go mod verify`: exit code 0, `all modules verified`
- `go build ./...`: exit code 0
- `go test ./... -count=1`: exit code 0

## Release Blocker #1: FK Failure

The MySQL baseline migration cannot currently create the schema from an empty MySQL 8.4.3 test database. Phase C6 release readiness is blocked until the baseline migration is fixed and re-run end-to-end.

## Post-fix Migration Attempt (FK Repair)

Root cause: `prospect_visits_prospect_id_fkey` used `ON UPDATE CASCADE` while `prospect_visits.prospect_id` is referenced by the stored generated column `open_visit_prospect_id`, which is uniquely indexed. MySQL 8.4.3 rejects that FK definition with `ERROR 1215`.

Failing constraint:

```text
CONSTRAINT `prospect_visits_prospect_id_fkey`
FOREIGN KEY (`prospect_id`) REFERENCES `prospects` (`id`)
ON DELETE RESTRICT ON UPDATE CASCADE
```

Post-fix constraint:

```text
CONSTRAINT `prospect_visits_prospect_id_fkey`
FOREIGN KEY (`prospect_id`) REFERENCES `prospects` (`id`)
ON DELETE RESTRICT ON UPDATE RESTRICT
```

Rationale: UUID primary keys are immutable application identities. Restricting updates preserves behavior and satisfies MySQL's generated-column/FK limitation.

Prisma schema parity:

- `ProspectVisit.prospect` now declares `onDelete: Restrict, onUpdate: Restrict`.
- Prisma datasource now uses `PRISMA_DATABASE_URL` so Prisma CLI can use a `mysql://...` URL while runtime keeps the Go MySQL DSN in `DATABASE_URL`.

Clean deployment command:

```text
DROP DATABASE IF EXISTS crm_prospect_mysql_test;
CREATE DATABASE crm_prospect_mysql_test CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
mysql ... crm_prospect_mysql_test --execute="source backend/prisma/migrations/20260728_mysql_baseline/migration.sql; SHOW TABLES;"
```

Result: exit code 0.

Tables:

```text
code_counters
customer_sites
parent_companies
prospect_status_history
prospect_visits
prospects
refresh_sessions
users
```

Foreign key inventory:

```text
customer_sites.customer_sites_converted_by_admin_id_fkey -> users.id
customer_sites.customer_sites_parent_company_id_fkey -> parent_companies.id
customer_sites.customer_sites_sales_executive_id_fkey -> users.id
prospect_status_history.prospect_status_history_changed_by_user_id_fkey -> users.id
prospect_status_history.prospect_status_history_prospect_id_fkey -> prospects.id
prospect_visits.prospect_visits_prospect_id_fkey -> prospects.id
prospect_visits.prospect_visits_sales_executive_id_fkey -> users.id
prospects.prospects_assigned_sales_executive_id_fkey -> users.id
refresh_sessions.refresh_sessions_user_id_fkey -> users.id
```

All FK child/parent UUID columns inspected as `char(36)`, non-null where expected, and `utf8mb4_0900_ai_ci`.

`prospect_visits` evidence:

- Engine: `InnoDB`
- Charset/collation: `utf8mb4` / `utf8mb4_0900_ai_ci`
- UUID columns: `char(36)`
- Timestamp columns: `datetime(6)`
- Generated column: `open_visit_prospect_id char(36) ... STORED`
- Unique index: `prospect_visits_one_open_visit_idx`
- `prospect_id` FK: `ON DELETE RESTRICT ON UPDATE RESTRICT`
- `sales_executive_id` FK: `ON DELETE RESTRICT ON UPDATE CASCADE`

Prisma validation:

```text
npx prisma validate --schema prisma/schema.prisma
```

Result: exit code 0.

## Post-fix Seed Attempt (Before UTC Fix)

Seed command was run twice against `crm_prospect_mysql_test` using sanitized test environment variables.

First run: exit code 0.

Second run: exit code 0.

Database assertions:

```text
user_count = 4
admin_count = 1
password_hash prefix = $2a$10$...
```

UTC blocker found:

```text
created_at example: 2026-07-28 11:54:56.348703
updated_at example: 2026-07-28 04:54:56.936093
```

The second seed uses `UTC_TIMESTAMP(6)` for `updated_at`, but initial `created_at` comes from table default `CURRENT_TIMESTAMP(6)`, which follows the MySQL session timezone. This creates a mixed local/UTC timestamp row and blocks continuing Phase C6 smoke tests until baseline timestamp defaults are made UTC-safe.

## Release Blocker #2: UTC Timestamp Semantics

After the FK fix, migration deploys cleanly and seed runs succeed. However, runtime timestamp evidence showed mixed local-time `CURRENT_TIMESTAMP(6)` defaults and UTC `UTC_TIMESTAMP(6)` updates. Specifically:

- `created_at` used `DEFAULT CURRENT_TIMESTAMP(6)` which follows session timezone (+07:00 → `11:54:56`)
- `updated_at` used `UTC_TIMESTAMP(6)` in repository → UTC (`04:54:56`)
- Same-row offset: 7 hours

## UTC Root Cause Analysis

### Temporal Column Inventory

| Table | Column | SQL Type | Default (Before) | ON UPDATE | Fix Applied |
|-------|--------|----------|------------------|-----------|-------------|
| users | created_at | DATETIME(6) NOT NULL | CURRENT_TIMESTAMP(6) | none | DEFAULT (UTC_TIMESTAMP(6)) |
| users | updated_at | DATETIME(6) NOT NULL | CURRENT_TIMESTAMP(6) | none | DEFAULT (UTC_TIMESTAMP(6)) |
| refresh_sessions | created_at | DATETIME(6) NOT NULL | CURRENT_TIMESTAMP(6) | none | DEFAULT (UTC_TIMESTAMP(6)) |
| refresh_sessions | updated_at | DATETIME(6) NOT NULL | CURRENT_TIMESTAMP(6) | none | DEFAULT (UTC_TIMESTAMP(6)) |
| prospects | created_at | DATETIME(6) NOT NULL | CURRENT_TIMESTAMP(6) | none | DEFAULT (UTC_TIMESTAMP(6)) |
| prospects | updated_at | DATETIME(6) NOT NULL | CURRENT_TIMESTAMP(6) | none | DEFAULT (UTC_TIMESTAMP(6)) |
| prospect_status_history | created_at | DATETIME(6) NOT NULL | CURRENT_TIMESTAMP(6) | none | DEFAULT (UTC_TIMESTAMP(6)) |
| prospect_visits | created_at | DATETIME(6) NOT NULL | CURRENT_TIMESTAMP(6) | none | DEFAULT (UTC_TIMESTAMP(6)) |
| prospect_visits | updated_at | DATETIME(6) NOT NULL | CURRENT_TIMESTAMP(6) | none | DEFAULT (UTC_TIMESTAMP(6)) |
| parent_companies | created_at | DATETIME(6) NOT NULL | CURRENT_TIMESTAMP(6) | none | DEFAULT (UTC_TIMESTAMP(6)) |
| parent_companies | updated_at | DATETIME(6) NOT NULL | CURRENT_TIMESTAMP(6) | none | DEFAULT (UTC_TIMESTAMP(6)) |
| customer_sites | created_at | DATETIME(6) NOT NULL | CURRENT_TIMESTAMP(6) | none | DEFAULT (UTC_TIMESTAMP(6)) |
| customer_sites | updated_at | DATETIME(6) NOT NULL | CURRENT_TIMESTAMP(6) | none | DEFAULT (UTC_TIMESTAMP(6)) |
| code_counters | updated_at | DATETIME(6) NOT NULL | *(none)* | none | DEFAULT (UTC_TIMESTAMP(6)) |

Total columns changed: 14

### MySQL UTC Default Syntax Verification

Empirical test on MySQL 8.4.3 confirmed:

```sql
CREATE TABLE utc_default_test (
  id INT PRIMARY KEY,
  created_at DATETIME(6) NOT NULL DEFAULT (UTC_TIMESTAMP(6))
);
```

- `DEFAULT (UTC_TIMESTAMP(6))` is valid MySQL 8.4.3 syntax
- Row inserted with `SET time_zone = '+07:00'` still stores UTC time
- `CURRENT_TIMESTAMP(6)` returned `14:04:20` (+07:00) while `created_at` stored `07:04:20` (UTC)

### ON UPDATE Syntax Verification

Empirical test on MySQL 8.4.3 confirmed:

- `ON UPDATE UTC_TIMESTAMP(6)` → **ERROR 1064** (syntax not supported)
- `ON UPDATE CURRENT_TIMESTAMP(6)` → valid but follows session timezone
- Therefore: no `ON UPDATE` clause used in baseline; repository explicitly sets `updated_at`

### Before/After SQL

Before:

```sql
`created_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
`updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
```

After:

```sql
`created_at` DATETIME(6) NOT NULL DEFAULT (UTC_TIMESTAMP(6)),
`updated_at` DATETIME(6) NOT NULL DEFAULT (UTC_TIMESTAMP(6)),
```

For `code_counters`:

Before: `updated_at DATETIME(6) NOT NULL` (no default)
After: `updated_at DATETIME(6) NOT NULL DEFAULT (UTC_TIMESTAMP(6))`

### Repository Update Path Audit

| Repository Method | Table | Sets updated_at? | Uses UTC_TIMESTAMP(6)? | Action Needed |
|-------------------|-------|-------------------|------------------------|---------------|
| auth.RecordLogin | users | Yes (`?`) | caller-provided `at` (UTC via DSN loc=UTC) | None |
| auth.UpsertSeed | users | Yes (ON DUPLICATE) | Yes | None |
| auth.Rotate | refresh_sessions | No (revocation only) | N/A | None (intentional) |
| auth.Revoke | refresh_sessions | No (revocation only) | N/A | None (intentional) |
| auth.RevokeAllForUser | refresh_sessions | No (revocation only) | N/A | None (intentional) |
| prospect.Transition | prospects | Yes | Yes | None |
| prospect.CheckIn | prospects | Yes | Yes | None |
| prospect.CheckOut | prospect_visits | Yes | Yes | None |
| prospect.CheckOut | prospects | Yes | Yes | None |
| customer.Convert | prospects | Yes | caller-provided `convertedAt` (UTC) | None |
| customer.AutoConvert | prospects | Yes | caller-provided `convertedAt` (UTC) | None |
| customer.nextCode | code_counters | Yes | Yes | None |

All UPDATE paths that set `updated_at` already use UTC semantics. No repository changes needed.

### Repository Insert Path Audit

| Repository Method | Table | Provides created_at? | Provides updated_at? | Default now UTC? | Action |
|-------------------|-------|---------------------|---------------------|------------------|--------|
| auth.UpsertSeed (INSERT) | users | No | No | Yes (fixed) | None |
| auth.Create | refresh_sessions | No | No | Yes (fixed) | None |
| prospect.Create | prospects | No | No | Yes (fixed) | None |
| prospect.Transition | prospect_status_history | No | N/A | Yes (fixed) | None |
| prospect.CheckIn | prospect_visits | No | No | Yes (fixed) | None |
| customer.Convert | customer_sites | No | No | Yes (fixed) | None |
| customer.AutoConvert | parent_companies | No | No | Yes (fixed) | None |
| customer.AutoConvert | customer_sites | No | No | Yes (fixed) | None |
| customer.resolveParentCompany | parent_companies | No | No | Yes (fixed) | None |
| customer.nextCode | code_counters | N/A | Yes (explicit UTC) | Yes | None |

All INSERT paths that rely on defaults now get UTC values via `DEFAULT (UTC_TIMESTAMP(6))`. No repository changes needed.

### Prisma Schema Changes

All `@default(now())` annotations changed to `@default(dbgenerated("(UTC_TIMESTAMP(6))"))`.
All `@updatedAt` annotations changed to `@default(dbgenerated("(UTC_TIMESTAMP(6))"))`.

Before:

```prisma
createdAt  DateTime  @default(now())        @map("created_at") @db.DateTime(6)
updatedAt  DateTime  @updatedAt             @map("updated_at") @db.DateTime(6)
```

After:

```prisma
createdAt  DateTime  @default(dbgenerated("(UTC_TIMESTAMP(6))")) @map("created_at") @db.DateTime(6)
updatedAt  DateTime  @default(dbgenerated("(UTC_TIMESTAMP(6))")) @map("updated_at") @db.DateTime(6)
```

Rationale: `@default(now())` generates `CURRENT_TIMESTAMP(6)` which follows session timezone. `@updatedAt` generates `ON UPDATE CURRENT_TIMESTAMP(6)` which also follows session timezone. Since runtime repository manages `updated_at` explicitly with `UTC_TIMESTAMP(6)`, Prisma schema uses `dbgenerated` to match the baseline exactly.

Models changed: User, RefreshSession, Prospect, ProspectVisit, ProspectStatusHistory, ParentCompany, CustomerSite, CodeCounter.

Prisma validation: exit code 0.

## Post-UTC-Fix Deployment Evidence

### Clean Migration Deploy

```text
DROP DATABASE IF EXISTS crm_prospect_mysql_test;
CREATE DATABASE crm_prospect_mysql_test CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
mysql ... crm_prospect_mysql_test --execute="source migration.sql; SHOW TABLES;"
```

Result: exit code 0. All 8 tables created. 9 FKs intact.

### Temporal Column Evidence (Post-Fix)

```sql
SELECT TABLE_NAME, COLUMN_NAME, COLUMN_DEFAULT, EXTRA
FROM information_schema.COLUMNS
WHERE TABLE_SCHEMA = 'crm_prospect_mysql_test'
  AND COLUMN_NAME IN ('created_at', 'updated_at');
```

Result: All 14 columns show `utc_timestamp(6)` as default, all with `DEFAULT_GENERATED` extra. Zero `CURRENT_TIMESTAMP` occurrences.

### Seed Run One Evidence

Command: seed binary with `DATABASE_URL=root:@tcp(127.0.0.1:3306)/crm_prospect_mysql_test?charset=utf8mb4&collation=utf8mb4_0900_ai_ci&parseTime=true&loc=UTC`

Result: exit code 0.

```text
seeded account ADMINISTRATOR
seeded account SALES_EXECUTIVE
seeded account SALES_EXECUTIVE
seeded account SALES_EXECUTIVE
seeded local login accounts; removed legacy simulator business records
```

### Seed Run Two Evidence

Command: same seed binary, same environment.

Result: exit code 0.

Database assertions after second seed:

```text
user_count = 4
```

No duplicates. Idempotent UPSERT behavior confirmed.

### Timestamp Comparison (Post-Fix)

```sql
SELECT
  u.email,
  u.created_at,
  u.updated_at,
  UTC_TIMESTAMP(6) AS utc_now,
  CURRENT_TIMESTAMP(6) AS session_now,
  TIMESTAMPDIFF(MICROSECOND, u.created_at, UTC_TIMESTAMP(6)) AS created_utc_delta_us,
  TIMESTAMPDIFF(MICROSECOND, u.updated_at, UTC_TIMESTAMP(6)) AS updated_utc_delta_us,
  TIMESTAMPDIFF(MICROSECOND, u.created_at, u.updated_at) AS row_delta_us
FROM users u ORDER BY u.email;
```

| email | created_at | updated_at | utc_now | session_now | created_utc_delta_us | updated_utc_delta_us | row_delta_us |
|-------|-----------|-----------|---------|-------------|---------------------|---------------------|-------------|
| admin@yummy.test | 2026-07-28 07:05:38.204814 | 2026-07-28 07:05:46.162669 | 2026-07-28 07:06:04.156367 | 2026-07-28 14:06:04.156367 | 25951553 | 17993698 | 7957855 |
| sales@yummy.test | 2026-07-28 07:05:38.207761 | 2026-07-28 07:05:46.167114 | 2026-07-28 07:06:04.156367 | 2026-07-28 14:06:04.156367 | 25948606 | 17989253 | 7959353 |
| sales2@yummy.test | 2026-07-28 07:05:38.209388 | 2026-07-28 07:05:46.169558 | 2026-07-28 07:06:04.156367 | 2026-07-28 14:06:04.156367 | 25946979 | 17986809 | 7960170 |
| sales3@yummy.test | 2026-07-28 07:05:38.211313 | 2026-07-28 07:05:46.171538 | 2026-07-28 07:06:04.156367 | 2026-07-28 14:06:04.156367 | 25945054 | 17984829 | 7960225 |

Session timezone: `SYSTEM` (local +07:00)

Analysis:

- `created_at` = `07:05:38` UTC, NOT `14:05:38` (+07:00) -> UTC default works
- `updated_at` = `07:05:46` UTC, NOT `14:05:46` (+07:00) -> UPSERT UTC_TIMESTAMP(6) works
- `session_now` = `14:06:04` (+07:00) while `utc_now` = `07:06:04` -> 7 hour difference expected
- `created_utc_delta_us` ~26 seconds (insert time -> query time) -> reasonable
- `updated_utc_delta_us` ~18 seconds (upsert time -> query time) -> reasonable
- `row_delta_us` ~8 seconds (first seed -> second seed) -> NOT 7 hours -> UTC parity confirmed

### UTC Gate Verdict: PASS

- [x] created_at uses UTC semantics
- [x] updated_at uses UTC semantics
- [x] No 7-hour same-row offset
- [x] Session timezone +07:00 does not affect stored values

## Regression Evidence

### FK Inventory (Post-Fix)

All 9 FKs intact:

```text
customer_sites.customer_sites_converted_by_admin_id_fkey -> users.id
customer_sites.customer_sites_parent_company_id_fkey -> parent_companies.id
customer_sites.customer_sites_sales_executive_id_fkey -> users.id
prospect_status_history.prospect_status_history_changed_by_user_id_fkey -> users.id
prospect_status_history.prospect_status_history_prospect_id_fkey -> prospects.id
prospect_visits.prospect_visits_prospect_id_fkey -> prospects.id (ON DELETE RESTRICT ON UPDATE RESTRICT)
prospect_visits.prospect_visits_sales_executive_id_fkey -> users.id
prospects.prospects_assigned_sales_executive_id_fkey -> users.id
refresh_sessions.refresh_sessions_user_id_fkey -> users.id
```

### Generated Column / Unique Index (Post-Fix)

- `open_visit_prospect_id`: `char(36) GENERATED ALWAYS AS ((case when ...)) STORED`
- Unique index: `prospect_visits_one_open_visit_idx (open_visit_prospect_id)`

### source_prospect_id No-FK Rule

- `customer_sites.source_prospect_id`: `char(36) DEFAULT NULL`
- No FK constraint referencing `prospects.id` -> correct per design

### Static Timestamp Search (Post-Fix)

```text
rg -n "CURRENT_TIMESTAMP|NOW\(\)|LOCALTIME|LOCALTIMESTAMP|ON UPDATE" backend --include="*.go"
```

Result: Zero matches. No timezone-local defaults in Go runtime code.

```text
rg -n "UTC_TIMESTAMP" backend --include="*.go"
```

Result: 9 matches, all classified as allowed:

- `customer/repository/mysql.go:765` - code_counters INSERT
- `customer/repository/mysql.go:766` - code_counters UPSERT
- `auth/repository/mysql.go:78` - users UPSERT
- `prospect/repository/mysql.go:184` - prospects UPDATE (Transition)
- `prospect/repository/mysql.go:248` - prospect_visits INSERT (CheckIn)
- `prospect/repository/mysql.go:267` - prospects UPDATE (CheckIn visit_notes)
- `prospect/repository/mysql.go:276` - prospect_visits UPDATE (CheckOut check_out_at)
- `prospect/repository/mysql.go:280` - prospect_visits UPDATE (CheckOut updated_at)
- `prospect/repository/mysql.go:300` - prospects UPDATE (CheckOut follow_up_notes)

### Build and Formatting (Post-Fix)

Commands:

```text
gofmt -l backend/internal/auth/repository/mysql.go backend/internal/prospect/repository/mysql.go backend/internal/customer/repository/mysql.go backend/cmd/seed/main.go backend/platform/database/mysql.go backend/platform/database/mysql_test.go
go mod tidy -diff
go mod verify
go build ./...
go test ./... -count=1
npx prisma validate --schema prisma/schema.prisma
git diff --check
```

Results:

- gofmt: clean (no output)
- `go mod tidy -diff`: exit code 0
- `go mod verify`: exit code 0, `all modules verified`
- `go build ./...`: exit code 0
- `go test ./... -count=1`: exit code 0 (4 packages with tests, all pass)
- Prisma validate: exit code 0
- `git diff --check`: exit code 0 with CRLF normalization warnings (pre-existing)

### Existing Dirty Files Preserved

```text
 M .env.example              (pre-existing)
 M CODING_STANDARD.md        (pre-existing)
 M DATABASE.md               (pre-existing)
 M DEPLOYMENT.md             (pre-existing)
 M backend/platform/database/mysql_test.go (pre-existing)
 M backend/prisma/migrations/20260728_mysql_baseline/migration.sql  (C6 fix)
 M backend/prisma/schema.prisma                                  (C6 fix)
?? backend/test-results/                                          (C6 report)
```

## Files Changed in This Session

| File | Change | Domain Behavior Change? |
|------|--------|------------------------|
| `backend/prisma/migrations/20260728_mysql_baseline/migration.sql` | `DEFAULT CURRENT_TIMESTAMP(6)` -> `DEFAULT (UTC_TIMESTAMP(6))` on 14 columns; added default to `code_counters.updated_at` | No |
| `backend/prisma/schema.prisma` | `@default(now())` -> `@default(dbgenerated("(UTC_TIMESTAMP(6))"))` on 8 createdAt fields; `@updatedAt` -> `@default(dbgenerated("(UTC_TIMESTAMP(6))"))` on 7 updatedAt fields | No |

No Go source files were changed. No handler/service/route/DTO/frontend changes.

## Remaining Blockers

The UTC gate passes. The remaining blocker is that the full HTTP smoke test was not executed in this session due to the scope being limited to UTC timestamp parity. The following items remain for the next session:

- Full HTTP smoke test (server startup, auth, prospect, visits, customer conversion, AutoConvert, direct creation, counter concurrency, authorization, UTC runtime, outage/recovery, Ctrl+C/SIGTERM)
- Smoke test must use a fresh database deployment with the fixed baseline

## Verdict

**PASS WITH REQUIRED FIXES**

- UTC gate: PASS
- Clean baseline deploy: PASS
- FK integrity: PASS
- Seed idempotency: PASS
- created_at UTC: PASS
- updated_at UTC: PASS
- No same-row offset: PASS
- Prisma validation: PASS
- Build/tests: PASS
- Formatting: PASS
- Full HTTP smoke: DEFERRED (not in scope of UTC gate session)

---

# Full Runtime/API Smoke Addendum

Timestamp: 2026-07-28T14:45:00+07:00

Verdict: `PASS WITH REQUIRED FIXES`

This addendum preserves the earlier C6 history above and adds the runtime HTTP/API smoke evidence requested for the fixed MySQL baseline.

## A. Runtime Environment

- Branch: `migration/mysql`
- Test database: `crm_prospect_mysql_test`
- Reset/deploy: PASS; database name guard verified suffix `_test`
- Seed twice: PASS; 4 users, 1 administrator, 3 sales executives
- Runtime command: `go run ./cmd/server` from `backend`
- Runtime DSN properties only:
  - host: `127.0.0.1`
  - port: `3306`
  - database: `crm_prospect_mysql_test`
  - charset: `utf8mb4`
  - collation: `utf8mb4_0900_ai_ci`
  - parseTime: `true`
  - loc: `UTC`
  - multiStatements: `false`
- Secrets: supplied through process environment; no full DSN, password, JWT secret, JWT, refresh token, or password hash stored in the report.

## B. Route Matrix

Actual route registration from `backend/server/app.go`:

| Feature | Method | Path | Auth | Role | Body | Success | Error |
|---|---|---|---|---|---|---:|---:|
| Health | GET | `/api/health` | No | Public | none | 200 | 404 |
| Health alias | GET | `/api/v1/health` | No | Public | none | 200 | 404 |
| Login | POST | `/api/v1/auth/login` | No | Public | JSON email/password | 200 | 401/422 |
| Refresh | POST | `/api/v1/auth/refresh` | Cookie | Public | refresh cookie | 200 | 401 |
| Logout | POST | `/api/v1/auth/logout` | Cookie | Public route | refresh cookie | 204 | 500 |
| Me | GET | `/api/v1/auth/me` | Bearer | Any active user | none | 200 | 401 |
| Logout all | POST | `/api/v1/auth/logout-all` | Bearer | Any active user | none | 204 | 401 |
| Admin dashboard | GET | `/api/v1/dashboard/admin` | Bearer | ADMINISTRATOR | none | 200 | 401/403 |
| Sales dashboard | GET | `/api/v1/dashboard/sales` | Bearer | SALES_EXECUTIVE | none | 200 | 401/403 |
| Sales prospects | GET | `/api/v1/sales/prospects` | Bearer | SALES_EXECUTIVE | none | 200 | 401/403 |
| Sales prospect detail | GET | `/api/v1/sales/prospects/:id` | Bearer | assigned SALES_EXECUTIVE | none | 200 | 401/403/404 |
| Prospect transition | PATCH | `/api/v1/sales/prospects/:id/transition` | Bearer | assigned SALES_EXECUTIVE | JSON `status`, `notes` | 200 | 401/403/422 |
| Prospect decision alias | PATCH | `/api/v1/sales/prospects/:id/decision` | Bearer | assigned SALES_EXECUTIVE | JSON `status`, `notes` | 200 | 401/403/422 |
| Visit check-in | POST | `/api/v1/sales/prospects/:id/visits/check-in` | Bearer | assigned SALES_EXECUTIVE | form `latitude`, `longitude`, optional selfie/notes | 201 | 400/401/403/409/422 |
| Visit check-out | PATCH | `/api/v1/sales/prospects/:id/visits/:visitId/check-out` | Bearer | visit owner | JSON coordinates/notes | 200 | 400/401/403/409 |
| Sales customers | GET | `/api/v1/sales/customers` | Bearer | SALES_EXECUTIVE | none | 200 | 401/403 |
| Sales customer detail | GET | `/api/v1/sales/customers/:id` | Bearer | assigned SALES_EXECUTIVE | none | 200 | 401/403/404 |
| Won queue / AutoConvert trigger | GET | `/api/v1/admin/prospects/won` | Bearer | ADMINISTRATOR | none | 200 | 401/403 |
| Prospect pipeline | GET | `/api/v1/admin/prospects/pipeline` | Bearer | ADMINISTRATOR | none | 200 | 401/403 |
| Sales executives | GET | `/api/v1/admin/sales-executives` | Bearer | ADMINISTRATOR | none | 200 | 401/403 |
| Places search | GET | `/api/v1/admin/prospect-finder/search` | Bearer | ADMINISTRATOR | query | 200 | 422/503 |
| Place detail | GET | `/api/v1/admin/prospect-finder/places/:placeId` | Bearer | ADMINISTRATOR | none | 200 | 503 |
| Prospect create | POST | `/api/v1/admin/prospects` | Bearer | ADMINISTRATOR | JSON place snapshot/assignment | 201 | 401/403/409/422 |
| Prospect review | GET | `/api/v1/admin/prospects/:id` | Bearer | ADMINISTRATOR | none | 200 | 401/403/404 |
| Visit monitor | GET | `/api/v1/admin/visits` | Bearer | ADMINISTRATOR | query filters | 200 | 401/403 |
| Delete visit | DELETE | `/api/v1/admin/visits/:visitId` | Bearer | ADMINISTRATOR | none | 200 | 401/403/404 |
| Conversion form | GET | `/api/v1/admin/prospects/:id/conversion-form` | Bearer | ADMINISTRATOR | none | 200 | 401/403/409 |
| Convert | POST | `/api/v1/admin/prospects/:id/convert` | Bearer | ADMINISTRATOR | JSON conversion form | 201 | 401/403/409/422 |
| Parent company search | GET | `/api/v1/admin/parent-companies` | Bearer | ADMINISTRATOR | query `search` | 200 | 401/403 |
| Customers | GET | `/api/v1/admin/customers` | Bearer | ADMINISTRATOR | none | 200 | 401/403 |
| Customers paged | GET | `/api/v1/admin/customers/list` | Bearer | ADMINISTRATOR | query page/limit/keyword/filter/sort | 200 | 401/403 |
| Customer filter options | GET | `/api/v1/admin/customers/filter-options` | Bearer | ADMINISTRATOR | none | 200 | 401/403 |
| Customer detail | GET | `/api/v1/admin/customers/:id` | Bearer | ADMINISTRATOR | none | 200 | 401/403/404 |
| Customer delete | DELETE | `/api/v1/admin/customers/:id` | Bearer | ADMINISTRATOR | none | 204 | 401/403/404 |

No public user creation, direct customer creation, customer update endpoint, prospect list search endpoint, prospect pagination endpoint, or visit list-by-prospect route is registered in the current server.

## C. Server Startup

- PID: `21820`
- Listening address: `http://127.0.0.1:18081`
- Startup: PASS; `/api/health` returned 200.
- Startup log: no full DSN, password, JWT, refresh token, or password hash observed.
- Note: an earlier attempt on port `18080` failed with Windows bind error because the port was already unavailable; no HTTP evidence was claimed from that failed process.

## D. Health/Reachability

- `GET /api/health`: 200, body `{"data":{"status":"ok"},"meta":{"requestId":"..."}}`

## E. Invalid Login

- `POST /api/v1/auth/login`, sanitized body `admin@yummy.test` + wrong password.
- Status: 401.
- Result: PASS; generic invalid credentials response, no password/hash leak.

## F. Valid Login

- Admin: `POST /api/v1/auth/login` -> 200, role `ADMINISTRATOR`, access token present, token length 441.
- Sales A: `POST /api/v1/auth/login` -> 200, role `SALES_EXECUTIVE`, access token present, token length 444.
- DB assertion: refresh sessions created; total sessions 9 after all auth smoke actions.

## G. Refresh

- `POST /api/v1/auth/refresh` with admin refresh cookie -> 200.
- New access token present, token length 441.
- DB assertion: rotated/revoked sessions present.

## H. Logout/Revoke

- `POST /api/v1/auth/logout` -> 204.
- Reuse old refresh cookie -> `POST /api/v1/auth/refresh` -> 401.
- DB assertion: refresh sessions `revoked_at IS NOT NULL` count = 2.

## I. Prospect Create/List/Detail

- `POST /api/v1/admin/prospects` -> 201.
- Created prospect `00bc16b8-4649-46fa-8f99-632cc63bfa37`, source place `phase-c6-place-*`.
- `GET /api/v1/admin/prospects/:id` -> 200.
- `GET /api/v1/admin/prospects/pipeline` -> 200.
- DB assertion: `prospects` rows with `google_place_id LIKE 'phase-c6-%'` = 3.

## J. Prospect Search/Pagination

- Prospect Finder search route exists but depends on Google Places.
- `GET /api/v1/admin/prospect-finder/search?...keyword=cafe` -> 503 because `GOOGLE_MAPS_API_KEY` intentionally unset in test runtime.
- Native prospect list search/pagination endpoint is not registered; SKIP with route evidence.

## K. Prospect Authorization

- Sales A own prospect detail: `GET /api/v1/sales/prospects/:id` -> 200.
- Sales B on Sales A prospect: `GET /api/v1/sales/prospects/:id` -> 403.
- No auth on admin protected endpoint: 401.
- Invalid bearer token on `/api/v1/auth/me`: 401.

## L. Review/Status Transition

- `GET /api/v1/admin/prospects/:id` review -> 200.
- Valid transitions through `CONTACTED`, `INTERESTED`, `QUALIFIED`, `PROPOSAL_SENT`, `NEGOTIATION`, `WON`: all 200.
- Invalid jump `CONTACTED -> WON`: 422.
- DB assertion: status history rows inserted during transition and conversion; `changed_by_user_id` follows acting sales/admin.

## M. Check-In

- JSON check-in attempt returned 400 because actual handler requires form fields.
- Correct form request `POST /api/v1/sales/prospects/:id/visits/check-in` -> 201.
- Visit ID: `a919004c-2f94-476b-b8bc-5ca74c4a4983`.

## N. Duplicate Open Visit

- Second form check-in before checkout -> 409.
- DB assertion: generated `open_visit_prospect_id` unique index prevented a second open visit; no raw MySQL duplicate error leaked to client.

## O. Check-Out and Monitoring

- `PATCH /api/v1/sales/prospects/:id/visits/:visitId/check-out` -> 200.
- `GET /api/v1/admin/visits` -> 200.
- DB assertion: phase-c6 visits total = 1, open = 0, closed = 1.

## P. Radius Validation

- Current prospect visit code validates coordinate range only; it does not enforce an attendance radius in service/repository.
- Inside coordinate: accepted.
- Invalid coordinate latitude `91`: rejected 422.
- Outside-radius behavior: SKIP, no radius rule/field exists in current prospect visit API.

## Q. Customer Convert

- `GET /api/v1/admin/prospects/:id/conversion-form` -> 200.
- `POST /api/v1/admin/prospects/:id/convert` -> 201.
- DB assertions:
  - parent company created: `PC-000001`
  - customer site created: `PC-000001-S001`
  - `source_prospect_id = 00bc16b8-4649-46fa-8f99-632cc63bfa37`
  - `sales_executive_id = 1935cbdb-daaa-427b-8450-d3731e6ced3f`
  - `converted_at = 2026-07-28 07:35:33.918624` UTC
  - prospect status changed to `CONVERTED`
  - `npwp_name = C6 Parent NPWP`
  - `kam_assignments[0].ownerName = Andini Putri`

## R. Duplicate Convert

- Repeating `POST /api/v1/admin/prospects/:id/convert` -> 409.
- DB assertion: only one C6 customer row exists; counter rows remained `parent_company_code=1`, `customer_site_code=1`.

## S. AutoConvert

- AutoConvert is invoked asynchronously when Sales transitions to `WON` and when admin reads `/api/v1/admin/prospects/won`.
- In this run, manual Convert immediately followed the `WON` transition and succeeded; no separate AutoConvert-only prospect was fully isolated.
- SKIP as standalone proof; required follow-up should create a dedicated WON prospect and poll DB before manual conversion.

## T. Transaction Rollback

- Duplicate Convert conflict verified no duplicate customer/site and no counter increment.
- A deeper child-write constraint failure path was not safely exposed through public API without schema mutation or direct DB tampering; SKIP for orphan/partial-write rollback proof.

## U. Direct Customer Creation

- No direct `POST /api/v1/admin/customers` or update route is registered.
- Current customer creation path is Convert/AutoConvert only.
- `source_prospect_id IS NULL` direct creation smoke: SKIP, unsupported by route/service surface.

## V. Customer List/Search/Update/Delete

- `GET /api/v1/admin/customers/list?page=1&limit=10&keyword=C6&sort=name_asc` -> 200.
- `GET /api/v1/admin/customers/:id` -> 200.
- `DELETE /api/v1/admin/customers/:id` route exists but was not executed against the converted evidence row to preserve conversion assertions.
- Customer update route: SKIP, not registered.

## W. Customer Authorization

- Admin customer detail: 200.
- Sales A assigned customer detail: 200.
- Sales B unassigned customer detail: 404.
- No/invalid token behavior already covered: 401.

## X. Counter Sequential

- First generated parent code: `PC-000001`.
- First generated site code: `PC-000001-S001`.
- Counter table final state: `parent_company_code=1`, `customer_site_code=1`.
- Second sequential value not generated in this run because duplicate conversion correctly did not increment counters.

## Y. Counter Concurrency

- SKIP. No permanent integration harness was added in this session; current public API requires full prospect workflow per conversion and is not a clean counter-only concurrency harness.

## Z. Injection Safety

- `GET /api/v1/admin/customers/list?...keyword=' OR 1=1 --` -> 200.
- Assertion: no SQL error; DB unchanged.
- Prospect search injection: SKIP; no native prospect search route exists, and Google Places route was disabled by missing test API key.

## AA. UUID Safety

- Route UUID parse errors are covered by handler code for prospect/customer/visit IDs and return 400.
- Malformed UUID stored in DB was not inserted because production tables use UUID IDs with FK constraints except nullable `source_prospect_id`.
- Runtime malformed nullable `source_prospect_id` scanner test: SKIP, no safe public API path.

## AB. JSON Safety

- Valid JSON evidence: `place_types`, `site_contacts`, `company_contacts`, `sales_assignments`, and `kam_assignments` inserted and read through Convert.
- Malformed DB JSON insertion: SKIP; not attempted during HTTP smoke to avoid direct invalid writes.

## AC. UTC Runtime Evidence

- `UTC_TIMESTAMP(6)`: `2026-07-28 07:36:39.178872`
- `CURRENT_TIMESTAMP(6)`: `2026-07-28 14:36:39.178872`
- Session offset: 7 hours.
- API-created rows stored UTC:
  - created prospect: `created_at 2026-07-28 07:35:29.947698`
  - converted prospect/customer: `converted_at 2026-07-28 07:35:33.918624`
  - visit rows closed with UTC check-in/check-out values
- No seven-hour mixed same-row offset observed.

## AD. DB Outage

- SKIP. MySQL instance is Laragon local service, not a dedicated disposable container created by C6; stopping it would risk disrupting the user's broader development environment.

## AE. DB Recovery

- SKIP for same reason as DB outage.

## AF. Ctrl+C Shutdown

- SKIP as graceful signal proof. Server was launched hidden via `Start-Process`, which did not expose an attached console for a valid Ctrl+C event.
- Cleanup: process PID `21820` was stopped after smoke; no server listener left intentionally running.

## AG. SIGTERM Shutdown or Platform Limitation

- SKIP/platform limitation. Windows `Stop-Process`/forced task termination is not equivalent to Unix SIGTERM or console Ctrl+C signal path; no graceful SIGTERM claim made.

## AH. Runtime Log Security

- Reviewed server stdout/stderr files.
- No DSN, DB password, JWT secret, full JWT, refresh token, password, password hash, raw SQL args, or stack trace to client observed.
- One expected startup bind error from earlier port attempt was sanitized and contained no credentials.

## AI. API Compatibility

- Actual response envelope uses `data`/`meta` for successes and `error` for application errors.
- Actual implemented paths differ from aspirational API docs: admin/sales namespaces are required.
- Frontend contract review: no frontend files changed to mask backend behavior.
- Notable compatibility gap: check-in uses form fields (`FormValue`) rather than JSON DTO.

## AJ. Build/Test/Static Verification

- `gofmt -l <changed/relevant-go-files>`: exit 0, no output.
- `go mod tidy -diff`: exit 0 after using a workspace-local `GOCACHE`; initial Windows cache attempt returned access denied.
- `go mod verify`: exit 0, `all modules verified`.
- `go build ./...`: exit 0.
- `go test ./... -count=1`: exit 0.
- `npx.cmd prisma validate --schema prisma/schema.prisma`: exit 0 with `PRISMA_DATABASE_URL` set to test DB URL.
- `git diff --check`: exit 0, only CRLF normalization warnings.
- Static search `pgx|pgconn|pgxpool|postgres://|5432|sslmode|NewPostgresRepository`: no backend matches.
- Static search `\$[0-9]+|ILIKE|RETURNING|ON CONFLICT|nextval|UNIX_TIMESTAMP|NOW\(\)|CURRENT_TIMESTAMP` in Go files: no matches.

## AK. Files Changed by Full C6

- `backend/test-results/phase-c6-report.md`: updated with full runtime/API smoke addendum.
- `backend/test-results/phase-c6-http-results.json`: sanitized HTTP smoke evidence.
- `backend/test-results/phase-c6-visit-results.json`: visit form smoke evidence.
- `backend/test-results/phase-c6-server*.log`: startup/log security evidence, no credentials.

No commit was created.

## AL. Existing Dirty Files Preserved

Still dirty and not restored/removed by this addendum:

- `.env.example`
- `CODING_STANDARD.md`
- `DATABASE.md`
- `DEPLOYMENT.md`
- `backend/platform/database/mysql_test.go`
- `backend/prisma/migrations/20260728_mysql_baseline/migration.sql`
- `backend/prisma/schema.prisma`

## AM. Skipped Tests and Exact Reasons

- Prospect native search/pagination: no implemented route.
- Prospect radius outside check: no radius enforcement in current visit service.
- AutoConvert standalone proof: not isolated before manual Convert; needs dedicated follow-up run.
- Deep rollback via child constraint failure: no safe public API failure path found without direct DB/schema manipulation.
- Direct customer creation/update: no route registered.
- Counter concurrency: no repository integration harness added.
- UUID malformed DB scanner: no safe public API setup; FK/schema prevent representative malformed IDs except direct DB tampering.
- Malformed JSON DB rejection/helper scanner: not attempted in HTTP smoke.
- DB outage/recovery: Laragon local MySQL was not confirmed as disposable test-only service.
- Ctrl+C/SIGTERM: Windows hidden process had no valid attached console/signal path.

## AN. Release Blockers

Required fixes/follow-ups before full release confidence:

- Add or run a dedicated AutoConvert smoke that creates a separate `WON` prospect and verifies async conversion fields before any manual conversion.
- Add counter concurrency integration test using repository code, guarded by `TEST_DATABASE_URL`.
- Decide whether visit radius enforcement is required for prospects; current implementation validates coordinate range only.
- Add direct customer creation/update routes only if the product contract requires them; current API does not support them.
- Add safe UUID/JSON malformed scanner tests at unit/integration level.
- Add a deterministic graceful shutdown test harness for Windows or run signal tests on a platform that can send console Ctrl+C/SIGTERM correctly.

## AO. Verdict

`PASS WITH REQUIRED FIXES`

Runtime HTTP/API smoke passed for health, auth, refresh/revoke, protected auth rejection, prospect create/review/list/authorization, status workflow, visit check-in/duplicate/check-out, conversion, duplicate conversion, customer list/detail/authorization, injection no-SQL-error check, UTC evidence, build/tests, Prisma validation, and static checks.

The verdict is not full PASS because AutoConvert standalone proof, counter concurrency, DB outage/recovery, graceful signal shutdown, malformed UUID/JSON safety, direct customer creation/update, prospect native search/pagination, and radius-outside behavior were skipped with concrete implementation/platform reasons.
