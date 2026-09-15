# scaffold — AI working guide

## What this is

Go 1.26 + Postgres 16 API template, module `github.com/bete7512/scaffold`, one Go module.
Spec-first OpenAPI (oapi-codegen, std `net/http` ServeMux), hand-written pgx repos, goose Go migrations.
One service today, `services/api` (users CRUD, authentication, health): two binaries, `services/api/cmd/api` and `services/api/cmd/worker` (shell), share its `internal/`. Makefile is the only entrypoint.

## Commands

`make` loads `.env` if present (else defaults matching `.env.example`). Host Postgres port defaults to **5433**.
The api binary runs migrations itself: `api -m migrate <goose cmd>` (`up`, `down`, `status`, `up-to N`…).
`SERVICE ?= api` (and `SVC := services/$(SERVICE)`) selects the service for per-service targets: `make run-api`, `make migrate-up SERVICE=billing`.

| Target | Does |
|---|---|
| `setup` | install oapi-codegen, mockgen, air, golangci-lint, govulncheck; create `.env` |
| `db-up` / `db-down` / `db-reset` | compose `core` profile Postgres: start+wait / stop / wipe volume |
| `migrate-up` / `migrate-down` / `migrate-status` | `go run ./services/$(SERVICE)/cmd/api -m migrate up\|down\|status` |
| `run-api` / `dev` | `go run ./services/$(SERVICE)/cmd/api` on `:8080` / `db-up` then `run-api` |
| `run-worker` | `go run ./services/$(SERVICE)/cmd/worker` (waits for SIGTERM; job handlers Day 9) |
| `build` | `bin/$(SERVICE)/api`, `bin/$(SERVICE)/worker` (now `bin/api/api`, `bin/api/worker`) |
| `gen` | `gen-openapi` + `gen-proto` (stub) + `gen-mocks` |
| `gen-openapi` | bundle `services/$(SERVICE)/openapi` → `services/$(SERVICE)/gen/openapi/openapi.bundle.yaml`, then oapi-codegen |
| `gen-mocks` | `go generate ./...` |
| `gen-check` | hashes generated files under `services/`, runs `make gen`, fails if anything changed (no git needed) |
| `lint` / `lint-openapi` / `fmt` / `vet` | golangci-lint / kin-openapi validate (`SERVICE`) / gofmt+goimports / go vet |
| `test` | `go test -race ./...` (no Docker) |
| `test-integration` | same with `-tags integration` (testcontainers or `TEST_DATABASE_URL`) |
| `test-cover` / `tidy` / `vuln` | coverage html / go mod tidy / govulncheck |

## Layout

A service = a business area that owns its data; its processes live inside it.

```
services/api/                        the current service (users); future ones copy this shape (e.g. services/billing)
  cmd/api/main.go                    HTTP process: config → app.NewCore → handlers → server (or -m migrate)
  cmd/worker/main.go                 background process: config → app.NewCore → waits for SIGTERM (job handlers Day 9)
  internal/app/core.go               composition root shared by this service's binaries: pool, repos, hasher, tokens, services
  internal/config/                   service config: embeds pkg/config.Config, adds Auth (AUTH_ARGON2_*, AUTH_JWT_SIGNING_KEY, AUTH_ISSUER, AUTH_*_TOKEN_TTL)
  internal/handlers/                 HTTP only; imported only by cmd/api
  internal/{services,repos,models}/  shared by api and worker (+ repos/repo_mocks, services/service_mocks)
  migrations/                        goose Go migrations NNNN_name.go + migration.go registry; migrations.Up(ctx, pool)
  openapi/openapi.yaml               root: info, tags, security, paths: $ref ./paths/<file>.yaml#/<key>
  openapi/paths/<resource>.yaml      path items + that resource's schemas/params in a local components: block
  openapi/components/*.yaml          shared only: Error, error responses
  gen/openapi/                       generated: openapi.gen.go, openapi.bundle.yaml (committed, never edit)
pkg/apperr            sentinels + AppError
pkg/auth              PasswordHasher (argon2id), Tokens (EdDSA JWT), NewRefreshToken/HashRefreshToken, WithClaims/ClaimsFrom
pkg/config            common env → Config (App, HTTP, Log, DB), validated at boot
pkg/db                pool (+retries), DBTX, WithTx, HandleError
pkg/httpx             server, middleware, WriteJSON/WriteError/DecodeJSON, /docs, 404
pkg/logger            slog builder, request-id/context helpers
pkg/sorting           Columns: sort-key whitelist → OrderBy / Validate
pkg/testutil          Postgres(t, migrate) (integration tag; nil migrate = no migrations), Truncate, Main, Ptr, MustEnv
scripts/openapi-bundle/  shared tool, -in/-out per service
docs/adr/             decisions (0001–0003, 0016–0018)
go.mod                one module; per-service go.mod + go.work when a second service arrives
```

