---
page_title: "kaneo_label Data Source - Kaneo"
description: |-
  Retrieves a workspace-level Kaneo label by ID.
---

# kaneo_label

Retrieves a workspace-level label by its ID. Missing labels, task-specific copies, and lookup failures produce diagnostics.

## Example usage

```terraform
data "kaneo_label" "bug" {
  id = "workspace-label-id"
}
```

Use `data.kaneo_label.bug.id` as the `label_id` of a `kaneo_task_label` resource to attach an existing workspace label without managing the workspace label itself.

## Argument reference

- `id` (String, Required) Non-empty workspace-level label identifier, not a task-copy ID.

## Attribute reference

- `workspace_id` (String) Workspace identifier.
- `name` (String) Label name.
- `color` (String) Label color.
- `created_at` (String) Label creation timestamp in RFC3339 format.
- `updated_at` (String) Label last update timestamp in RFC3339 format.
