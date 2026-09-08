resource "kaneo_workspace" "engineering" {
  name = "Engineering"
  slug = "engineering"
}

resource "kaneo_label" "bug" {
  workspace_id = kaneo_workspace.engineering.id
  name         = "Bug"
  color        = "#ef4444"
}
