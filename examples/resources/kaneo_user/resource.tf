# The provider must authenticate as an instance admin.
# This user receives no workspace membership.
resource "kaneo_user" "alice" {
  email = "alice@example.com"
  name  = "Alice"
  role  = "user"
}

# To enable password login, set password from a sensitive variable.
# Passwords are stored in Terraform state even when marked sensitive.
