---
page_title: "Provider: Kaneo"
description: |-
  Configure the Kaneo Terraform provider.
---

# Kaneo Provider

The Kaneo provider uses the Kaneo HTTP API. It supports Kaneo Cloud and self-hosted instances.

## Example Usage

```terraform
terraform {
  required_providers {
    kaneo = {
      source = "glitchedmob/kaneo"
    }
  }
}

provider "kaneo" {
  endpoint = "https://cloud.kaneo.app/api"
}
```

Set credentials with environment variables rather than committing them to configuration:

```shell
export KANEO_USERNAME="terraform@example.com"
export KANEO_PASSWORD="your-password"
```

`username` is the Kaneo account's email address. Both credentials are required, either through provider attributes or the environment. Explicit attributes override their corresponding environment variables.

The provider signs in with email/password when configured and uses the returned session token for API requests. It does not create or use API keys. The account must already exist, have a password, and have permission to manage the requested resources. SSO-only accounts and interactive MFA are not supported.

On Kaneo releases where `DISABLE_LOGIN_FORM=true` also blocks backend password sign-in, including 2.23.2, password sign-in must be enabled. Use HTTPS outside local development. A new provider configuration signs in again; expired or revoked sessions are not automatically renewed during a run.

## Schema

### Optional

- `endpoint` (String) Kaneo API base URL. Defaults to `https://cloud.kaneo.app/api`. May also be set with `KANEO_API_URL`.
- `username` (String) Kaneo account email address for password sign-in. May also be set with `KANEO_USERNAME`.
- `password` (String, Sensitive) Kaneo account password. May also be set with `KANEO_PASSWORD`.
