# Replace with an existing workspace ID.
resource "kaneo_project" "platform" {
  workspace_id = "existing-workspace-id"
  name         = "Platform"
  slug         = "PLAT"
  icon         = "Code"
  description  = "Platform engineering tasks"
  is_public    = false
}
