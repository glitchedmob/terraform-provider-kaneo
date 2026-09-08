resource "kaneo_workspace" "engineering" {
  name = "Engineering"
  slug = "engineering"
}

# Kaneo also seeds a default team. This creates an additional team.
resource "kaneo_team" "platform" {
  workspace_id = kaneo_workspace.engineering.id
  name         = "Platform"
}
