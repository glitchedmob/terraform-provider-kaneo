resource "kaneo_workspace" "engineering" {
  name = "Engineering"
  slug = "engineering"
}

resource "kaneo_project" "platform" {
  workspace_id = kaneo_workspace.engineering.id
  name         = "Platform"
  slug         = "PLAT"
}

resource "kaneo_column" "testing" {
  project_id = kaneo_project.platform.id
  name       = "Testing"
}

resource "kaneo_task" "deployment" {
  project_id  = kaneo_project.platform.id
  title       = "Verify deployment"
  description = "Run the deployment checks."
  status      = kaneo_column.testing.slug
  priority    = "high"
  start_date  = "2027-09-01T09:00:00Z"
  due_date    = "2027-09-02T17:00:00Z"
}
