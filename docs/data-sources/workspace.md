---
page_title: "kaneo_workspace Data Source - Kaneo"
description: |-
  Retrieves a Kaneo workspace by ID or slug.
---

# kaneo_workspace

Retrieves an existing Kaneo workspace. Specify exactly one of `id` or `slug`.

## Example usage

```terraform
data "kaneo_workspace" "engineering" {
  slug = "engineering"
}
```

For ID lookup, replace `slug` with `id = "existing-workspace-id"`.

## Argument reference

- `id` (String, Optional) Workspace identifier. Conflicts with `slug`.
- `slug` (String, Optional) Workspace slug. Conflicts with `id`.

## Attribute reference

- `id` (String) Workspace identifier.
- `name` (String) Workspace name.
- `slug` (String) Workspace slug.
- `description` (String) Workspace description.
- `logo` (String) Workspace logo URL.
- `created_at` (String) Workspace creation timestamp.
