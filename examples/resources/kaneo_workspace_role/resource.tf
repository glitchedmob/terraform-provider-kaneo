resource "kaneo_workspace" "engineering" {
  name = "Engineering"
  slug = "engineering"
}

resource "kaneo_workspace_role" "triage" {
  workspace_id = kaneo_workspace.engineering.id
  name         = "triage"
  permissions = {
    project = ["read"]
    task    = ["read", "update"]
    label   = ["read"]
  }
}
