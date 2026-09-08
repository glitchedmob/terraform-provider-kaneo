resource "kaneo_workspace" "engineering" {
  name = "Engineering"
  slug = "engineering"
}

resource "kaneo_project" "platform" {
  workspace_id = kaneo_workspace.engineering.id
  name         = "Platform"
  slug         = "PLAT"
}

# Adds a column after the four default columns.
resource "kaneo_column" "testing" {
  project_id = kaneo_project.platform.id
  name       = "Testing"
  icon       = "FlaskConical"
  color      = "#8b5cf6"
  is_final   = false
}
