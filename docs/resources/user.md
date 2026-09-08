---
page_title: "kaneo_user Resource - Kaneo"
description: |-
  Manages a Kaneo instance user without workspace membership.
---

# kaneo_user

Manages a Kaneo instance user. The provider must authenticate as an instance admin. Creating a user does not add them to any workspace. Do not manage the provider's own account with this resource; deleting that account is prohibited by Kaneo, and demoting it can prevent further administration.

## Example usage

```terraform
resource "kaneo_user" "alice" {
  email = "alice@example.com"
  name  = "Alice"
  role  = "user"
}
```

Omitting both password arguments creates a user without a credential account, suitable for social or OIDC login when the server permits account linking.

Password management requires Terraform 1.11 or later. Use a sensitive **ephemeral** input so the source value is also omitted from saved plans and state:

```terraform
terraform {
  required_version = ">= 1.11.0"
}

variable "user_password" {
  type      = string
  sensitive = true
  ephemeral = true
}

resource "kaneo_user" "password_login" {
  email               = "password-login@example.com"
  name                = "Password Login"
  password_wo         = var.user_password
  password_wo_version = 1
}
```

Supply the input through `TF_VAR_user_password` or your runner's secret injection. Provide it again when applying a saved plan. Change `password_wo_version` whenever you want to set a new password. A changed ephemeral value alone produces no diff and is ignored even during unrelated user updates.

For a generated value, use the Random provider's ephemeral resource, not its managed `random_password` resource:

```terraform
terraform {
  required_version = ">= 1.11.0"
  required_providers {
    random = {
      source  = "hashicorp/random"
      version = ">= 3.7.0"
    }
  }
}

ephemeral "random_password" "user" {
  length = 32
}

resource "kaneo_user" "generated_password" {
  email               = "generated@example.com"
  name                = "Generated Password"
  password_wo         = ephemeral.random_password.user.result
  password_wo_version = 1
}
```

The generated value is discarded after the operation. If it must be retrieved later, deliberately send it to an external secret store that supports write-only inputs in the same apply. Do not use a persistent Terraform output or managed `random_password` just to retain it.

## Argument reference

- `email` (String, Required) User email. Kaneo stores lowercase email. The provider preserves equivalent configured casing in state; import returns lowercase.
- `name` (String, Required) Display name.
- `role` (String, Optional) Instance role, `user` or `admin`. Defaults to `user`. This is not a workspace role.
- `email_verified` (Boolean, Optional) Defaults to `false`. Set `true` only for an email you have independently verified. This can enable OIDC account linking depending on the server's settings; the resource does not configure OIDC.
- `password_wo` (String, Optional, Sensitive, Write-only) Credential password, 8 to 128 bytes. Must be configured together with `password_wo_version`. The provider sets it on creation or when the version changes. Never stored in plan, state, or private state.
- `password_wo_version` (Number, Optional) Positive integer recording the last successfully applied password version. Stored in state. Change this value to apply `password_wo` again.

Removing both arguments stops password management without removing the actual credentials. Password drift cannot be detected because the API never returns passwords. Refresh never fetches or changes a password.

Import leaves both arguments unset. Adding both afterward sets a password. Write-only storage does not protect a non-ephemeral source: an HCL literal can appear in saved plan configuration, and ordinary sensitive variables or managed random resources can persist their values. Provider login authentication is unchanged; these arguments apply only to the managed user.

## Attribute reference

- `id` (String) User identifier.

## Import

```shell
terraform import kaneo_user.alice user-id
```

## Failure handling

The provider saves a newly created user's ID before setting its password. If password setup fails, Terraform retains the user for cleanup or recovery rather than leaving it untracked. Terraform may mark the failed creation as tainted and propose replacement. Review the plan before retrying. A failed password operation keeps the created ID and leaves the version unset. A failed rotation keeps the last successfully applied version so the next plan can retry it.

A missing user returns HTTP 404 and is removed from state. Permission failures, including HTTP 403, remain errors. Deletion removes the user and their sessions and accounts. Admin error diagnostics omit response bodies to avoid exposing credentials.
