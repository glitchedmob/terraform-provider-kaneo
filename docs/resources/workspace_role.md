---
page_title: "kaneo_workspace_role Resource - Kaneo"
description: |-
  Manages a dynamic Kaneo workspace role.
---

# kaneo_workspace_role

Manages a dynamic workspace role. It neither assigns memberships nor changes `kaneo_user` instance roles.

The caller must belong to the workspace and hold `ac.read` for refresh and import, `ac.create` for creation, `ac.update` for updates, and `ac.delete` for deletion. Creation and permission updates also require every permission being granted. An instance admin has no bypass. Avoid managing a role that supplies the operator's own permissions, since an update can lock Terraform out.

## Example usage

```terraform
# Replace with an existing workspace ID.
resource "kaneo_workspace_role" "triage" {
  workspace_id = "existing-workspace-id"
  name         = "triage"
  permissions = {
    project = ["read"]
    task    = ["read", "update"]
    label   = ["read"]
  }
}
```

Assign memberships with `kaneo_workspace_role.triage.name`, not `.id`, to establish dependencies. Renaming does not rewrite existing memberships; update their role names too.

## Argument reference

- `workspace_id` (String, Required) Workspace identifier. Changing it replaces the role.
- `name` (String, Required) Lowercase name without whitespace or commas. Rename is supported in place and preserves the native ID. `owner` is reserved.
- `permissions` (Map of Sets of String, Required) Permission resources mapped to action sets. Updates replace the whole map. Action ordering is insignificant. Empty maps and empty sets are supported; null sets or actions are not. Kaneo validates permission resources and the caller's ability to grant each action.

## Attribute reference

- `id` (String) Native workspace role ID, not the role name or a composite ID.

## Import

Import uses two native IDs separated by a slash. Neither ID may contain a slash.

```shell
terraform import kaneo_workspace_role.triage workspace-id/role-id
```

The authenticated `/api/auth/organization/list-roles?organizationId=workspace-id` endpoint returns role IDs, names, and permissions. Import reads the complete permission map.

Kaneo 2.23.2 seeds `admin`, `member`, and `viewer` as dynamic database roles. Import those existing rows rather than trying to create duplicates. Their permissions can be managed, but deleting an assigned role is rejected. Kaneo may recreate missing seeded roles at startup. The static `owner` role has no dynamic row and cannot be created, imported, or deleted with this resource. Kaneo limits each workspace to 25 dynamic roles, including seeded roles.

## Limitations and recovery

Only explicit `ROLE_NOT_FOUND` or confirmed `ORGANIZATION_NOT_FOUND` removes state. Lost membership, lost `ac.read`, generic 404s, authentication failures, and other lookup errors retain state; restore access before retrying.

Deletion requires both `ac.read` and `ac.delete`. Remove member assignments first; Kaneo rejects deleting assigned roles.
