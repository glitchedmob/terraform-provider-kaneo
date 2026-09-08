// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"encoding/json"
	"fmt"

	kaneoclient "github.com/glitchedmob/terraform-provider-kaneo/internal/client"
)

func authErrorCode(body []byte, code string) bool {
	var payload struct {
		Code string `json:"code"`
	}
	return json.Unmarshal(body, &payload) == nil && payload.Code == code
}

// Unlike list-organization, this endpoint checks existence before membership.
// Only its explicit not-found response proves absence. A forbidden response
// means the operator lost access and must not remove dependent state.
func workspacePresent(ctx context.Context, client *kaneoclient.ClientWithResponses, id string) (bool, error) {
	response, err := client.GetWorkspacePresenceWithResponse(ctx, &kaneoclient.GetWorkspacePresenceParams{OrganizationId: id})
	if err != nil {
		return false, fmt.Errorf("get workspace presence: %w", err)
	}
	if response.StatusCode() == 400 && authErrorCode(response.Body, "ORGANIZATION_NOT_FOUND") {
		return false, nil
	}
	if response.StatusCode() != 200 {
		return false, apiResponseError("get workspace presence", response.StatusCode(), response.Body)
	}
	if response.JSON200 == nil || response.JSON200.Id != id {
		return false, fmt.Errorf("get workspace presence: response did not contain the requested workspace ID")
	}
	return true, nil
}
