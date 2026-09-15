# services

Each folder here is one **service**: a business area that owns its data. The processes it runs (api, worker) live inside it.

```
services/<name>/
  cmd/api/main.go       HTTP process
  cmd/worker/main.go    background jobs(or any helper pro)
  internal/             code shared by this service's processes
  migrations/           this service's schema
  openapi/              this service's API contract
  gen/                  generated code
```

## Why this layout

- **Processes share their service's code.** The api and the worker of one service use the same services, repos and database. Go's `internal/` rule only allows imports from inside the parent folder, so both binaries live under the same `services/<name>/`.
- **Services stay separate.** Go blocks `services/billing` from importing `services/api/internal/...`. Services talk through their API contracts or events, never through shared tables or Go structs.
- **Each service owns its data.** It has its own migrations, database URL, OpenAPI spec, generated code and config, so a new service with its own database is just a new folder.
- **Ready to split.** Everything a service needs is in its folder plus the domain-free `pkg/`. Moving one into its own repository means moving the folder and publishing `pkg/` as a module.
- **Simple for now.** One `go.mod` covers every service. Switch to a `go.mod` per service with a `go.work` file when a second service arrives or before a split.

## Adding a service

Copy the shape of `services/api`, give it its own database settings and migrations, and run it with `make run-api SERVICE=<name>`.