Import paths: `github.com/bete7512/scaffold/services/api/internal/<pkg>`, `.../services/api/migrations`, `.../services/api/gen/openapi`.

Planned, absent: `services/api/internal/jobs` (worker event handlers, Day 9), Dockerfile (Day 5, one file with `TARGET` build arg), `infra/`, `deploy/`, proto contracts, `apps/web`.

## Services and split-readiness

1. A service imports only its own `internal/` + `pkg/`; Go's `internal/` rule blocks cross-service imports.
2. Each service owns its migrations, database URL, OpenAPI spec, generated code and config.
3. Services talk only through contracts (HTTP/gRPC client or events); never shared tables or Go structs.
4. `pkg/` stays domain-free: `testutil.Postgres(t, migrate)` takes the service's migrate func; `pkg/config` holds only common settings, service settings live in `services/<name>/internal/config`.
5. New service = copy the folder shape. A new business area with its own data is a new `services/<name>/`, not a folder inside an existing service.

## Request flow

1. `httpx.Default`: `RequestID` → `Logger` (puts logger in ctx) → `Recover` → `MaxBody`.
2. Generated `openapi.HandlerWithOptions` binds path/query params; bind failure → `ErrorHandlerFunc` → 400 `BAD_REQUEST`.
3. Auth middleware (`handlers/authn.go`) enforces the op's spec `security`, puts claims in ctx (see Authentication).
4. Handler decodes body into generated DTO, maps to model, calls service.
5. Service validates, calls repo interface, classifies repo errors into `*apperr.AppError`.
6. Repo runs SQL via `db.DBTX`; driver errors → `db.HandleError` → wrapped sentinels.
7. Handler writes `httpx.WriteJSON` or `httpx.WriteError(w, r, err)`; `WriteError` records err for the access log line.

## Layer rules

| Layer | Owns | May import | Must not |
|---|---|---|---|
| `handlers` | implement `openapi.ServerInterface` with raw `(w, r, params)`; DTO⇄model mapping (`toUserDTO`) | `services/api/gen/openapi`, `models`, `services` (interfaces), `pkg/httpx`, `pkg/apperr`, `pkg/auth` (`ClaimsFrom`); `repos` **only** for `ListXOpts` | SQL, pgx, status mapping, logging, token parsing |
| `services` | validation (`apperr.NewValidation`), business rules, repo-error classification | `models`, `repos` (consume `repos.XRepo` directly), `pkg/apperr`, `pkg/auth` | HTTP, DTOs, SQL, logging (keep `Logger` in Deps, unused) |
| `repos` | hand-written SQL, `XSortColumns` whitelist | `models`, pgx, `pkg/db`, `pkg/apperr`, `pkg/sorting` | business rules, HTTP, logging |
| `models` | structs with `db:"col"` tags (also row types) | stdlib | everything else |
| `app` | `Core`: pool, repos, hasher, `auth.Tokens`, services | `pkg/*`, `repos`, `services` | `handlers` (api-only) |

- `services/api/internal/handlers` is imported only by `services/api/cmd/api`; `services/api/internal/app` never imports handlers.

Ids: `BIGINT GENERATED ALWAYS AS IDENTITY` in SQL, `int64` in Go at every layer, `type: integer, format: int64, minimum: 1` in the spec (ADR 0018).

Handler shape:
```go
var req openapi.CreateUserRequest
if err := httpx.DecodeJSON(r, &req); err != nil { httpx.WriteError(w, r, err); return }
user := &models.User{Email: req.Email, Name: req.Name}
if err := h.deps.Users.Register(r.Context(), user, req.Password); err != nil { httpx.WriteError(w, r, err); return }
_ = httpx.WriteJSON(w, http.StatusCreated, toUserDTO(user))
```

