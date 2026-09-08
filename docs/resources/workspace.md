---
page_title: "kaneo_workspace Resource - Kaneo"
description: |-
  Manages a Kaneo workspace.
---

# kaneo_workspace

Manages a Kaneo workspace. Destroying it deletes the workspace and its contents; review dependent projects and access before applying.

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

```shell
terraform import kaneo_workspace.engineering workspace-id
```

See the [import guide](/providers/glitchedmob/kaneo/latest/docs/guides/import) for matching configuration and declarative imports.
