---
page_title: "kaneo_task_label Resource - Kaneo"
description: |-
  Attaches a workspace-level label to a Kaneo task.
---

# kaneo_task_label

Attaches a workspace label as a task-specific copy with its own ID. Destroy detaches only that copy, not the workspace label, task, or other attachments.

## Example usage

```terraform
# Existing workspace label and task must be in the same workspace.
resource "kaneo_task_label" "bug" {
  label_id = "existing-workspace-label-id"
  task_id  = "existing-task-id"
}
```

When Terraform manages the label or task, use `kaneo_label.bug.id` and `kaneo_task.fix.id` instead of literals to establish creation/deletion dependencies and reconnect recreated objects. A label data source can supply an externally managed source ID.

## Argument reference

- `label_id` (String, Required) Non-empty source workspace-level label ID. Task-specific copies are rejected because Kaneo's attach endpoint can move them away from another task. Changing this replaces the attachment.
- `task_id` (String, Required) Non-empty task ID in the same workspace as the source label. Changing this replaces the attachment.

Only one same-name label is allowed per task. Import existing copies; creates do not adopt them. Manage each task/name pair once and avoid concurrent creates. Use default destroy-before-create replacement; `create_before_destroy` can collide on the same task.

## Attribute reference

- `id` (String) ID of the task-specific label copy returned by Kaneo. Different from `label_id`.
- `workspace_id` (String) Workspace identifier shared by the task and source label.

Name and color are not managed here; independent edits do not replace the attachment. Workspace-label updates and deletion cascade to same-name copies, including unmanaged ones. See [label cascades](/providers/glitchedmob/kaneo/latest/docs/resources/label).

## Import

Import using `source-label-id/task-label-id`:

```shell
terraform import kaneo_task_label.bug workspace-label-id/task-label-copy-id
```

The second ID is the copy ID, not the task ID. Import reads `task_id` and `workspace_id` from it. Supply a workspace-level source in the same workspace and configure the copy's current task. Kaneo stores no source-to-copy link; Terraform cannot prove the original source and uses your `label_id` for future recreation. See the [import guide](/providers/glitchedmob/kaneo/latest/docs/guides/import).

## Limitations and recovery

Terraform recreates externally detached copies but leaves copies moved to another task alone. Failed or unauthorized workspace lookups retain state; reconcile an externally deleted workspace separately if its label list is unreadable.