Repo rules (`services/api/internal/repos/<resource>.go`):
- Exported `XRepo` interface + `ListXOpts` + unexported struct over `db.DBTX`; `NewXRepo(conn db.DBTX) (XRepo, error)`.
- Explicit column-list consts (`userColumns`, `userListColumns`); never `SELECT *`.
- `pgx.NamedArgs` (`@name`); reads via `pgx.CollectOneRow` / `pgx.CollectRows` with `pgx.RowToStructByName[models.X]`.
- Every error returned through `db.HandleError(err)` (NoRows → `ErrNotFound`, 23505 → `ErrConflict`).
- `Exec` with `RowsAffected() == 0` → return `apperr.ErrNotFound`.
- Writes take `*models.X`, fill generated fields via `RETURNING` (Scan or CollectOneRow into the pointer), return only `error`.
- Never set `updated_at`; the trigger does.

## Constructors & wiring

- One `XDeps` struct; `NewX(deps XDeps) (X, error)`; single `if deps.A == nil || deps.B == nil` check returning `errors.New`.
- Services/repos return the interface; `handlers.New` returns `*Handler`. No DI framework.
- `services/api/internal/app/core.go` is the service's shared composition root: `app.NewCore(ctx, app.Deps{Config, Logger})` → `*app.Core{Pool, Users, Auth}`; `Close()` releases the pool.
- `NewCore` builds `userRepo` + `sessionRepo`, the argon2id hasher, `auth.NewTokens(seed, issuer, ttl)`, then `UserService` (Users, Sessions, Hasher) and `AuthService` (Users, Sessions, Hasher, Tokens, RefreshTokenTTL).
- Config: `services/api/internal/config` embeds `pkg/config.Config` (App, HTTP, Log, DB) and adds service settings (`Auth`: `AUTH_ARGON2_*`, `AUTH_JWT_SIGNING_KEY`, `AUTH_ISSUER`, `AUTH_ACCESS_TOKEN_TTL`, `AUTH_REFRESH_TOKEN_TTL`).
- `services/api/cmd/api/main.go`: config → logger → `app.NewCore` → `handlers.New(handlers.Deps{Users, Auth, DB, Logger})` → router → server.
- `services/api/cmd/worker/main.go`: config → logger → `app.NewCore` → job handlers (Day 9) → wait for SIGTERM.

Wiring a new resource `Project`:
1. `services/api/internal/app/core.go`: add `Projects services.ProjectService` to `Core`.
2. `core.go`: `projectRepo, err := repos.NewProjectRepo(pool)`, then `services.NewProjectService(services.ProjectServiceDeps{Projects: projectRepo, Logger: d.Logger})` → `Core.Projects`.
3. `services/api/internal/handlers/handler.go`: add `Projects services.ProjectService` to `Deps` and to the nil check in `New`.
4. `services/api/cmd/api/main.go`: pass `Projects: core.Projects` in `handlers.Deps{...}`; update `newFixture` in `handler_test.go`.
5. Worker needs it: read `core.Projects` in `services/api/cmd/worker/main.go`.

## Errors (`pkg/apperr`)

| Constructor | Status | `error` code |
|---|---|---|
| `NewNotFound(resource, err)` | 404 | `RESOURCE_NOT_FOUND` |
| `NewConflict(msg, err)` | 409 | `ALREADY_EXISTS` |
| `NewValidation(fields map[string]string)` | 422 | `VALIDATION_FAILED` (+ `fields`) |
| `NewBadRequest(msg, err)` | 400 | `BAD_REQUEST` |
| `NewUnauthorized(msg, err)` / `NewForbidden(msg, err)` | 401 / 403 | `UNAUTHORIZED` / `FORBIDDEN` |
| `NewUnavailable(msg, err)` | 503 | `SERVICE_UNAVAILABLE` |
| `NewInternal(err)` | 500 | `INTERNAL_ERROR` (message is generic) |
| `httpx.DecodeJSON` body over limit | 413 | `PAYLOAD_TOO_LARGE` |

Sentinels: `ErrNotFound, ErrConflict, ErrValidation, ErrBadRequest, ErrUnauthorized, ErrForbidden, ErrUnavailable, ErrInternal`.

