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
make test
make build
```

The generated Go client in `internal/client/client.gen.go` is committed to the repository. Run `make generate` after updating `openapi/kaneo.openapi.json` or `openapi/kaneo.overlay.yaml`. The overlay contains local schema additions and compatibility fixes.

## License

This project is licensed under the [Mozilla Public License 2.0](LICENSE).
