---
page_title: "kaneo_project Data Source - Kaneo"
description: |-
  Retrieves a Kaneo project by ID or workspace ID and slug.
---

# kaneo_project

Retrieves a Kaneo project, including archived projects. Specify either `id` alone or `workspace_id` and `slug` together.

## Example usage

```terraform
data "kaneo_project" "by_id" {
  id = "existing-project-id"
}

data "kaneo_project" "by_slug" {
  workspace_id = "existing-workspace-id"
  slug         = "PLAT"
}
```

## Argument reference

- `id` (String, Optional) Project identifier. Conflicts with `workspace_id` and `slug`.
- `workspace_id` (String, Optional) Workspace identifier. Required with `slug`.
- `slug` (String, Optional) Project slug. Required with `workspace_id`.

Lookup arguments must not be empty. Missing or ambiguous matches fail. Kaneo allows duplicate slugs within a workspace; use an ID to disambiguate.

## Attribute reference

The lookup also returns `id`, `workspace_id`, and `slug` when they are not supplied.

- `name` (String) Project name.
- `icon` (String) Project icon name. Null API values become an empty string.
- `description` (String) Project description. Null API values become an empty string.
- `is_public` (Boolean) Whether the project board is readable without signing in. Null API values become `false`.
- `created_at` (String) Project creation timestamp.
- `archived_at` (String) Project archive timestamp, or null if not archived.
