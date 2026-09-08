---
page_title: "kaneo_label Resource - Kaneo"
description: |-
  Manages a workspace-level Kaneo label.
---

# kaneo_label

Manages a workspace-level label. Use [`kaneo_task_label`](task_label.md) to attach it to a task.

Deleting or replacing a workspace label also deletes same-name task-label copies throughout that workspace, including copies not managed by Terraform. Renaming or recoloring the workspace label updates those copies. These are Kaneo API behaviors, not Terraform collection management.

## Example usage

```terraform
resource "kaneo_workspace" "engineering" {
  name = "Engineering"
  slug = "engineering"
}

resource "kaneo_label" "bug" {
  workspace_id = kaneo_workspace.engineering.id
  name         = "Bug"
  color        = "#ef4444"
}
```

## Argument reference

- `workspace_id` (String, Required) Workspace identifier. Changing this replaces the label and deletes matching task copies in the previous workspace.
- `name` (String, Required) Non-empty label name, unique among workspace-level labels in this workspace.
- `color` (String, Required) Non-empty color string, for example `#ef4444`. Passed through to Kaneo without normalization.

Kaneo identifies task copies for cascading changes by workspace and the label's previous name, not by a source-label foreign key. A copy with the same name is affected even if it was created separately. A copy renamed independently no longer matches that cascade.

The provider checks for existing workspace labels before creating one and reports a collision instead of silently adopting it. Import existing labels. These checks cannot prevent concurrent creates from racing; manage each workspace/name pair in only one Terraform resource and avoid concurrent creation of the same name elsewhere.

## Attribute reference

- `id` (String) Workspace label identifier, not a task-copy ID.
- `created_at` (String) Label creation timestamp in RFC3339 format.
- `updated_at` (String) Label last update timestamp in RFC3339 format.

## Import

Import by workspace-level label ID:

```shell
terraform import kaneo_label.bug workspace-label-id
```

Terraform 1.5 or later also supports declarative import:

```terraform
import {
  to = kaneo_label.bug
  id = "workspace-label-id"
}
```

Configure `workspace_id`, `name`, and `color` to match the existing label before applying. Task-specific labels are rejected; use the task-label resource to import those copies.

## Drift and deletion

Terraform restores configured names and colors changed outside Terraform and recreates deleted labels. Because Kaneo can report a missing label as HTTP 400, the provider confirms absence against a successful workspace label list. Access errors, malformed responses, or a failed workspace lookup produce diagnostics instead of removing state. An externally deleted workspace requires separate reconciliation if its label list can no longer be read.

To remove a label from just one task, destroy its `kaneo_task_label` resource, not this workspace-label resource.
