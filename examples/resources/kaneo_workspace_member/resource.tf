# Replace with an existing workspace ID.
resource "kaneo_workspace_role" "triage" {
  workspace_id = "existing-workspace-id"
  name         = "triage"
  permissions = {
    task = ["read", "update"]
  }
}

resource "kaneo_workspace_member" "teammate" {
  workspace_id = kaneo_workspace_role.triage.workspace_id
  email        = "teammate@example.com"
  role         = kaneo_workspace_role.triage.name
}
