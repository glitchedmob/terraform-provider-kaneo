data "kaneo_project" "by_id" {
  id = "existing-project-id"
}

data "kaneo_project" "by_slug" {
  workspace_id = "existing-workspace-id"
  slug         = "PLAT"
}
