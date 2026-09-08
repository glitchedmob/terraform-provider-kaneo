# Use an existing project's ID. Appends after its current columns.
resource "kaneo_column" "testing" {
  project_id = "existing-project-id"
  name       = "Testing"
  icon       = "FlaskConical"
  color      = "#8b5cf6"
  is_final   = false
}
