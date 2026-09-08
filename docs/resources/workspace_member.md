---
page_title: "kaneo_workspace_member Resource - Kaneo"
description: |-
  Manages a workspace invitation and its accepted membership.
---

# kaneo_workspace_member

Manages one email's workspace access. Create returns `pending`; the recipient accepts through Kaneo without provider impersonation or polling. Invitation creation does not prove delivery; configure SMTP on the server.

Refresh never resends. Accepted membership takes precedence over invitation history without changing the Terraform ID. Rejected, canceled, or expired invitations without a member, or external removal of an observed member, allow a new invitation on apply.

## Example usage

```terraform
# Replace with an existing workspace ID.
resource "kaneo_workspace_role" "triage" {
  workspace_id = "existing-workspace-id"
  name         = "triage"
  permissions = {
    task = ["read", "update"]
  }
}

resource "kaneo_workspace_member" "teammate" {
  workspace_id = kaneo_workspace_role.triage.workspace_id
  email        = "teammate@example.com"
  role         = kaneo_workspace_role.triage.name
}
```

The recipient need not exist when invited. `kaneo_user` can manage the account separately; this resource never creates or deletes user accounts. Use a workspace role's `.name`, not `.id`. Instance roles on `kaneo_user` do not grant workspace access.

## Argument reference

- `workspace_id` (String, Required) Workspace identifier. Changes replace the resource.
- `email` (String, Required) Canonical lowercase email without whitespace or a display name. Changes replace the resource. Mixed-case configuration is rejected, not silently rewritten. If referencing a mixed-case `kaneo_user.email`, use `lower(kaneo_user.recipient.email)` explicitly.
- `role` (String, Required) Single, nonempty role name without whitespace or commas. Supports custom names and built-in roles. Kaneo validates role existence. Multi-role assignments are rejected during discovery and import rather than selecting one role or silently overwriting the others.

## Attribute reference

- `id` (String) Stable `workspace_id/email` identity, with each part URL path-escaped.
- `status` (String) `pending` or `accepted`, as last observed.
- `member_id` (String) Native accepted member ID, or null while pending.
- `invitation_id` (String) Live pending invitation ID, or null when none is outstanding. A pending invitation can coexist with a member; destroy cleans up both.

## Import

Existing membership or a live invitation requires import. Create does not adopt preexisting access.

```shell
terraform import kaneo_workspace_member.teammate 'workspace-id/teammate@example.com'
```

Import uses exactly two URL path-escaped components separated by `/`. Email must be lowercase. Escape a slash inside either component as `%2F`, and a literal percent sign as `%25`. For example, `workspace-id/slash%2Fname@example.com`. The parser rejects malformed escapes, extra separators, and noncanonical encodings. Ordinary `@` and `+` remain unescaped. The ID is not an invitation, member, or user ID.

## Updates and deletion

Accepted updates replace the member's role. Pending updates cancel and replace the invitation, or update the member if acceptance wins the race; resending alone cannot change a role.

Destroy cancels outstanding pending invitations, including expired pending rows, then removes accepted membership by native member ID and verifies cleanup. It does not touch unrelated emails, workspaces, or user accounts.

The operator must belong to the workspace. Create requires `invitation.create`; pending updates need `invitation.cancel` and `invitation.create`; accepted updates need `member.update`; destroy needs `invitation.cancel` and `member.delete` as applicable. An accepted update also cancels any outstanding invitations. Instance admin grants no bypass. Kaneo enforces owner and last-owner restrictions. Avoid managing the operator's own access, since revocation can prevent verification and future refresh.

## Limits and failure handling

Kaneo 2.23.2 exposes at most 100 stored invitations, including terminal history, without pagination. A result of 100 or more blocks refresh, import, and mutations even if the target is visible, since unseen duplicates cannot be ruled out. An administrator must reduce stored invitation history outside this provider before retrying. Cancellation does not reduce the row count; this resource cannot delete history.

Ambiguous or inconsistent discovery, malformed responses, and unknown statuses retain state with errors. Lost permissions and generic 400/401/403 errors do not prove absence; only explicit `ORGANIZATION_NOT_FOUND` proves workspace deletion. A missing-member mutation is rechecked with operator access before treating the target as absent.

Acceptance and membership creation are not atomic; cancellation can overwrite concurrent acceptance. An accepted invitation without its member retains pending/import state and asks you to retry. Historical accepted invitations alone do not block recreation after confirmed member absence.

Coordinate external changes and refresh afterward: in-flight acceptance can add a member even after cleanup verification. Cancellations or member changes may succeed before a later failure. Errors retain prior state and describe partial completion; refresh before retrying. After an ambiguous Create response, inspect the workspace and import any created invitation rather than blindly retrying.
