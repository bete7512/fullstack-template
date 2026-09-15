---
name: verify-change
description: Use before reporting ANY code change as done in this repo, and always after adding or changing an endpoint, OpenAPI spec, migration, repo, interface, or error handling. Runs the quality gates in order, then a live curl + access-log check against a real Postgres.
---

# verify-change

Never claim done with a red or skipped-but-relevant gate.

## 1. Gates (in order; stop and fix at first red)

| # | When | Command | Green |
|---|---|---|---|
| 1 | spec under `services/api/openapi/` touched | `make lint-openapi && make gen-openapi` (`SERVICE=<name>` for another service) | exit 0; `services/api/gen/openapi/*` updated |
| 2 | an interface with `//go:generate` changed | `go generate ./services/api/internal/...` | exit 0; `mock_*.go` updated |
| 3 | always | `make fmt` | exit 0 (may rewrite files) |
| 4 | always | `go build ./... && go vet ./... && go vet -tags integration ./...` | no output |
| 5 | always | `make lint && golangci-lint run --build-tags integration ./...` | `0 issues.` twice |
| 6 | always | `make test` | all `ok`, no `FAIL` |
| 7 | repos, migrations, `pkg/db`, `pkg/testutil` touched | `make test-integration` | all `ok` |
| 8 | always | `make gen-check` | exit 0 |

- 7 needs Docker: testcontainers starts `postgres:16-alpine` unless `TEST_DATABASE_URL` is set.
- 8: `gen-check` hashes generated files under `services/`, runs `make gen`, and fails if anything changed.
  A failure means generated code was stale; it has been regenerated, so review the diff. Never stage or commit to make it pass.
- Migrations additionally: `make migrate-up && make migrate-down && make migrate-up` (see `add-migration`).
- Integration tests are unseen by `go build`/`make lint` without the tag — never skip 4/5's tagged half.

## 2. Live check

Run when an endpoint, middleware, error mapping, or migration changed. Edit `PORT` and the curls.

```bash
cd "$(git rev-parse --show-toplevel)"
set -a; source <(sed -E 's/[[:space:]]+#.*$//' .env); set +a   # strip inline comments
echo "db: $POSTGRES_USER@localhost:$POSTGRES_PORT/$POSTGRES_DB"  # read, don't assume scaffold/5433
# required at boot (also for migrate); an older .env may lack it, so fall back to the dev key in .env.example
export AUTH_JWT_SIGNING_KEY="${AUTH_JWT_SIGNING_KEY:-$(sed -nE 's/^AUTH_JWT_SIGNING_KEY=([^[:space:]]+).*/\1/p' .env.example)}"
SERVICE=api
make db-up && make migrate-up SERVICE=$SERVICE && make build SERVICE=$SERVICE
PORT=18080; LOG=$(mktemp)
HTTP_ADDR=":$PORT" LOG_FORMAT=json ./bin/$SERVICE/api >"$LOG" 2>&1 &
API_PID=$!
for i in $(seq 50); do curl -sf "localhost:$PORT/healthz" >/dev/null && break; sleep 0.2; done
B="localhost:$PORT"; H='-H Content-Type:application/json'
c() { curl -s -w ' [%{http_code}]\n' "$@"; }
field() { python3 -c 'import json,sys; print(json.load(sys.stdin)[sys.argv[1]])' "$1"; }
c "$B/healthz"
c "$B/readyz"
c -X POST $H -d '{"email":"live@example.com","name":"Live","password":"correct-horse-9"}' "$B/v1/users"   # 201 (public)
c -X POST $H -d '{"email":"live@example.com","name":"Live","password":"correct-horse-9"}' "$B/v1/users"   # 409
c -X POST $H -d '{"bogus":1}' "$B/v1/users"
c -X POST $H -d '{"email":"live@example.com","password":"wrong-password"}' "$B/v1/auth/login"            # 401
LOGIN=$(curl -s -X POST $H -d '{"email":"live@example.com","password":"correct-horse-9"}' "$B/v1/auth/login")
TOKEN=$(field accessToken <<<"$LOGIN"); REFRESH=$(field refreshToken <<<"$LOGIN")
c "$B/v1/me"                                                                     # 401: no token
c -H "Authorization: Bearer $TOKEN" "$B/v1/me"                                   # 200
c -H "Authorization: Bearer $TOKEN" "$B/v1/users/not-a-number"                   # 400: ids are int64
c -H "Authorization: Bearer $TOKEN" "$B/v1/users/999999999"                      # 404
c -H "Authorization: Bearer $TOKEN" "$B/v1/users?limit=2"                        # default order id asc
c -H "Authorization: Bearer $TOKEN" "$B/v1/users?sort_by=email&sort_dir=desc"    # 200
c -H "Authorization: Bearer $TOKEN" "$B/v1/users?sort_by=password&sort_dir=sideways"  # 422 fields sort_by, sort_dir
c -X POST $H -d "{\"refreshToken\":\"$REFRESH\"}" "$B/v1/auth/refresh"           # 200: rotated
c -X POST $H -d "{\"refreshToken\":\"$REFRESH\"}" "$B/v1/auth/refresh"           # 401: reuse revokes the session
kill -TERM "$API_PID"; wait "$API_PID"; echo "exit=$?"
python3 - "$LOG" <<'PY'
import json, sys
for line in open(sys.argv[1]):
    try: e = json.loads(line)
    except ValueError: print("NON-JSON:", line.rstrip()); continue
    if e.get("msg") != "http request": continue
    print(f'{e["level"]:5} {e["status"]} {e["method"]} route={e["route"]!r} path={e["path"]} error={e.get("error","")}')
PY
make db-down
```

