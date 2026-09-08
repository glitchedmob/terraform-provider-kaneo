// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"

	kaneoclient "github.com/glitchedmob/terraform-provider-kaneo/internal/client"
)

func validTeam(team *kaneoclient.OrganizationTeam, workspaceID, id string) bool {
	return team != nil && team.Id != "" && (id == "" || team.Id == id) && team.OrganizationId == workspaceID && team.Name != nil
}

// getTeam returns nil only after a complete scoped list proves team absence or
// workspacePresent proves parent absence. Access denial must retain state.
func getTeam(ctx context.Context, client *kaneoclient.ClientWithResponses, workspaceID, id string) (*kaneoclient.OrganizationTeam, error) {
	if workspaceID == "" || id == "" {
		return nil, fmt.Errorf("get team: workspace and team IDs are required")
	}
	response, err := client.ListOrganizationTeamsWithResponse(ctx, &kaneoclient.ListOrganizationTeamsParams{OrganizationId: workspaceID})
	if err != nil {
		return nil, fmt.Errorf("list teams: %w", err)
	}
	if response.StatusCode() == 403 {
		present, err := workspacePresent(ctx, client, workspaceID)
		if err != nil {
			return nil, err
		}
		if !present {
			return nil, nil
		}
	}
	if response.StatusCode() != 200 {
		return nil, apiResponseError("list teams", response.StatusCode(), response.Body)
	}
	// Better Auth listTeams has no pagination and defaults findMany to 100.
	// Kaneo's maximumTeams=10 stays below this, but fail closed on a capped list.
	if response.JSON200 == nil || *response.JSON200 == nil || len(*response.JSON200) >= 100 {
		return nil, fmt.Errorf("list teams: missing or potentially incomplete team list")
	}
	seen := make(map[string]bool)
	var found *kaneoclient.OrganizationTeam
	for _, team := range *response.JSON200 {
		if !validTeam(&team, workspaceID, "") || seen[team.Id] {
			return nil, fmt.Errorf("list teams: invalid scope, missing fields, or duplicate team ID")
		}
		seen[team.Id] = true
		if team.Id == id {
			found = &team
		}
	}
	return found, nil
}
