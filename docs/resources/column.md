---
page_title: "kaneo_column Resource - Kaneo"
description: |-
  Manages a Kaneo board column.
---

# kaneo_column

Manages a column within a project's board. Kaneo refuses to delete columns containing tasks. Move or delete those tasks before destroying or replacing a column.

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
}

# Adds a column after the four default columns.
resource "kaneo_column" "testing" {
  project_id = kaneo_project.platform.id
  name       = "Testing"
  icon       = "FlaskConical"
  color      = "#8b5cf6"
  is_final   = false
}
```

## Argument reference

- `project_id` (String, Required) Project identifier. Changing this replaces the column rather than moving it. Tasks are not moved.
- `name` (String, Required) Column display name. Kaneo derives the slug from this name at creation and requires at least one letter or number. Renaming a column does not change its slug.
- `icon` (String, Optional) Icon name. Omit to clear it. Empty strings are not supported.
- `color` (String, Optional) Color value, such as `#8b5cf6`. Omit to clear it. Empty strings are not supported.
- `is_final` (Boolean, Optional) Marks tasks in this column as done and stops their overdue reminders. Defaults to `false`.
- `position` (Number, Optional) Absolute board position, an integer from `0` to `2147483647`. When omitted, new columns are appended and existing columns retain their current position.

Setting `position` updates only this column. It does not shift other columns or prevent tied positions. To arrange the whole board, import the existing columns and assign distinct positions to each. The provider creates or updates the column before making a separate ordering request.

Kaneo reserves the slugs `planned` and `archived` for virtual task statuses and rejects duplicate slugs within a project.

## Attribute reference

- `id` (String) Column identifier.
- `slug` (String) Stable slug derived at creation. It cannot be configured or changed by renaming the column.
- `created_at` (String) Column creation timestamp.
- `updated_at` (String) Column last update timestamp.

## Import and default columns

Kaneo automatically creates four columns for a new project:

| Name | Slug | Position | Final |
|---|---|---|---|
| To Do | `to-do` | 0 | false |
| In Progress | `in-progress` | 1 | false |
| In Review | `in-review` | 2 | false |
| Done | `done` | 3 | true |

These columns are not automatically managed by the provider. Import them explicitly rather than declaring new resources with the same names. A conflicting create fails instead of adopting an existing column.

Import requires both IDs because Kaneo only lists columns within a project:

```shell
terraform import kaneo_column.todo project-id/column-id
```

For an existing project, Terraform 1.5 or later can look up a default column and import it declaratively:

```terraform
data "kaneo_column" "todo" {
  project_id = "existing-project-id"
  slug       = "to-do"
}

resource "kaneo_column" "todo" {
  project_id = data.kaneo_column.todo.project_id
  name       = "To Do"
  is_final   = false
}

import {
  to = kaneo_column.todo
  id = "${data.kaneo_column.todo.project_id}/${data.kaneo_column.todo.id}"
}
```

Create the project first so the import IDs are known during planning. Match the resource arguments to the existing column before applying changes. In particular, set `is_final = true` when importing the default Done column.
