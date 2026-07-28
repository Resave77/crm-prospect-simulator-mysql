# Yummy Enterprise CRM Field Sales

Vue 3 frontend and JSON-only Go/Fiber API for the approved Enterprise CRM architecture.

## Local prerequisites

- Go 1.24+
- Node.js 22+
- MySQL 8.0.13+ (MySQL 8.4 LTS recommended)

## Initial setup

1. Copy `.env.example` to `.env`, set a MySQL `DATABASE_URL`, and replace the secrets.
2. Install database tooling with `npm.cmd --prefix backend install`.
3. Apply migrations with `npm.cmd --prefix backend run prisma:migrate:deploy`.
4. Seed the two approved users with `go run ./backend/cmd/seed`.
5. Run the API with `go run ./backend/cmd/server`.
6. Install and run the frontend with `npm.cmd --prefix frontend install` and `npm.cmd --prefix frontend run dev`.

Seed accounts:

- `admin@yummy.test` / `password123`
- `sales@yummy.test` / `password123`

Seed credentials are for controlled development and test environments only.

## Routing contract

- Vue owns `/`, `/login`, `/admin/*`, and `/sales/*`.
- Fiber owns `/api/*` and returns JSON only.
- Fiber does not contain templates, embedded HTML, or server-side rendering.
