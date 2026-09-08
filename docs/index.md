---
page_title: "Provider: Kaneo"
description: |-
  Configure the Kaneo Terraform provider.
---

# Kaneo provider

The Kaneo provider uses the Kaneo HTTP API. It supports Kaneo Cloud and self-hosted instances.

## Example usage

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

Both credentials are required; explicit attributes override their environment variables. The provider signs in with email/password and uses a session token, not API keys.

## Authentication limits

Use an existing password-enabled account with permission for the requested resources. SSO-only accounts and interactive MFA are unsupported. Enable password sign-in if `DISABLE_LOGIN_FORM=true` blocks it, as in Kaneo 2.23.2. Use HTTPS outside local development.

Expired or revoked sessions are not renewed during a run; a new provider configuration signs in again. Managed-user [password arguments](/providers/glitchedmob/kaneo/latest/docs/guides/passwords) do not change provider authentication.

## Schema

### Optional

- `endpoint` (String) Kaneo API base URL. Defaults to `https://cloud.kaneo.app/api`. May also be set with `KANEO_API_URL`.
- `username` (String) Kaneo account email address for password sign-in. May also be set with `KANEO_USERNAME`.
- `password` (String, Sensitive) Kaneo account password. May also be set with `KANEO_PASSWORD`.
