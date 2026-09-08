---
page_title: "kaneo_column Data Source - Kaneo"
description: |-
  Retrieves a Kaneo board column by project ID and column ID or slug.
---

# kaneo_column

Retrieves a project's board column, including automatically created default columns. The project ID is required for both lookup forms because Kaneo only lists columns by project.

## Example usage

```terraform
data "kaneo_column" "by_id" {
  project_id = "project-id"
  id         = "column-id"
}

# Kaneo creates this column automatically for every new project.
data "kaneo_column" "done" {
  project_id = "project-id"
  slug       = "done"
}
```

## Argument reference

- `project_id` (String, Required) Project identifier.
- `id` (String, Optional) Column identifier. Specify either `id` or `slug`.
- `slug` (String, Optional) Column slug. Specify either `id` or `slug`.

Lookup arguments must not be empty. Lookup fails if no column matches or the response is ambiguous. Slugs are stable even after a column is renamed. Default slugs are `to-do`, `in-progress`, `in-review`, and `done`.

## Attribute reference

The lookup also returns `id` or `slug` when not supplied.

- `name` (String) Column display name.
- `icon` (String) Icon name, or null if unset.
- `color` (String) Color value, or null if unset.
- `is_final` (Boolean) Whether the column marks tasks as done and stops their overdue reminders.
- `position` (Number) Absolute board position as an integer.
- `created_at` (String) Column creation timestamp.
- `updated_at` (String) Column last update timestamp.
