# Replace with an existing workspace ID.
resource "kaneo_label" "bug" {
  workspace_id = "existing-workspace-id"
  name         = "Bug"
  color        = "#ef4444"
}
