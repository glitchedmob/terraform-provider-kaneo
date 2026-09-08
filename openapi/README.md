# API specifications

- `kaneo.openapi.json` is the pinned upstream Kaneo specification.
- `kaneo.admin.openapi.yaml` contains the admin operations and models used by the provider, verified against Better Auth 1.6.25 in Kaneo 2.23.2. It describes the subset the provider uses, not every optional API field. Get/update responses are bare users, despite the published auth schema showing wrappers.
- `kaneo.overlay.yaml` corrects the upstream specification and includes the admin paths through external Path Item references. Their relative paths resolve beside `kaneo.openapi.json`.

Run `make generate` from the repository root. It uses the existing `oapi-codegen` tool twice:

1. `oapi-codegen.yaml` generates all client operations and operation-derived types in `internal/client/client.gen.go`.
2. `oapi-codegen.admin.yaml` generates the admin component models in `internal/client/admin.gen.go`.

Both outputs share the `client` package and one runtime API client. The second pass excludes operations tagged `admin` to avoid duplicate body/parameter types. `skip-prune` retains their component models.

When adding an admin endpoint, define it in the admin specification with the `admin` tag and add its Path Item reference to the overlay. Keep schema references local to the admin document. Regenerate and commit both generated files; the existing generated-client CI check detects drift.
