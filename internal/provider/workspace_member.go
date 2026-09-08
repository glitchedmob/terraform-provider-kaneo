// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"net/mail"
	"net/url"
	"strings"
	"time"
	"unicode"

	kaneoclient "github.com/glitchedmob/terraform-provider-kaneo/internal/client"
)

func validMemberEmail(email string) bool {
	a, err := mail.ParseAddress(email)
	return err == nil && a.Address == email && email == strings.ToLower(email) && !strings.ContainsAny(email, " \t\r\n")
}
func validMemberRole(role string) bool {
	return role != "" && strings.IndexFunc(role, unicode.IsSpace) < 0 && !strings.Contains(role, ",")
}
func memberIdentity(workspace, email string) string {
	return url.PathEscape(workspace) + "/" + url.PathEscape(email)
}
func parseMemberIdentity(id string) (string, string, error) {
	parts := strings.Split(id, "/")
	if len(parts) == 2 {
		ws, e1 := url.PathUnescape(parts[0])
		email, e2 := url.PathUnescape(parts[1])
		if e1 == nil && e2 == nil && ws != "" && strings.IndexFunc(ws, unicode.IsSpace) < 0 && validMemberEmail(email) && memberIdentity(ws, email) == id {
			return ws, email, nil
		}
	}
	return "", "", fmt.Errorf("expected percent-escaped workspace_id/email with a canonical lowercase email; escape each part using URL path escaping")
}

type memberObservation struct {
	member           *kaneoclient.WorkspaceMembershipMember
	live             *kaneoclient.WorkspaceInvitation
	pending          []kaneoclient.WorkspaceInvitation // Includes expired pending rows for cleanup.
	accepted         []string
	missingWorkspace bool
}

func (o memberObservation) absent() bool { return o.member == nil && o.live == nil }

// A known invitation distinguishes an in-flight acceptance from older history.
func (o memberObservation) acceptanceGap(invitationID string) bool {
	for _, id := range o.accepted {
		if invitationID == "" || id == invitationID {
			return true
		}
	}
	return false
}

// Lists are not snapshots. Reject moving totals, duplicate IDs and truncated
// invitations rather than infer absence from an incomplete observation.
func observeWorkspaceMember(ctx context.Context, client *kaneoclient.ClientWithResponses, ws, email string) (memberObservation, error) {
	var out memberObservation
	present, err := workspacePresent(ctx, client, ws)
	if err != nil {
		return out, err
	}
	if !present {
		out.missingWorkspace = true
		return out, nil
	}
	seen := map[string]bool{}
	total := -1
	last := ""
	for offset := 0; ; {
		if offset < 0 {
			return out, fmt.Errorf("member pagination offset overflow")
		}
		response, err := client.ListOrganizationMembersWithResponse(ctx, &kaneoclient.ListOrganizationMembersParams{OrganizationId: ws, Limit: 100, Offset: offset, SortBy: "id", SortDirection: "asc"})
		if err != nil {
			return out, fmt.Errorf("list members: %w", err)
		}
		if response.StatusCode() != 200 {
			return out, apiResponseError("list members", response.StatusCode(), response.Body)
		}
		page := response.JSON200
		if page == nil || page.Members == nil || page.Total == nil || *page.Total < 0 || len(page.Members) > 100 {
			return out, fmt.Errorf("list members: invalid members/total response")
		}
		if total < 0 {
			total = *page.Total
		}
		if total != *page.Total {
			return out, fmt.Errorf("list members: total changed during pagination; retry")
		}
		for _, m := range page.Members {
			if m.Id == "" || m.OrganizationId != ws || m.UserId == "" || m.User == nil || m.User.Id != m.UserId || m.User.Email == "" || m.Role == "" || seen[m.Id] || (last != "" && m.Id <= last) {
				return out, fmt.Errorf("list members: malformed, duplicate or non-progressing member page; retry")
			}
			seen[m.Id] = true
			last = m.Id
			if strings.EqualFold(m.User.Email, email) {
				if out.member != nil {
					return out, fmt.Errorf("multiple members match this email; resolve duplicate membership before retrying")
				}
				if !validMemberRole(m.Role) {
					return out, fmt.Errorf("target has a multi-role or noncanonical assignment; this resource manages one role and will not select or overwrite one implicitly")
				}
				copy := m
				out.member = &copy
			}
		}
		offset += len(page.Members)
		if offset > total || (offset < total && len(page.Members) == 0) {
			return out, fmt.Errorf("list members: page did not progress toward total; retry")
		}
		if offset == total {
			break
		}
	}
	response, err := client.ListOrganizationInvitationsWithResponse(ctx, &kaneoclient.ListOrganizationInvitationsParams{OrganizationId: ws})
	if err != nil {
		return out, fmt.Errorf("list invitations: %w", err)
	}
	if response.StatusCode() != 200 {
		return out, apiResponseError("list invitations", response.StatusCode(), response.Body)
	}
	if response.JSON200 == nil || *response.JSON200 == nil {
		return out, fmt.Errorf("list invitations: expected an invitation array")
	}
	if len(*response.JSON200) >= 100 {
		return out, fmt.Errorf("list invitations reached the 100 stored-row cap without pagination; discovery and cleanup cannot be proven complete. Reduce stored invitation history upstream before retrying; no access was changed by discovery")
	}
	seen = map[string]bool{}
	for _, i := range *response.JSON200 {
		if i.Id == "" || i.OrganizationId != ws || i.Email == "" || i.ExpiresAt.IsZero() || seen[i.Id] {
			return out, fmt.Errorf("list invitations: malformed or duplicate invitation")
		}
		seen[i.Id] = true
		switch i.Status {
		case "pending", "accepted", "rejected", "canceled":
		default:
			return out, fmt.Errorf("list invitations: unknown status")
		}
		if !strings.EqualFold(i.Email, email) {
			continue
		}
		if i.Status == "accepted" {
			out.accepted = append(out.accepted, i.Id)
		}
		if i.Status != "pending" {
			continue
		}
		out.pending = append(out.pending, i)
		if !i.ExpiresAt.After(time.Now()) {
			continue
		}
		role, err := i.Role.Get()
		if err != nil || !validMemberRole(role) {
			return out, fmt.Errorf("pending invitation has a missing or unsupported role")
		}
		if out.live != nil {
			return out, fmt.Errorf("multiple live invitations match this email; resolve duplicates before retrying")
		}
		copy := i
		out.live = &copy
	}
	return out, nil
}

