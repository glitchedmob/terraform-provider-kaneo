---
page_title: "kaneo_workspace_role Resource - Kaneo"
description: |-
  Manages a dynamic Kaneo workspace role.
---

# kaneo_workspace_role

Manages a dynamic workspace role, not an instance role or a membership. Instance roles remain the `role` attribute of `kaneo_user`.

The caller must belong to the workspace and hold `ac.read` for refresh and import, `ac.create` for creation, `ac.update` for updates, and `ac.delete` for deletion. Creation and permission updates also require every permission being granted. An instance admin has no bypass. Avoid managing a role that supplies the operator's own permissions, since an update can lock Terraform out.

## Example usage

```terraform
resource "kaneo_workspace" "engineering" {
  name = "Engineering"
  slug = "engineering"
}

resource "kaneo_workspace_role" "triage" {
  workspace_id = kaneo_workspace.engineering.id
  name         = "triage"
  permissions = {
    project = ["read"]
    task    = ["read", "update"]
    label   = ["read"]
  }
}
```

Use `kaneo_workspace_role.triage.name` when assigning a workspace role, not its ID. This resource does not assign memberships. Better Auth renames the role row without rewriting existing memberships; update those assignments to the new name too.

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

## Failure handling

Explicit `ROLE_NOT_FOUND` removes the role from state. If role lookup is forbidden, the provider checks `get-full-organization`: only an explicit `ORGANIZATION_NOT_FOUND` proves that the workspace was deleted. Lost membership or lost `ac.read` remains an error, not disappearance. Authentication errors, generic 404s, malformed responses, and server failures also retain state and report errors.

Deletion reads the role first and therefore requires `ac.read` as well as `ac.delete`. Kaneo rejects deletion while the role is assigned to members. Remove those assignments before destroying the role.
