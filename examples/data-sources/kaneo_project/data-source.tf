data "kaneo_project" "by_id" {
  id = "project-id"
}

data "kaneo_workspace" "engineering" {
  slug = "engineering"
}

data "kaneo_project" "by_slug" {
  workspace_id = data.kaneo_workspace.engineering.id
  slug         = "PLAT"
}
