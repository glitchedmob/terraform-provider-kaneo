# Terraform Provider for Kaneo

[![Tests](https://github.com/glitchedmob/terraform-provider-kaneo/actions/workflows/test.yml/badge.svg)](https://github.com/glitchedmob/terraform-provider-kaneo/actions/workflows/test.yml)

This repository contains the initial scaffold for the Kaneo Terraform provider. Provider functionality has not been implemented yet.

## Requirements

- [Terraform](https://developer.hashicorp.com/terraform/downloads) 1.0 or later
- [Go](https://go.dev/doc/install) 1.25 or later

## Development

```shell
git clone git@github.com:glitchedmob/terraform-provider-kaneo.git
cd terraform-provider-kaneo
make generate
make test
make build
```

The generated Go client in `internal/client/client.gen.go` is committed to the repository. Run `make generate` after updating `openapi/kaneo.openapi.json`.

## License

This project is licensed under the [Mozilla Public License 2.0](LICENSE).
