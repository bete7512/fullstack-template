# apps

Each folder here is one **app**: a business area that owns its data. The processes it runs (api, worker) live inside it.

```
apps/<name>/
  cmd/api/main.go       HTTP process
  cmd/api/Dockerfile    its image, built from the repo root
  cmd/worker/main.go    background jobs(or any helper pro)
  app/ config/ services/ repos/ models/ events/ jobs/   code shared by this app's processes
  migrations/           this app's schema
  openapi/              this app's API contract
  gen/                  generated code
```

## Why this layout

- **Processes share their app's code.** The api and the worker of one app use the same services, repos and database. Both binaries live under the same `apps/<name>/` so they import the same packages by short relative paths. There is no `internal/`: apps are leaves that nothing imports, so there is nothing to hide, and `pkg/` is kept domain-free by a `depguard` lint rule rather than by the compiler.
- **Apps stay separate.** Go blocks `apps/billing` from importing `apps/api/...`. Apps talk through their API contracts or events, never through shared tables or Go structs.
- **Each app owns its data.** It has its own migrations, database URL, OpenAPI spec, generated code and config, so a new app with its own database is just a new folder.
- **Ready to split.** Everything an app needs is in its folder plus the domain-free `pkg/`. Moving one into its own repository means moving the folder and publishing `pkg/` as a module.
- **Simple for now.** One `go.mod` covers every app. Switch to a `go.mod` per app with a `go.work` file when a second app arrives or before a split.

## Adding an app

Copy the shape of `apps/api`, give it its own database settings and migrations, and run it with `make run-api APP=<name>`.
