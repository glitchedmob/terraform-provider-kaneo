---
page_title: "kaneo_label Resource - Kaneo"
description: |-
  Manages a workspace-level Kaneo label.
---

# kaneo_label

Manages a workspace-level label. Use [`kaneo_task_label`](/providers/glitchedmob/kaneo/latest/docs/resources/task_label) to attach it to a task.

Deleting or replacing a workspace label also deletes same-name task-label copies throughout that workspace, including copies not managed by Terraform. Renaming or recoloring the workspace label updates those copies.

## Example usage

```terraform
# Replace with an existing workspace ID.
resource "kaneo_label" "bug" {
  workspace_id = "existing-workspace-id"
  name         = "Bug"
  color        = "#ef4444"
}
```

## Argument reference

- `workspace_id` (String, Required) Workspace identifier. Changing this replaces the label and deletes matching task copies in the previous workspace.
- `name` (String, Required) Non-empty label name, unique among workspace-level labels in this workspace.
- `color` (String, Required) Non-empty color string, for example `#ef4444`. Passed through to Kaneo without normalization.

Cascades match the workspace and previous label name, even for separately created copies. Independently renamed copies no longer match.

Import existing labels; creates do not adopt collisions. Manage each workspace/name pair once and avoid concurrent same-name creates, which can race.

## Attribute reference

- `id` (String) Workspace label identifier, not a task-copy ID.
- `created_at` (String) Label creation timestamp in RFC3339 format.
- `updated_at` (String) Label last update timestamp in RFC3339 format.

## Import

Import by workspace-level label ID:

```shell
terraform import kaneo_label.bug workspace-label-id
```

Match `workspace_id`, `name`, and `color` before applying. Task-copy IDs are rejected; import those with `kaneo_task_label`. See the [import guide](/providers/glitchedmob/kaneo/latest/docs/guides/import) for declarative imports and dependency guidance.

## Limitations and recovery

Terraform restores configured values and recreates deleted labels. Failed or unauthorized lookups retain state; reconcile an externally deleted workspace separately if its label list is unreadable.

To remove a label from just one task, destroy its `kaneo_task_label` resource, not this workspace-label resource.
