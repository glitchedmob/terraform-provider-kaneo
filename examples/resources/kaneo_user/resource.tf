# The provider must authenticate as an instance admin.
# This user receives no workspace membership.
terraform {
  required_version = ">= 1.11.0"
}

variable "user_password" {
  type      = string
  sensitive = true
  ephemeral = true
}

resource "kaneo_user" "alice" {
  email               = "alice@example.com"
  name                = "Alice"
  role                = "user"
  password_wo         = var.user_password
  password_wo_version = 1
}

# Supply TF_VAR_user_password through your runner's secret injection.
# Change password_wo_version to apply a new password.
# Omit both password arguments to create without password login.
# Removing both from an existing user does not remove their credentials.