func inviteWorkspaceMember(ctx context.Context, client *kaneoclient.ClientWithResponses, ws, email, role string) (*kaneoclient.WorkspaceInvitation, error) {
	body := kaneoclient.InviteOrganizationMemberJSONRequestBody{OrganizationId: &ws, Email: email}
	if err := body.Role.FromInviteOrganizationMemberJSONBodyRole0(role); err != nil {
		return nil, err
	}
	response, err := client.InviteOrganizationMemberWithResponse(ctx, body)
	if err != nil {
		return nil, fmt.Errorf("invite member: %w; creation may have succeeded. Inspect invitations and import workspace_id/email before retrying", err)
	}
	if response.StatusCode() != 200 {
		return nil, apiResponseError("invite member", response.StatusCode(), response.Body)
	}
	i := response.JSON200
	if i == nil {
		return nil, fmt.Errorf("invite member: invalid response; inspect invitations and import if created")
	}
	got, e := i.Role.Get()
	if i.Id == "" || i.OrganizationId != ws || !strings.EqualFold(i.Email, email) || i.Status != "pending" || !i.ExpiresAt.After(time.Now()) || e != nil || got != role {
		return nil, fmt.Errorf("invite member: response did not confirm requested pending invitation; inspect and import if created")
	}
	return i, nil
}

func cancelWorkspaceInvitation(ctx context.Context, client *kaneoclient.ClientWithResponses, ws, email string, i kaneoclient.WorkspaceInvitation) error {
	response, err := client.CancelOrganizationInvitationWithResponse(ctx, kaneoclient.CancelOrganizationInvitationJSONRequestBody{InvitationId: i.Id})
	if err != nil {
		return fmt.Errorf("cancel invitation: %w", err)
	}
	if response.StatusCode() == 400 && (authErrorCode(response.Body, "MEMBER_NOT_FOUND") || authErrorCode(response.Body, "INVITATION_NOT_FOUND")) {
		o, e := observeWorkspaceMember(ctx, client, ws, email)
		if e != nil {
			return e
		}
		for _, pending := range o.pending {
			if pending.Id == i.Id {
				return apiResponseError("cancel invitation", response.StatusCode(), response.Body)
			}
		}
		return nil
	}
	if response.StatusCode() != 200 {
		return apiResponseError("cancel invitation", response.StatusCode(), response.Body)
	}
	v := response.JSON200
	if v == nil || v.Id != i.Id || v.OrganizationId != ws || !strings.EqualFold(v.Email, email) || v.Status != "canceled" {
		return fmt.Errorf("cancel invitation: response did not confirm cancellation; refresh before retrying")
	}
	return nil
}