- Repo: return `db.HandleError(err)` or a bare sentinel; never an `AppError`.
- Service: `errors.Is(err, apperr.ErrX)` → `apperr.NewX(...)`; anything else → `apperr.NewInternal(err)`.
- Handler: pass any error to `httpx.WriteError`; non-`AppError` becomes 500 without leaking text.

```go
func userError(err error) error {
	if errors.Is(err, apperr.ErrNotFound) {
		return apperr.NewNotFound("user", err)
	}
	return apperr.NewInternal(err)
}
```

Body (`httpx.ErrorResponse`, `application/json`): `{"error":"RESOURCE_NOT_FOUND","message":"user not found","code":404,"fields":{...},"request_id":"..."}`.

Logging: `httpx.Logger` writes one `http request` line per request (Info < 500, Error ≥ 500) with `method, route (r.Pattern), path, status, duration_ms, error`. Never log-and-return; return the error.

## Authentication

- `pkg/auth.Tokens`: Ed25519 (EdDSA) JWT access tokens with `sub`, `tv` (token_version), `jti`, `iat`, `exp`, `iss`; `Parse` accepts only EdDSA, errors wrap `auth.ErrInvalidToken`.
- Refresh tokens are opaque random strings (`auth.NewRefreshToken`); only their SHA-256 (`auth.HashRefreshToken`) is stored.
- Table (`migrations/0002_create_sessions.go`): `sessions`, one row per login with `refresh_token_hash`, `previous_token_hash`, `expires_at`, `revoked_at`; BIGINT identity id.
- `repos.SessionRepo`: `CreateSession`, `FindSessionByToken` (matches current or previous hash), `RotateRefreshToken` (one UPDATE that moves current to previous; stale or revoked → `ErrNotFound`), `RevokeSession`, `RevokeUserSessions`.
- Refresh with a token matching `previous_token_hash` counts as reuse and revokes the session. Tokens older than one rotation are unknown: rejected with 401, session untouched.
- `services.AuthService.Login`: one 401 message for unknown email, wrong password or no password; on an unknown email it still hashes the password (result discarded) so timing matches.
- `Refresh` rotates; reuse or a lost concurrent rotation revokes the session → 401; expired token or revoked session → 401. `Logout` is idempotent.
- `LogoutAll` and `UserService.ChangePassword` bump `users.token_version` and revoke all sessions.
- `Authenticate` parses the token and compares `tv` with the user's `token_version` (one DB read per request).
- Middleware `handlers/authn.go` runs in the generated router's `Middlewares`, reads each op's `security` from `openapi.GetSpec()`, keyed by `r.Pattern`.
- Modes: `security: []` anonymous; `bearerAuth`/`cookieAuth` required; a `- {}` alternative optional; route missing from the spec required; op omitting `security` inherits root, and with no root it is required (fail closed). A bad token is 401 even on optional routes.
- Token source: `Authorization: Bearer` or the `access_token` cookie. Claims go into ctx via `auth.WithClaims`.
- Routes: public `POST /v1/auth/login|refresh|logout`; authenticated `POST /v1/auth/logout-all`, `GET /v1/me`. Body `{accessToken, refreshToken, tokenType: "Bearer", expiresIn}`.
- Config: `AUTH_JWT_SIGNING_KEY` required, base64 of 32 bytes (`openssl rand -base64 32`); `AUTH_ISSUER` (scaffold), `AUTH_ACCESS_TOKEN_TTL` (15m), `AUTH_REFRESH_TOKEN_TTL` (720h).
- **Rule:** a protected route is declared by `security` in the spec. Never check tokens in handlers; read `auth.ClaimsFrom(r.Context())`, `!ok` → `apperr.NewUnauthorized` (copy `GetMe`).
- No authorization yet: any logged-in user can update or delete any user (Day 16).

## OpenAPI workflow

1. Edit `services/api/openapi/paths/<resource>.yaml`: path items as top-level keys (`users:`, `user:`); request/response schemas and resource params in the same file's `components:` block, ref'd locally (`'#/components/schemas/User'`, `'#/components/parameters/UserId'`).
   - `components/*.yaml` is shared only: the `Error` schema and error responses. Never add resource schemas or parameters there.
   - Reuse another resource's schema via `'./users.yaml#/components/schemas/User'` (see `me.yaml`).
   - Component names must be unique across all files: the bundler names each by its last pointer segment, so duplicates collide.
