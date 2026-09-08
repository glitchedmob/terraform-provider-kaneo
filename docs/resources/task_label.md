---
page_title: "kaneo_task_label Resource - Kaneo"
description: |-
  Attaches a workspace-level label to a Kaneo task.
---

# kaneo_task_label

Attaches a workspace-level label to a task. Kaneo creates a separate task-label copy with its own ID. Terraform tracks that returned ID rather than treating the source label as the attachment.

Destroying this resource detaches only the tracked copy. It does not delete the workspace label or task, and does not replace the task's collection of labels.

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

resource "kaneo_task" "fix" {
  project_id = kaneo_project.platform.id
  title      = "Fix deployment failure"
}

resource "kaneo_label" "bug" {
  workspace_id = kaneo_workspace.engineering.id
  name         = "Bug"
  color        = "#ef4444"
}

resource "kaneo_task_label" "bug" {
  label_id = kaneo_label.bug.id
  task_id  = kaneo_task.fix.id
}
```

References to the source label and task establish their creation and deletion dependencies. A label data source can supply `label_id` when the workspace label is managed elsewhere.

## Argument reference

- `label_id` (String, Required) Non-empty source workspace-level label ID. Task-specific copies are rejected because Kaneo's attach endpoint can move them away from another task. Changing this replaces the attachment.
- `task_id` (String, Required) Non-empty task ID in the same workspace as the source label. Changing this replaces the attachment.

Kaneo allows only one label with a given name on a task. If a same-name task copy already exists, the provider reports a collision and asks you to import it instead of silently taking ownership. Manage each task/name pair in only one resource. Preflight checks cannot prevent concurrent creates from racing. Use default destroy-before-create replacement; `create_before_destroy` can collide with an existing copy on the same task.

## Attribute reference

- `id` (String) ID of the task-specific label copy returned by Kaneo. Different from `label_id`.
- `workspace_id` (String) Workspace identifier shared by the task and source label.

This resource manages the attachment's presence, not its name or color. Workspace-label updates cascade to same-name task copies through Kaneo. Independently editing a task copy's name or color does not trigger an attachment replacement. Deleting the workspace label also deletes matching task copies, including unmanaged ones. See [`kaneo_label`](label.md) for cascade behavior.

## Import

Import using `source-label-id/task-label-id`:

```shell
terraform import kaneo_task_label.bug workspace-label-id/task-label-copy-id
```

Terraform 1.5 or later also supports declarative import:

```terraform
import {
  to = kaneo_task_label.bug
  id = "workspace-label-id/task-label-copy-id"
}
```

The second ID is the task copy's ID, not the task ID. Read obtains `task_id` and `workspace_id` from the copy. The source must be a workspace-level label in the same workspace. Kaneo stores no source-to-copy foreign key, so Terraform cannot infer or prove the original source; the supplied `label_id` selects the source for future recreation. Configure the target resource with that source and the copy's current task.

## Drift and deletion

Terraform recreates externally detached copies. If a managed task or source label is deleted externally and recreated, references in the configuration reconnect the new objects. A copy moved to a different task is left alone rather than detached from that other task.

Missing-copy responses are confirmed against the workspace label list before state is removed. Permission errors and failed workspace lookups retain state and report diagnostics. Deleting the workspace externally can require separate reconciliation if its label list is no longer accessible.
