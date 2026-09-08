---
page_title: "kaneo_label Data Source - Kaneo"
description: |-
  Retrieves a workspace-level Kaneo label by ID.
---

# kaneo_label

Retrieves a workspace label by ID. Missing labels, task-copy IDs, and failed lookups are errors.

## Example usage

```terraform
data "kaneo_label" "bug" {
  id = "existing-workspace-label-id"
}
```

Pass `data.kaneo_label.bug.id` to [`kaneo_task_label.label_id`](/providers/glitchedmob/kaneo/latest/docs/resources/task_label) to attach it without managing the source label.

## Argument reference

- `id` (String, Required) Non-empty workspace-level label identifier, not a task-copy ID.

## Attribute reference

- `workspace_id` (String) Workspace identifier.
- `name` (String) Label name.
- `color` (String) Label color.
- `created_at` (String) Label creation timestamp in RFC3339 format.
- `updated_at` (String) Label last update timestamp in RFC3339 format.
