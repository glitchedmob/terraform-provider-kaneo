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

Omitting `password` creates a user without a credential account, suitable for social or OIDC login when the server permits account linking. For password login, supply `password` through a sensitive Terraform variable.

## Argument reference

- `email` (String, Required) User email. Kaneo stores lowercase email. The provider preserves equivalent configured casing in state; import returns lowercase.
- `name` (String, Required) Display name.
- `role` (String, Optional) Instance role, `user` or `admin`. Defaults to `user`. This is not a workspace role.
- `email_verified` (Boolean, Optional) Defaults to `false`. Set `true` only for an email you have independently verified. This can enable OIDC account linking depending on the server's settings; the resource does not configure OIDC.
- `password` (String, Optional, Sensitive) Credential password, 8 to 128 characters for the default Kaneo password policy. Setting or changing it uses the admin password endpoint. Removing it from configuration stops tracking the value but does not remove the remote password. Password drift cannot be detected because the API never returns passwords.

Sensitive passwords still reside in Terraform state. Protect the state backend and any saved plans. Import leaves `password` unset; adding it afterward sets a new password. Refresh never fetches or changes the password.

## Attribute reference

- `id` (String) User identifier.

## Import

```shell
terraform import kaneo_user.alice user-id
```

## Failure handling

The provider saves a newly created user's ID before setting its password. If password setup fails, Terraform retains the user for cleanup or recovery rather than leaving it untracked. Terraform may mark the failed creation as tainted and propose replacement. Review the plan before retrying.

A missing user returns HTTP 404 and is removed from state. Permission failures, including HTTP 403, remain errors. Deletion removes the user and their sessions and accounts. Admin error diagnostics omit response bodies to avoid exposing credentials.
