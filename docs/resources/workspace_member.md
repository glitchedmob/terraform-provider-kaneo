---
page_title: "kaneo_workspace_member Resource - Kaneo"
description: |-
  Manages a workspace invitation and its accepted membership.
---

# kaneo_workspace_member

Manages access for one email in one workspace. Create sends an invitation and returns immediately with `status = "pending"`. The recipient accepts independently through Kaneo. The provider never signs in as the recipient, impersonates users, or waits for acceptance. Invitation creation does not prove email delivery; SMTP is a server concern.

Refresh observes either the live invitation or accepted membership without resending. Accepted membership takes precedence over invitation history, and the Terraform ID stays unchanged. Rejected, canceled, or expired invitations without a member allow recreation on the next apply. External removal of a previously observed member also allows recreation through a new invitation.

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
    task = ["read", "update"]
  }
}

resource "kaneo_workspace_member" "teammate" {
  workspace_id = kaneo_workspace.engineering.id
  email        = "teammate@example.com"
  role         = kaneo_workspace_role.triage.name
}
```

The recipient need not exist when invited. `kaneo_user` can manage the account separately; this resource never creates or deletes user accounts. Use a workspace role's `.name`, not `.id`. Instance roles on `kaneo_user` do not grant workspace access.

## Argument reference

- `workspace_id` - Required workspace identifier. Changes replace the resource.
- `email` - Required canonical lowercase email without whitespace or a display name. Changes replace the resource. Mixed-case configuration is rejected, not silently rewritten. If referencing a mixed-case `kaneo_user.email`, use `lower(kaneo_user.recipient.email)` explicitly.
- `role` - Required single, nonempty role name without whitespace or commas. Supports custom names and built-in roles. Kaneo validates role existence. Multi-role assignments are rejected during discovery and import rather than selecting one role or silently overwriting the others.

## Attribute reference

- `id` - Stable `workspace_id/email` identity, with each part URL path-escaped.
- `status` - `pending` or `accepted`, as last observed.
- `member_id` - Native accepted member ID, or null while pending.
- `invitation_id` - Live pending invitation ID, or null when none is outstanding. A pending invitation can coexist with a member; destroy cleans up both.

## Import

Existing membership or a live invitation requires import. Create does not adopt preexisting access.

```shell
terraform import kaneo_workspace_member.teammate 'workspace-id/teammate@example.com'
```

Import uses exactly two URL path-escaped components separated by `/`. Email must be lowercase. Escape a slash inside either component as `%2F`, and a literal percent sign as `%25`. For example, `workspace-id/slash%2Fname@example.com`. The parser rejects malformed escapes, extra separators, and noncanonical encodings. Ordinary `@` and `+` remain unescaped, following Go's `url.PathEscape`. The ID is not an invitation, member, or user ID.

## Updates and deletion

An accepted role update replaces the member's role. A pending role update cancels the old invitation, rechecks for concurrent acceptance, then creates a replacement invitation with the new role. It does not use resend, which leaves the old role unchanged. If acceptance wins, the provider updates the actual member instead.

Destroy cancels outstanding pending invitations, including expired pending rows, then removes accepted membership by native member ID and verifies cleanup. It does not touch unrelated emails, workspaces, or user accounts.

The operator must belong to the workspace. Create requires `invitation.create`; pending updates need `invitation.cancel` and `invitation.create`; accepted updates need `member.update`; destroy needs `invitation.cancel` and `member.delete` as applicable. An accepted update also cancels any outstanding invitations. Instance admin grants no bypass. Kaneo enforces owner and last-owner restrictions. Avoid managing the operator's own access, since revocation can prevent verification and future refresh.

## Limits and failure handling

Kaneo 2.23.2 lists at most **100 stored invitations**, including terminal history, and exposes no pagination. A result of 100 or more fails closed for refresh, import, and mutations, even if the target appears in that result. It cannot prove that unseen duplicate invitations are absent. An administrator must reduce stored invitation history outside this provider before retrying; cancellation alone does not reduce the stored row count. This resource does not delete invitation history or alter upstream endpoints.

Members are scanned in explicit 100-row pages ordered by ID. Changed totals, duplicate or non-progressing pages, ambiguous targets, malformed responses, and unknown invitation statuses produce errors rather than disappearance. Only `workspacePresent`'s explicit `ORGANIZATION_NOT_FOUND` proves workspace deletion. Generic 400/401/403 errors and lost permissions retain state. `MEMBER_NOT_FOUND` on a mutation triggers fresh scoped discovery to confirm operator access and target absence.

Kaneo changes invitation status and creates membership separately. Cancel can overwrite a concurrently accepted status. The provider makes at most three immediate reconciliation passes, without sleeps or acceptance polling. An accepted invitation without its member retains pending/import state with a retry diagnostic. A previously observed member's disappearance is removal; historical accepted invitations alone do not block a new Create after repeated absent observations.

These operations are not atomic. Even a clean final observation cannot exclude an already-running acceptance that inserts a member later. Coordinate external changes with Terraform and refresh afterward. Cancellations or member mutations can succeed before a later operation fails. Errors retain prior state and describe partial completion; refresh before retrying. If Create has an ambiguous response, inspect the workspace and import any created invitation instead of blindly retrying.