// Three immediate reconciliation passes cover observable concurrent transitions.
// Acceptance creates its member separately, so this cannot guarantee atomicity.
func reconcileWorkspaceMember(ctx context.Context, client *kaneoclient.ClientWithResponses, ws, email, role string, deleting, knownMember bool, knownInvitation string) (memberObservation, error) {
	var out memberObservation
	for attempt := 0; attempt < 3; attempt++ {
		o, err := observeWorkspaceMember(ctx, client, ws, email)
		out = o
		if err != nil {
			return out, err
		}
		if o.missingWorkspace {
			if deleting {
				return out, nil
			}
			return out, fmt.Errorf("workspace no longer exists; refresh and recreate")
		}
		changed := false
		for _, i := range o.pending {
			if deleting || o.member != nil || o.live == nil || i.Id != o.live.Id || invitationRole(i) != role {
				if err := cancelWorkspaceInvitation(ctx, client, ws, email, i); err != nil {
					return out, err
				}
				changed = true
			}
		}
		// Re-read after cancellation: acceptance may have won before cancellation.
		if changed {
			continue
		}
		if o.member != nil {
			m := o.member
			knownMember = true
			if !deleting && m.Role == role {
				return out, nil
			}
			if deleting {
				response, e := client.RemoveOrganizationMemberWithResponse(ctx, kaneoclient.RemoveOrganizationMemberJSONRequestBody{OrganizationId: &ws, MemberIdOrEmail: m.Id})
				if e != nil {
					return out, fmt.Errorf("remove member: %w", e)
				}
				if response.StatusCode() == 400 && authErrorCode(response.Body, "MEMBER_NOT_FOUND") {
					continue
				} // Next observation verifies operator access and target absence.
				if response.StatusCode() != 200 {
					return out, apiResponseError("remove member", response.StatusCode(), response.Body)
				}
				if response.JSON200 == nil || response.JSON200.Member.Id != m.Id || response.JSON200.Member.OrganizationId != ws || response.JSON200.Member.UserId != m.UserId {
					return out, fmt.Errorf("remove member: invalid confirmation; refresh before retrying")
				}
				// Historical accepted invitations no longer indicate an unobserved creation
				// once this specific accepted member has been removed. Verify below.
				next, e := observeWorkspaceMember(ctx, client, ws, email)
				if e != nil {
					return out, e
				}
				out = next
				if next.member == nil && len(next.pending) == 0 {
					return out, nil
				}
				continue
			}
			body := kaneoclient.UpdateOrganizationMemberRoleJSONRequestBody{OrganizationId: &ws, MemberId: m.Id}
			if e := body.Role.FromUpdateOrganizationMemberRoleJSONBodyRole0(role); e != nil {
				return out, e
			}
			response, e := client.UpdateOrganizationMemberRoleWithResponse(ctx, body)
			if e != nil {
				return out, fmt.Errorf("update member role: %w", e)
			}
			if response.StatusCode() == 400 && authErrorCode(response.Body, "MEMBER_NOT_FOUND") {
				continue
			}
			if response.StatusCode() != 200 {
				return out, apiResponseError("update member role", response.StatusCode(), response.Body)
			}
			v := response.JSON200
			if v == nil || v.Id != m.Id || v.OrganizationId != ws || v.UserId != m.UserId || v.Role != role {
				return out, fmt.Errorf("update member role: expected bare member confirming requested role")
			}
			out.member.Role = role
			return out, nil
		}
		if o.acceptanceGap(knownInvitation) && !knownMember {
			continue
		}
		if deleting {
			return out, nil
		}
		if o.live != nil {
			return out, nil
		}
		i, e := inviteWorkspaceMember(ctx, client, ws, email, role)
		if e != nil {
			// Do not retry an ambiguous invite. Discovery may establish that acceptance
			// won, in which case the next pass updates the member instead.
			next, readErr := observeWorkspaceMember(ctx, client, ws, email)
			if readErr == nil && next.member != nil {
				continue
			}
			return out, fmt.Errorf("pending invitation may have been canceled but replacement was not confirmed: %w; refresh before retrying", e)
		}
		out.live = i
		return out, nil
	}
	return out, fmt.Errorf("membership changed concurrently or an accepted invitation has no member yet; state retained. Refresh and retry after the external operation finishes")
}
func invitationRole(i kaneoclient.WorkspaceInvitation) string { v, _ := i.Role.Get(); return v }
