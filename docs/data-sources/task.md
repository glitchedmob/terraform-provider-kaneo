---
page_title: "kaneo_task Data Source - Kaneo"
description: |-
  Retrieves a Kaneo task by ID.
---

# kaneo_task

Retrieves a task, including planned or archived tasks, without a project ID. Missing tasks and failed lookups are errors.

## Example usage

```terraform
data "kaneo_task" "deployment" {
  id = "existing-task-id"
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

Dates use UTC with fractional seconds when present. Column renames preserve slugs, so `status` can differ from the display name.