2. New path file/route → add under `paths:` in `services/api/openapi/openapi.yaml`.
3. Every error response `$ref`s `components/responses.yaml` (`BadRequest, ValidationFailed, Unauthorized, NotFound, Conflict, Unavailable, InternalError`).
4. Public ops set `security: []`; protected ops list `bearerAuth` and `cookieAuth`. Enforced by `handlers/authn.go`. An op that omits `security` requires login (fail closed), and `TestEveryOperationDeclaresSecurity` fails the build until it declares one.
5. `make gen-openapi` (optionally `make lint-openapi`; both take `SERVICE=`) regenerates `services/api/gen/openapi`; build fails until `Handler` implements the new method.
- Non-strict server (`strict-server: false`): handler owns decode/write.
- `format: email` generates `string`; email and length validation live in services.

## List endpoints

- Params: inline on each list operation in its own file (copy `listUsers`): `limit` (default 20, 1..100), `offset`, `search`, `sort_by` enum (default `id`), `sort_dir` (`asc|desc`, default asc).
- Handler builds `repos.ListXOpts{Limit: defaultPageSize}` (20) and overrides from non-nil params; enums copy as `string(*params.SortBy)`.
- Repo exports `var XSortColumns = sorting.Columns{"id": "id", "createdAt": "created_at", ...}` (API key → column).
- Service validates `Limit` 1..100, `Offset >= 0`, and `maps.Copy(fields, repos.XSortColumns.Validate(opts.SortBy, opts.SortDir))` (422), calls `ListX` then `CountX`, returns `(items, total, err)`.
- Repo: `XForList` model without sensitive fields, `xListColumns` + shared `xListFilter` const used by both List and Count, `ORDER BY ` + `XSortColumns.OrderBy(opts.SortBy, opts.SortDir)` + ` LIMIT @limit OFFSET @offset`.
- `OrderBy`: empty/unknown key → `id`, non-`desc` → `ASC`, appends `, id <DIR>` tie-breaker. Default order `id ASC`.
- Response schema `XList` `{items, total}` in `paths/<resource>.yaml` `components:`; index sort columns that large tables sort by.

## Migrations

- Each service owns its migrations and database: `services/api/migrations/`, exposed as `migrations.Up(ctx, pool)`.
- Copy the latest `services/api/migrations/NNNN_*.go` to the next number, e.g. `0003_add_foo.go` (4-digit prefix is load-bearing), rename the methods; add `s.registerMigration_NNNN_AddFoo()` to `registerMigrations()` in `services/api/migrations/migration.go`.
- Implement both `up_` and `down_`; check `make migrate-up && make migrate-down && make migrate-up` (add `SERVICE=<name>` for another service).
- New table: `id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY`; FK columns `BIGINT`.
- New table with `updated_at`: `CREATE TRIGGER update_<table>_updated_at BEFORE UPDATE ON <table> FOR EACH ROW EXECUTE PROCEDURE update_updated_at_column();` (function from 0001).
- Index on existing table: `goose.AddMigrationNoTxContext` with `*sql.DB` and `CREATE INDEX CONCURRENTLY IF NOT EXISTS` (template in the `add-migration` skill). Indexes on a table created in the same migration are plain `CREATE INDEX`.
- Expand/contract: add → deploy code → backfill → drop. Never rename or drop a column the running code reads.
- Update `models` `db` tags and repo column consts in the same change.
- Never run migrations at boot in prod; deploy runs `api -m migrate up` as a one-off task.

## Testing

