---
page_title: "kaneo_project Resource - Kaneo"
description: |-
  Manages a Kaneo project.
---

# kaneo_project

Manages a Kaneo project within a workspace. Deleting a project permanently deletes its tasks and other contents.

## Example usage

```terraform
resource "kaneo_workspace" "engineering" {
  name = "Engineering"
  slug = "engineering"
}

resource "kaneo_project" "platform" {
  workspace_id = kaneo_workspace.engineering.id
  name         = "Platform"
  slug         = "PLAT"
  icon         = "Code"
  description  = "Platform engineering tasks"
  is_public    = false
}
```

## Argument reference

- `workspace_id` (String, Required) Workspace identifier. Changing this replaces the project rather than moving it. The old project's contents are not copied.
- `name` (String, Required) Project name. Must not be empty.
- `slug` (String, Required) Prefix used in task identifiers, for example `PLAT` in `PLAT-12`. Must not be empty.
- `icon` (String, Optional) Project icon name. Defaults to `Layout`.
- `description` (String, Optional) Project description. Defaults to an empty string. Removing this argument clears the description.
- `is_public` (Boolean, Optional) Whether the project board is readable without signing in. Defaults to `false`.

Kaneo's create endpoint does not accept description or visibility. The provider sends an update after creation when either differs from the API defaults. Null descriptions returned by Kaneo are represented as empty strings, and null visibility values as `false`.

## Attribute reference

- `id` (String) Project identifier.
- `created_at` (String) Project creation timestamp.
- `archived_at` (String) Project archive timestamp, or null if not archived. This resource reads archived projects but does not manage archiving or sidebar order.

## Import

Import a project with its ID:

```shell
terraform import kaneo_project.platform project-id
```

Import retrieves the workspace ID and project attributes from Kaneo. Set the arguments in your configuration to match the imported project before applying changes.
