# Replace with an existing workspace ID.
resource "kaneo_workspace_role" "triage" {
  workspace_id = "existing-workspace-id"
  name         = "triage"
  permissions = {
    project = ["read"]
    task    = ["read", "update"]
    label   = ["read"]
  }
}