| Target | Pattern |
|---|---|
| services | `services_test` package, testify `suite`, `repo_mocks.NewMockXRepo(gomock.NewController(s.T()))`, argon2 low params (`Memory: 8*1024, Iterations: 1`), assert `*apperr.AppError.Code`; table of cases where a method has several inputs |
| repos | `//go:build integration`, `TestMain` → `os.Exit(testutil.Main(m))` once per package, suite with `testutil.Postgres(s.T(), migrations.Up)` (`github.com/bete7512/scaffold/services/api/migrations`) in `SetupSuite`, `testutil.Truncate` in `SetupSubTest`, a `seed(...)` helper for fixtures; one table-driven method per repo method (example: `repos/users_test.go`); no `t.Parallel` |
| handlers | one `newFixture(t)` with `service_mocks.NewMockXService` + `handlers.New` + `handlers.NewRouter`; its `auth` mock accepts token `"good"` (→ `ada.ID`); `f.do(method, path, body)` sends `Authorization: Bearer good`, `f.send(req)` sends a custom request (no token, cookie); `TestAuthentication` covers missing, invalid, cookie, public; table test through the router asserting status and `httpx.ErrorResponse.Error` |
| pkg | plain unit tests (`pkg/httpx`, `pkg/apperr`, `pkg/config`, `pkg/logger`, `pkg/auth`, `pkg/sorting`) |

Shape every test the same way: one test per function or repo method, `tests := []struct{ name; inputs; want; wantErr error }`, `s.Run(tt.name, ...)` per case, then `seed → call → if tt.wantErr != nil { ErrorIs; return } → assert`. Fixtures come from a seeder helper, never copy-pasted setup. Not-found cases use `int64(-1)`; created ids are asserted with `s.Positive(x.ID)`; handler paths use `strconv.FormatInt(id, 10)`.

Mocks: put `//go:generate mockgen -source=<file>.go -destination=<name>_mocks/mock_<file>.go -package=<name>_mocks` under the package clause of each interface file; `make gen-mocks`; commit `mock_*.go`. Mocks live in a `<name>_mocks` subpackage (`repos/repo_mocks`, `services/service_mocks`), never in the production package.

## Style

- Comments minimal: one line per exported identifier, short package doc; no README/ADR refs in new code.
- No speculative code, options, or interfaces nothing uses.
- gofmt + goimports with local prefix `github.com/bete7512/scaffold` (`make fmt`).
- Makefile targets, hyphenated; no Taskfile.

## Definition of done

1. `make gen-openapi` if the spec changed.
2. `go generate ./services/api/internal/...` for touched interfaces (or `make gen-mocks`).
3. `make fmt`
4. `make lint`
5. `make test`
6. `make test-integration` if repos or migrations changed.
7. `make gen-check` (regenerates and fails if any generated file changed).
8. New endpoint: live curl check against `make dev` (see `verify-change` skill).

## Do not

- No ORM, sqlc, or SQL builders; no strict server.
- No `git add`, `git commit` or `git push` unless the user explicitly asks for it.
- No client input in `ORDER BY` or any SQL string; sort keys go through `sorting.Columns`, values through `pgx.NamedArgs`.
- No UUID primary keys; ids are bigint identity / `int64`.
- No routes outside the spec (only `/openapi.json`, `/docs`, and the `/` 404 are added in `router.go`).
- No logging in services or repos; no global loggers/pools/config.
- No `panic` in request paths.
- No hand edits under `services/*/gen/`.
- No imports across services (`services/a` → `services/b/...`), no shared tables between services.
- No migrations at app boot.
- No `x-` custom spec extensions for auth; auth reads `security`.
- No token parsing or `Authorization` header checks in handlers or services; the middleware owns it, handlers read `auth.ClaimsFrom`.
- No Taskfile.

## Skills

| Skill | Use when |
|---|---|
| `add-endpoint` | adding a single-resource operation (spec → handler → service → repo → tests) |
| `add-list-endpoint` | adding a paginated, searchable, sortable list (`ListXOpts`, `XForList`, `XSortColumns`, `{items,total}`) |
| `add-migration` | any schema change |
| `verify-change` | before declaring done: gates + live curl check |

## Planned, not built (do not invent)

- Auth, remaining: setting `access_token` cookies on login (with the frontend), rate limiting on `/v1/auth/*` (Day 10), email verification + forgot/reset password (Days 9–10), Google login (Day 11).
- Authz (Day 16): `Authorizer` interface, RBAC adapter + OpenFGA adapter.
- Worker job handlers (`services/api/internal/jobs`), `pkg/events`, outbox, mailer.
- Observability: OTel, metrics, Sentry/GlitchTip, dashboards, runbooks.
- Infra: Terraform, Ansible, Dockerfiles/ECS, gRPC (`gen-proto` is a stub).
