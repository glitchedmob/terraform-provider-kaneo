# Terraform Provider for Kaneo

[![Tests](https://github.com/glitchedmob/terraform-provider-kaneo/actions/workflows/test.yml/badge.svg)](https://github.com/glitchedmob/terraform-provider-kaneo/actions/workflows/test.yml)

The Kaneo provider manages Kaneo resources through Terraform for both cloud-hosted and self-hosted installations.

## Requirements

- [Terraform](https://developer.hashicorp.com/terraform/downloads) 1.0 or later
- [Go](https://go.dev/doc/install) 1.27.1 or later

## Authentication

Set `KANEO_USERNAME` to an existing Kaneo account's email address and `KANEO_PASSWORD` to its password. The provider signs in and uses the returned Kaneo session, not an API key. See the [provider configuration](docs/index.md) for attributes and sign-in requirements.

## Resources and data sources

- [User resource](docs/resources/user.md), requires instance admin access and does not add workspace membership
- [Workspace resource](docs/resources/workspace.md) and [data source](docs/data-sources/workspace.md)
- [Workspace role resource](docs/resources/workspace_role.md), manages dynamic role names and permission sets with actual workspace authorization
- [Workspace member resource](docs/resources/workspace_member.md), invites without waiting for acceptance, then manages the accepted member's role and removal. Requires lowercase email; fails closed at Kaneo's 100 stored-invitation listing cap.
- [Project resource](docs/resources/project.md) and [data source](docs/data-sources/project.md)
- [Column resource](docs/resources/column.md) and [data source](docs/data-sources/column.md)
- [Task resource](docs/resources/task.md) and [data source](docs/data-sources/task.md)
- [Label resource](docs/resources/label.md) and [data source](docs/data-sources/label.md)
- [Task-label attachment resource](docs/resources/task_label.md)

## Development

```shell
git clone git@github.com:glitchedmob/terraform-provider-kaneo.git
cd terraform-provider-kaneo
make generate
make lint
make test
make build
```

`make lint` runs `go tool golangci-lint`, pinned in the root `go.mod`, just as CI does. No separate installer or global installation is needed.

The generated Go client and admin models in `internal/client/` are committed to the repository. Run `make generate` after updating the specifications or overlay in `openapi/`. See [API specifications](openapi/README.md) for the separately maintained admin endpoints and generation steps.

## License

This project is licensed under the [Mozilla Public License 2.0](LICENSE).
