resource "kaneo_workspace" "engineering" {
  name = "Engineering"
  slug = "engineering"
}

resource "kaneo_project" "platform" {
  workspace_id = kaneo_workspace.engineering.id
  name         = "Platform"
  slug         = "PLAT"
}

resource "kaneo_task" "fix" {
  project_id = kaneo_project.platform.id
  title      = "Fix deployment failure"
}

resource "kaneo_label" "bug" {
  workspace_id = kaneo_workspace.engineering.id
  name         = "Bug"
  color        = "#ef4444"
}

resource "kaneo_task_label" "bug" {
  label_id = kaneo_label.bug.id
  task_id  = kaneo_task.fix.id
}
