---
page_title: "kaneo_task Data Source - Kaneo"
description: |-
  Retrieves a Kaneo task by ID.
---

# kaneo_task

Retrieves a task by its task ID, including planned and archived tasks. A project ID is not required. Missing tasks and lookup failures produce diagnostics.

## Example usage

```terraform
data "kaneo_task" "deployment" {
  id = "task-id"
}
```

## Argument reference

- `id` (String, Required) Non-empty task identifier, not a displayed identifier such as `PLAT-12`.

## Attribute reference

- `project_id` (String) Project identifier.
- `title` (String) Task title.
- `description` (String) Task description. Null API values are returned as an empty string.
- `status` (String) Column slug, or the virtual status `planned` or `archived`.
- `priority` (String) Task priority.
- `assignee_id` (String) Assignee user ID, or null if unassigned.
- `start_date` (String) Start timestamp in RFC3339 format, or null if unset.
- `due_date` (String) Due timestamp in RFC3339 format, or null if unset.
- `number` (Number) API-assigned per-project task number. May be null for legacy tasks.
- `position` (Number) Order within the column. May be null for legacy tasks.
- `created_at` (String) Task creation timestamp.

Dates are returned in UTC with fractional seconds when present. Slugs remain stable after column renames, so `status` may differ from the column's display name.
