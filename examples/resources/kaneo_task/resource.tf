# Both objects belong to this existing project.
resource "kaneo_column" "testing" {
  project_id = "existing-project-id"
  name       = "Testing"
}

resource "kaneo_task" "deployment" {
  project_id  = kaneo_column.testing.project_id
  title       = "Verify deployment"
  description = "Run the deployment checks."
  status      = kaneo_column.testing.slug
  priority    = "high"
  start_date  = "2027-09-01T09:00:00Z"
  due_date    = "2027-09-02T17:00:00Z"
}
