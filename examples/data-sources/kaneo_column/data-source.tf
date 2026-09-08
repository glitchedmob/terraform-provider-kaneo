data "kaneo_column" "by_id" {
  project_id = "existing-project-id"
  id         = "existing-column-id"
}

# Kaneo creates this column automatically for every new project.
data "kaneo_column" "done" {
  project_id = "existing-project-id"
  slug       = "done"
}
