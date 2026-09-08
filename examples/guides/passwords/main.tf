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
