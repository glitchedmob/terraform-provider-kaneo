---
page_title: "kaneo_column Resource - Kaneo"
description: |-
  Manages a Kaneo board column.
---

# kaneo_column

Manages a column within a project's board. Kaneo refuses to delete columns containing tasks. Move or delete those tasks before destroying or replacing a column.

## Example usage

```terraform
# Use an existing project's ID. Appends after its current columns.
resource "kaneo_column" "testing" {
  project_id = "existing-project-id"
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

`position` changes only this column; it neither shifts others nor prevents ties. Import all columns and assign distinct positions to arrange the board. Ordering can fail after a successful create or update; refresh and review the plan before retrying.

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

Import defaults rather than creating duplicates; conflicting creates fail instead of adopting them. Import requires `project-id/column-id`:

```shell
terraform import kaneo_column.todo project-id/column-id
```

Match existing values, especially `is_final = true` for Done. See the [declarative default-column import example](/providers/glitchedmob/kaneo/latest/docs/guides/import). When managing projects or tasks in Terraform, reference project IDs and column slugs to establish dependencies; tasks must be deleted before their column.
