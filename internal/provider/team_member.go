// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"unicode"

	kaneoclient "github.com/glitchedmob/terraform-provider-kaneo/internal/client"
)

func validTeamMemberID(id string) bool {
	return id != "" && !strings.ContainsFunc(id, unicode.IsSpace)
}
func teamMemberID(workspace, team, user string) string {
	return url.PathEscape(workspace) + "/" + url.PathEscape(team) + "/" + url.PathEscape(user)
}
func parseTeamMemberID(id string) (workspace, team, user string, err error) {
	parts := strings.Split(id, "/")
	if len(parts) == 3 {
		for i := range parts {
			parts[i], err = url.PathUnescape(parts[i])
			if err != nil || !validTeamMemberID(parts[i]) {
				return "", "", "", fmt.Errorf("expected canonical workspace_id/team_id/user_id")
			}
		}
		if teamMemberID(parts[0], parts[1], parts[2]) == id {
			return parts[0], parts[1], parts[2], nil
		}
	}
	return "", "", "", fmt.Errorf("expected canonical workspace_id/team_id/user_id with each native ID URL path-escaped")
}

// parent is separate from member absence so Create cannot mutate a missing or
// foreign team. No target workspace scan is needed: add/remove enforce it.
func observeTeamMember(ctx context.Context, client *kaneoclient.ClientWithResponses, workspace, team, user string) (parent, member bool, err error) {
	if !validTeamMemberID(workspace) || !validTeamMemberID(team) || !validTeamMemberID(user) {
		return false, false, fmt.Errorf("workspace_id, team_id and user_id must be nonempty and contain no whitespace")
	}
	scoped, err := getTeam(ctx, client, workspace, team)
	if err != nil || scoped == nil {
		return false, false, err
	}
	response, err := client.ListOrganizationTeamMembersWithResponse(ctx, &kaneoclient.ListOrganizationTeamMembersParams{TeamId: team})
	if err != nil {
		return true, false, fmt.Errorf("list team members: %w", err)
	}
	if response.StatusCode() == 400 && authErrorCode(response.Body, "USER_IS_NOT_A_MEMBER_OF_THE_TEAM") {
		session, sessionErr := client.GetSessionWithResponse(ctx)
		// Never include the session response body or decoder error in diagnostics.
		if sessionErr != nil || session.StatusCode() != 200 || session.JSON200 == nil || !validTeamMemberID(session.JSON200.User.Id) {
			return true, false, fmt.Errorf("cannot determine authenticated user ID for team membership access check")
		}
		// Recheck after the denied list: the same code also means lost workspace access.
		scoped, err = getTeam(ctx, client, workspace, team)
		if err != nil || scoped == nil {
			return false, false, err
		}
		if session.JSON200.User.Id == user {
			return true, false, nil
		}
		return true, false, fmt.Errorf("operator cannot list this team's members; manage the operator's own kaneo_team_member separately and add depends_on to other memberships. State is retained; no implicit self-add")
	}
	if response.StatusCode() == 400 && authErrorCode(response.Body, "TEAM_NOT_FOUND") {
		scoped, err = getTeam(ctx, client, workspace, team)
		if err != nil || scoped == nil {
			return false, false, err
		}
	}
	if response.StatusCode() != 200 {
		return true, false, apiResponseError("list team members", response.StatusCode(), response.Body)
	}
	if response.JSON200 == nil || *response.JSON200 == nil || len(*response.JSON200) >= 100 {
		return true, false, fmt.Errorf("list team members: missing or potentially incomplete list, must contain fewer than 100 rows")
	}
	rows, users := map[string]bool{}, map[string]bool{}
	for _, row := range *response.JSON200 {
		if !validTeamMemberID(row.Id) || row.TeamId != team || !validTeamMemberID(row.UserId) || rows[row.Id] || users[row.UserId] {
			return true, false, fmt.Errorf("list team members: invalid identity, scope, duplicate row ID or duplicate user")
		}
		rows[row.Id], users[row.UserId] = true, true
	}
	return true, users[user], nil
}

func teamMemberMutationError(operation string, status int, body []byte) error {
	if status == 400 && authErrorCode(body, "USER_IS_NOT_A_MEMBER_OF_THE_ORGANIZATION") {
		return fmt.Errorf("%s: operator and user_id must be accepted workspace members; a pending invitation is not membership. Accept the workspace invitation and retry. An existing orphan team row requires upstream repair or restoration of workspace access by the operator; state is retained. No workspace membership was changed", operation)
	}
	return apiResponseError(operation, status, body)
}
