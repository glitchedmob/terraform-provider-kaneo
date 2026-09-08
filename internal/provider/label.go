// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"time"

	kaneoclient "github.com/glitchedmob/terraform-provider-kaneo/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type labelModel struct {
	ID          types.String `tfsdk:"id"`
	WorkspaceID types.String `tfsdk:"workspace_id"`
	Name        types.String `tfsdk:"name"`
	Color       types.String `tfsdk:"color"`
	CreatedAt   types.String `tfsdk:"created_at"`
	UpdatedAt   types.String `tfsdk:"updated_at"`
}

func validateLabel(label kaneoclient.Label, workspaceID string) error {
	if label.Id == "" || label.WorkspaceId.GetOrEmpty() == "" || !label.TaskId.IsSpecified() ||
		(workspaceID != "" && label.WorkspaceId.GetOrEmpty() != workspaceID) {
		return fmt.Errorf("label response contained an invalid label or workspace")
	}
	return nil
}

func workspaceLabels(ctx context.Context, client *kaneoclient.ClientWithResponses, workspaceID string) ([]kaneoclient.Label, error) {
	response, err := client.GetWorkspaceLabelsWithResponse(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list workspace labels: %w", err)
	}
	if response.StatusCode() != 200 {
		return nil, apiResponseError("list workspace labels", response.StatusCode(), response.Body)
	}
	if response.JSON200 == nil || *response.JSON200 == nil {
		return nil, fmt.Errorf("list workspace labels: response did not contain a label list")
	}
	seen := make(map[string]bool)
	for _, label := range *response.JSON200 {
		if err := validateLabel(label, workspaceID); err != nil {
			return nil, err
		}
		if seen[label.Id] {
			return nil, fmt.Errorf("list workspace labels: duplicate label ID")
		}
		seen[label.Id] = true
	}
	return *response.JSON200, nil
}

// Workspace lists include both workspace labels and task copies, without pagination.
// Only a successful list can confirm that an ambiguous 400/404 means deletion.
func getLabel(ctx context.Context, client *kaneoclient.ClientWithResponses, id, workspaceID string) (*kaneoclient.Label, error) {
	response, err := client.GetLabelWithResponse(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get label: %w", err)
	}
	if (response.StatusCode() == 400 || response.StatusCode() == 404) && workspaceID != "" {
		labels, err := workspaceLabels(ctx, client, workspaceID)
		if err != nil {
			return nil, err
		}
		found := false
		for _, label := range labels {
			found = found || label.Id == id
		}
		if !found {
			return nil, nil
		}
	}
	if response.StatusCode() != 200 {
		return nil, apiResponseError("get label", response.StatusCode(), response.Body)
	}
	if response.JSON200 == nil || response.JSON200.Id != id {
		return nil, fmt.Errorf("get label: response did not contain the requested label")
	}
	if err := validateLabel(*response.JSON200, workspaceID); err != nil {
		return nil, err
	}
	return response.JSON200, nil
}

func requireWorkspaceLabel(label kaneoclient.Label) error {
	if !label.TaskId.IsNull() {
		return fmt.Errorf("label %q is task-specific; use a workspace-level label instead", label.Id)
	}
	return nil
}

func labelModelFromAPI(label kaneoclient.Label) labelModel {
	return labelModel{
		ID: types.StringValue(label.Id), WorkspaceID: types.StringValue(label.WorkspaceId.GetOrEmpty()),
		Name: types.StringValue(label.Name), Color: types.StringValue(label.Color),
		CreatedAt: types.StringValue(label.CreatedAt.Format(time.RFC3339Nano)), UpdatedAt: types.StringValue(label.UpdatedAt.Format(time.RFC3339Nano)),
	}
}
