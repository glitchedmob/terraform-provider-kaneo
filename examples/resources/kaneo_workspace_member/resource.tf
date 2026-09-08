resource "kaneo_workspace" "engineering" {
  name = "Engineering"
  slug = "engineering"
}

resource "kaneo_workspace_role" "triage" {
  workspace_id = kaneo_workspace.engineering.id
  name         = "triage"
  permissions = {
    task = ["read", "update"]
  }
}

# Create returns pending. The recipient accepts independently in Kaneo.
resource "kaneo_workspace_member" "teammate" {
  workspace_id = kaneo_workspace.engineering.id
  email        = "teammate@example.com"
  role         = kaneo_workspace_role.triage.name
}
