---
page_title: "kaneo_task Resource - Kaneo"
description: |-
  Manages a Kaneo task.
---

# kaneo_task

Manages a task within a project. Deleting or replacing a task permanently deletes its contents, including comments and attachments. Changing `project_id` replaces the task rather than moving it, and gives it a new ID and project-local number.

## Example usage

```terraform
# Both objects belong to this existing project.
resource "kaneo_column" "testing" {
  project_id = "existing-project-id"
  name       = "Testing"
}

resource "kaneo_task" "deployment" {
  project_id  = kaneo_column.testing.project_id
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

Dates require a timezone and at most three fractional digits, such as `2027-09-01T09:00:00.123Z` or `2027-09-01T05:00:00.123-04:00`. Date-only strings and higher precision are rejected. Kaneo normalizes to UTC; equivalent configured spelling is preserved, with drift detected to the millisecond.

## Attribute reference

- `id` (String) Task identifier. This is different from the displayed identifier such as `PLAT-12`.
- `number` (Number) API-assigned per-project task number, such as `12` in `PLAT-12`. May be null for legacy tasks.
- `position` (Number) API-assigned order within the column. May be null for legacy tasks.
- `created_at` (String) Task creation timestamp.

Numbering and ordering are read-only. New tasks append to their column; updates preserve refreshed position even when status changes, without reordering others or preventing ties. For legacy tasks with null position, set a position in Kaneo and refresh before retrying updates.

## Import

Import by task ID, not its displayed project-slug/number identifier:

```shell
terraform import kaneo_task.deployment task-id
```

Match the existing task before applying: omitted description, assignee, or dates clear those values; omitted status or priority applies defaults. Import reads dates in UTC, also shown by the task data source. See the [import guide](/providers/glitchedmob/kaneo/latest/docs/guides/import).

## Limitations and recovery

Terraform restores configured fields and recreates deleted tasks. Failed or unauthorized lookups retain state; reconcile an externally deleted project separately if its board is unreadable.

Comments, attachments, labels, and task relationships are not managed by this resource.