- Port 5432 is often a host Postgres; compose maps `${POSTGRES_PORT}:5432`. If `make db-up` fails
  on bind or `make migrate-up` hits the wrong server, check `ss -ltnp | grep 543` and `.env`.
- Take real paths/bodies from `services/api/openapi/paths/*.yaml` (prefix in `services/api/openapi/openapi.yaml`);
  the curls above are examples.
- Protected ops (spec `security` is not `[]`) need `-H "Authorization: Bearer $TOKEN"` from the login above; without it expect `401 UNAUTHORIZED`.
- Boot fails with `AUTH_JWT_SIGNING_KEY must be base64 of 32 random bytes` if the key export was skipped.
- `exit=0` after SIGTERM = graceful shutdown worked.

## 3. What to assert

- **Status** matches the spec's responses for each case (201/200, 400, 404, 409, 422, 500).
- **Error body** is exactly `{"error","message","code","fields"?,"request_id"}`: `error` is the
  UPPER_SNAKE api code, `code` equals the HTTP status, `fields` only on validation, `request_id`
  equals the `X-Request-ID` response header.
- **500s leak nothing**: generic message only; no SQL, pgx, stack, or wrapped cause text in the body.
- **Access log**: exactly one `http request` line per curl; `route` is the pattern
  (`GET /v1/users/{id}`), never empty; `path` holds the concrete URL. `route="/"` means the
  request fell through to the catch-all 404 — the route is not registered (spec/gen/handler gap).
- **Levels**: `<500` → `INFO`; `>=500` → `ERROR` with `error` attr carrying the cause.
- No handler-level "request received" lines; no secrets/passwords/tokens in any log line.
- **Auth**: failed login is 401 `invalid email or password` whether or not the email exists; a reused refresh token is 401; a new protected route without a token is 401.
- Migration changes: `make migrate-status` shows the new version; affected endpoints return
  rows including the new columns.

## 4. Report

- List each gate run with pass/fail; name gates skipped and why (e.g. "7 skipped: no repo change").
- Paste only failing output (trimmed to the relevant lines), plus the live-check summary table.
- Red gate → say so and fix or hand back; never "done" with a failure outstanding.
