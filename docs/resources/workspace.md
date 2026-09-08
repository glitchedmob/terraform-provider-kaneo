---
page_title: "kaneo_workspace Resource - Kaneo"
description: |-
  Manages a Kaneo workspace.
---

# kaneo_workspace

Manages a Kaneo workspace.

## Example usage

```terraform
resource "kaneo_workspace" "engineering" {
  name        = "Engineering"
  slug        = "engineering"
  description = "Engineering projects"
}
```

## Argument reference

- `name` (String, Required) Workspace name.
- `slug` (String, Required) Workspace slug.
- `description` (String, Optional) Workspace description.
- `logo` (String, Optional) Workspace logo URL.

## Attribute reference

- `id` (String) Workspace identifier.
- `created_at` (String) Workspace creation timestamp.

## Import

Import a workspace with its ID:

```shell
terraform import kaneo_workspace.engineering workspace-id
```
