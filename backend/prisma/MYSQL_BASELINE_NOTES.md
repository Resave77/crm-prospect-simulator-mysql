# MySQL Baseline Notes

This baseline targets MySQL 8.0.13+ and replaces the PostgreSQL migration chain for the `migration/mysql` branch. PostgreSQL migrations are archived under `backend/prisma/migrations/postgresql_archive/` for reference and rollback comparison; they must not be applied to MySQL.

## Minimum MySQL Version

- **Minimum supported:** MySQL 8.0.13+
- **Reason:** Expression defaults for `JSON` columns (`DEFAULT (JSON_ARRAY())`) and `TEXT` columns (`DEFAULT ('')`) require MySQL 8.0.13 or newer. Earlier 8.0.x releases do not support expression-based defaults.
- **Recommended:** MySQL 8.4 LTS when available, for long-term support and performance improvements.

## Type Mapping

- UUID values remain application-generated strings and are stored as `CHAR(36)`.
- PostgreSQL `TIMESTAMPTZ(6)` is mapped to MySQL `DATETIME(6)`. The application layer remains responsible for UTC semantics.
- PostgreSQL `JSONB` is mapped to MySQL `JSON`. Empty-array defaults are represented as `DEFAULT (JSON_ARRAY())`, which requires MySQL 8.0.13 or newer.
- PostgreSQL enums are mapped to Prisma/MySQL enum values without renaming or translating values.

## Collation

The baseline uses `utf8mb4` with `utf8mb4_0900_ai_ci` as the table default so name, address, and login email comparisons remain case-insensitive in the MySQL layer. Code columns that represent generated identifiers use `utf8mb4_0900_as_cs` so `parent_code`, `customer_code`, and `code_counters.name` stay case-sensitive and stable.

## One Open Visit

MySQL does not support PostgreSQL partial unique indexes. The baseline adds this generated column to `prospect_visits`:

```sql
`open_visit_prospect_id` CHAR(36) GENERATED ALWAYS AS (CASE WHEN `check_out_at` IS NULL THEN `prospect_id` ELSE NULL END) STORED
```

The unique index `prospect_visits_one_open_visit_idx` is created on that generated column. MySQL permits multiple `NULL` values in a unique index, so completed visits do not conflict while one open visit per prospect is still enforced. The column is intentionally not exposed in the Prisma model as a writable field.

## Code Counters

`code_counters` replaces PostgreSQL sequences for generated parent company and customer site codes. It starts empty in this schema baseline. A later data migration should initialize:

- `parent_company_code`
- `customer_site_code`

from the PostgreSQL sequence `last_value` or from audited production data. The intended runtime pattern is a single transaction that locks or atomically upserts the named counter row, increments `current_value`, and uses the returned value for code formatting. Do not use `SELECT MAX(...) + 1`.

## Direct Customer Creation

The effective PostgreSQL migration history made `customer_sites.source_prospect_id` and `customer_sites.source_google_place_id` nullable and removed their unique indexes. The MySQL baseline preserves nullable source columns and removes the foreign key constraint on `source_prospect_id`.

- `customer_sites.source_prospect_id` is a **nullable reference value** (`CHAR(36) NULL`).
- It has **no database foreign key** to `prospects.id`.
- A normal index is retained for query performance.
- This decision maintains parity with the PostgreSQL `direct_customer_creation` migration, which also dropped the FK to allow direct customer creation without requiring the source prospect row to exist.
- Runtime code must **not** assume referential integrity from the database for this column. Application-layer validation should handle the optional relationship.
