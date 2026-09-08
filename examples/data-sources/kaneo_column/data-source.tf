data "kaneo_column" "by_id" {
  project_id = "project-id"
  id         = "column-id"
}

# Kaneo creates this column automatically for every new project.
data "kaneo_column" "done" {
  project_id = "project-id"
  slug       = "done"
}
