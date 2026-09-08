---
page_title: "kaneo_task Resource - Kaneo"
description: |-
  Manages a Kaneo task.
---

# kaneo_task

Manages a task within a project. Deleting or replacing a task permanently deletes its contents, including comments and attachments. Changing `project_id` replaces the task rather than moving it, and gives it a new ID and project-local number.

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

resource "kaneo_column" "testing" {
  project_id = kaneo_project.platform.id
  name       = "Testing"
}

resource "kaneo_task" "deployment" {
  project_id  = kaneo_project.platform.id
  title       = "Verify deployment"
  description = "Run the deployment checks."
  status      = kaneo_column.testing.slug
  priority    = "high"
  start_date  = "2027-09-01T09:00:00Z"
  due_date    = "2027-09-02T17:00:00Z"
}
```

## Argument reference

- `project_id` (String, Required) Project identifier. Changing this replaces the task and deletes its previous contents.
- `title` (String, Required) Non-empty task title.
- `description` (String, Optional) Task description. Defaults to an empty string. Omit to clear it.
- `status` (String, Optional) Column slug within this project, or the virtual status `planned` or `archived`. Defaults to `to-do`.
- `priority` (String, Optional) One of `no-priority`, `low`, `medium`, `high`, or `urgent`. Defaults to `no-priority`.
- `assignee_id` (String, Optional) Assignable user ID in the project's workspace, not an email address. Omit to unassign. Empty or whitespace-padded IDs are not supported.
- `start_date` (String, Optional) Start timestamp in RFC3339 format. Omit to clear it.
- `due_date` (String, Optional) Due timestamp in RFC3339 format. Must not precede `start_date` when both are set. Omit to clear it.

Use the column's stable `slug` for `status`, not its ID or display name. Referencing `kaneo_column.example.slug` also tells Terraform to create the column first and delete the task before deleting the column. If the project's default To Do column no longer exists, set `status` explicitly.

Dates require a timezone and support at most three fractional second digits, such as `2027-09-01T09:00:00.123Z` or `2027-09-01T05:00:00.123-04:00`. Kaneo normalizes timestamps to UTC. The provider preserves the configured spelling when the API returns the same instant, but detects actual changes down to the millisecond. Date-only strings and higher precision are rejected before applying.

## Attribute reference

- `id` (String) Task identifier. This is different from the displayed identifier such as `PLAT-12`.
- `number` (Number) API-assigned per-project task number, such as `12` in `PLAT-12`. May be null for legacy tasks.
- `position` (Number) API-assigned order within the column. May be null for legacy tasks.
- `created_at` (String) Task creation timestamp.

Task numbering and ordering are not configurable. New tasks are appended to their selected column. Updates preserve the position from refreshed state, including when changing status; they do not reorder other tasks or prevent tied positions. A legacy task with a null position cannot be updated through Kaneo's full update endpoint without choosing a position, so the provider reports an error instead of inventing one. Set its position in Kaneo and refresh before retrying.

## Import

Import by task ID, not its displayed project-slug/number identifier:

```shell
terraform import kaneo_task.deployment task-id
```

Terraform 1.5 or later also supports declarative import:

```terraform
import {
  to = kaneo_task.deployment
  id = "task-id"
}
```

Configure the target resource to match the existing task before applying. In particular, omitting its description, assignee, or dates clears those values, and omitting status or priority applies the provider defaults. Import reads dates in UTC; the task data source can show their returned values.

## Drift and deletion

Terraform restores configured task fields changed outside Terraform and recreates deleted tasks. Because Kaneo can report a missing task as HTTP 400, the provider confirms absence against the complete project board, including planned and archived tasks. Authorization errors, malformed responses, or a failed project lookup produce diagnostics rather than removing the task from state. If the parent project was also deleted externally, that failed lookup requires separate reconciliation.

Comments, attachments, labels, and task relationships are not managed by this resource.
