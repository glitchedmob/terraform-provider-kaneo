---
page_title: "Manage user passwords - Kaneo"
description: |-
  Set and rotate user passwords without persisting their source values in Terraform.
---

# Manage user passwords

User password management requires Terraform 1.11 or later. Write-only `password_wo` never enters plan, state, or private state, but its source can still persist. HCL literals can appear in saved plan configuration; ordinary sensitive variables and managed `random_password` resources can retain secrets. Use a sensitive ephemeral input:

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

Supply `TF_VAR_user_password` through secret injection, including when applying a saved plan. Change `password_wo_version` to set a new password. Changing only the ephemeral value produces no diff and is ignored even during unrelated updates.

For generated passwords, use `ephemeral "random_password"` with `hashicorp/random >= 3.7.0`, then assign `ephemeral.random_password.user.result` to `password_wo`. The value is discarded after the operation. If you need to retrieve it later, send it to an external secret store with write-only inputs in the same apply, not a persistent Terraform output or managed random resource.

Removing both password arguments stops management but leaves credentials intact. Password drift cannot be detected; refresh never fetches or changes passwords. Import leaves both arguments unset; adding both sets a password. These arguments affect only the [managed user](/providers/glitchedmob/kaneo/latest/docs/resources/user), not provider login authentication.
