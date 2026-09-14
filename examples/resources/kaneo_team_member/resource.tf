variable "workspace_id" {
  type = string
}

variable "operator_user_id" {
  type        = string
  description = "Authenticated provider user's native ID, already an accepted workspace member."
}

variable "accepted_user_id" {
  type        = string
  description = "Target's native user ID. Accept the workspace invitation before applying."
}

resource "kaneo_team" "engineering" {
  workspace_id = var.workspace_id
  name         = "Engineering"
}

resource "kaneo_team_member" "operator" {
  workspace_id = var.workspace_id
  team_id      = kaneo_team.engineering.id
  user_id      = var.operator_user_id
}

resource "kaneo_team_member" "engineer" {
  workspace_id = var.workspace_id
  team_id      = kaneo_team.engineering.id
  user_id      = var.accepted_user_id
  depends_on   = [kaneo_team_member.operator]
}
