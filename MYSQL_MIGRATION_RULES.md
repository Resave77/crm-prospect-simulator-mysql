# MySQL Migration Rules

## Repository Roles

* Repository PostgreSQL lama adalah baseline stabil dan rollback reference.
* Repository ini adalah repository khusus migrasi MySQL.
* Branch `main` menyimpan baseline yang telah disetujui.
* Semua implementasi migrasi dikerjakan pada branch `migration/mysql`.

## Primary Goal

Migrasikan runtime database PostgreSQL ke MySQL dengan strict behavior parity.

Aplikasi harus tetap memiliki:

* frontend yang sama;
* endpoint API yang sama;
* request dan response JSON yang sama;
* HTTP status dan error code yang sama;
* login, refresh, logout, dan authorization yang sama;
* UUID yang sama;
* role dan enum value yang sama;
* sorting, filtering, dan pagination yang sama;
* workflow prospect, visit, conversion, dan customer yang sama;
* data user dan data bisnis yang sama.

## Locked Technical Decisions

* MySQL minimal versi 8.0.
* Runtime menggunakan `database/sql`.
* Driver menggunakan `github.com/go-sql-driver/mysql`.
* Prisma hanya digunakan sebagai schema dan migration tooling.
* UUID MySQL menggunakan `CHAR(36)`.
* JSONB PostgreSQL menjadi MySQL `JSON`.
* TIMESTAMPTZ menjadi `DATETIME(6)`.
* Semua timestamp harus UTC.
* Password hash disalin tanpa re-hash.
* Repository runtime tetap menggunakan SQL manual.
* Generator kode menggunakan atomic counter table.
* Dilarang menggunakan `SELECT MAX(...) + 1`.
* Satu open visit per prospect harus ditegakkan di database.
* Migration PostgreSQL lama tidak dijalankan pada MySQL.
* MySQL menggunakan baseline migration baru.
* Frontend tidak boleh diubah kecuali diperlukan untuk mempertahankan API contract.
* Tidak boleh melakukan refactor besar bersamaan dengan migrasi.
* Tidak boleh menambah atau memperbaiki fitur di luar scope parity.

## Execution Roles

* OpenCode mengeksekusi perubahan file, build, test, migration, dan perbaikan.
* Codex melakukan audit, membuat checklist, menilai diff, dan memberikan corrective instruction.
* Setiap fase harus diaudit sebelum dilanjutkan.
* OpenCode tidak boleh membuat commit sebelum hasil perubahan diperiksa.
* Satu fase harus diselesaikan sebelum fase berikutnya dimulai.

## Prohibited Actions

* Jangan mengubah repository PostgreSQL lama untuk pekerjaan migrasi.
* Jangan force-push ke `main`.
* Jangan menghapus migration PostgreSQL sebelum parity selesai.
* Jangan menghapus data tanpa backup.
* Jangan menjalankan seed pada database berisi data penting.
* Jangan memasukkan credential atau secret ke Git.
* Jangan mengubah API contract.
* Jangan mengganti UUID menjadi integer.
* Jangan mengganti SQL manual menjadi ORM.
* Jangan menggabungkan migrasi database dengan redesign aplikasi.

## Required Phase Checks

Setiap fase harus menghasilkan:

* daftar file yang berubah;
* ringkasan implementasi;
* hasil `gofmt`;
* hasil build;
* hasil unit test;
* hasil integration test yang relevan;
* `git diff --stat`;
* daftar risiko atau blocker;
* audit Codex;
* commit terpisah setelah dinyatakan lulus.
