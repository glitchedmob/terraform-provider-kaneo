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

Set the API key with the `KANEO_API_KEY` environment variable:

```shell
export KANEO_API_KEY="your-api-key"
```

## Schema

### Optional

- `endpoint` (String) Kaneo API base URL. Defaults to `https://cloud.kaneo.app/api`. May also be set with `KANEO_API_URL`.
- `api_key` (String, Sensitive) Kaneo API key. May also be set with `KANEO_API_KEY`.
