// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"strings"
	"time"

	kaneoclient "github.com/glitchedmob/terraform-provider-kaneo/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/oapi-codegen/nullable"
)

type workspaceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Slug        types.String `tfsdk:"slug"`
	Description types.String `tfsdk:"description"`
	Logo        types.String `tfsdk:"logo"`
	CreatedAt   types.String `tfsdk:"created_at"`
}

func listWorkspaces(ctx context.Context, client *kaneoclient.ClientWithResponses) ([]kaneoclient.Workspace, error) {
	response, err := client.ListOrganizationWithResponse(ctx)
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	if response.StatusCode() != 200 {
		return nil, apiResponseError("list workspaces", response.StatusCode(), response.Body)
	}
	if response.JSON200 == nil {
		return nil, fmt.Errorf("list workspaces: response did not contain a workspace list")
	}
	return *response.JSON200, nil
}

func findWorkspace(ctx context.Context, client *kaneoclient.ClientWithResponses, id, slug string) (*kaneoclient.Workspace, error) {
	workspaces, err := listWorkspaces(ctx, client)
	if err != nil {
		return nil, err
	}
	for index := range workspaces {
		workspace := &workspaces[index]
		if id != "" && workspace.Id == id {
			return workspace, nil
		}
		if slug != "" && workspace.Slug == slug {
			return workspace, nil
		}
	}
	return nil, nil
}

func workspaceModelFromAPI(workspace kaneoclient.Workspace) workspaceModel {
	description := nullableStringToTerraform(workspace.Description)
	if description.IsNull() && workspace.Metadata.IsSpecified() && !workspace.Metadata.IsNull() {
		if metadata, err := workspace.Metadata.Get(); err == nil {
			if value, ok := metadata["description"].(string); ok {
				description = types.StringValue(value)
			}
		}
	}

	return workspaceModel{
		ID:          types.StringValue(workspace.Id),
		Name:        types.StringValue(workspace.Name),
		Slug:        types.StringValue(workspace.Slug),
		Description: description,
		Logo:        nullableStringToTerraform(workspace.Logo),
		CreatedAt:   types.StringValue(workspace.CreatedAt.Format(time.RFC3339Nano)),
	}
}

func nullableStringToTerraform(value nullable.Nullable[string]) types.String {
	if !value.IsSpecified() || value.IsNull() {
		return types.StringNull()
	}
	return types.StringValue(value.GetOrEmpty())
}

func nullableStringFromTerraform(value types.String) nullable.Nullable[string] {
	if value.IsNull() {
		return nullable.NewNullNullable[string]()
	}
	return nullable.NewNullableWithValue(value.ValueString())
}

func apiResponseError(operation string, statusCode int, body []byte) error {
	message := strings.TrimSpace(string(body))
	if len(message) > 512 {
		message = message[:512]
	}
	if message == "" {
		return fmt.Errorf("%s returned HTTP %d", operation, statusCode)
	}
	return fmt.Errorf("%s returned HTTP %d: %s", operation, statusCode, message)
}
