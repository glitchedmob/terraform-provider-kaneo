// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"time"

	kaneoclient "github.com/glitchedmob/terraform-provider-kaneo/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type projectModel struct {
	ID          types.String `tfsdk:"id"`
	WorkspaceID types.String `tfsdk:"workspace_id"`
	Name        types.String `tfsdk:"name"`
	Slug        types.String `tfsdk:"slug"`
	Icon        types.String `tfsdk:"icon"`
	Description types.String `tfsdk:"description"`
	IsPublic    types.Bool   `tfsdk:"is_public"`
	CreatedAt   types.String `tfsdk:"created_at"`
	ArchivedAt  types.String `tfsdk:"archived_at"`
}

func listProjects(ctx context.Context, client *kaneoclient.ClientWithResponses, workspaceID string) ([]kaneoclient.ProjectListItem, error) {
	response, err := client.ListProjectsWithResponse(ctx, &kaneoclient.ListProjectsParams{
		WorkspaceId: workspaceID, IncludeArchived: new("true"),
	})
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	if response.StatusCode() != 200 {
		return nil, apiResponseError("list projects", response.StatusCode(), response.Body)
	}
	if response.JSON200 == nil || *response.JSON200 == nil {
		return nil, fmt.Errorf("list projects: response did not contain a project list")
	}
	for _, project := range *response.JSON200 {
		if project.Id == "" || project.WorkspaceId != workspaceID {
			return nil, fmt.Errorf("list projects: response contained an invalid project or workspace ID")
		}
	}
	return *response.JSON200, nil
}

// Kaneo uses HTTP 400 for both a missing project and failed workspace lookups.
// Only a successful list that excludes this ID confirms deletion. Authorization
// and server errors must not cause Terraform to forget an existing project.
func projectAbsent(ctx context.Context, client *kaneoclient.ClientWithResponses, id, workspaceID string) (bool, error) {
	if workspaceID == "" {
		return false, nil
	}
	projects, err := listProjects(ctx, client, workspaceID)
	if err != nil {
		return false, err
	}
	for _, project := range projects {
		if project.Id == id {
			return false, nil
		}
	}
	return true, nil
}

func getProject(ctx context.Context, client *kaneoclient.ClientWithResponses, id, workspaceID string) (*kaneoclient.Project, error) {
	response, err := client.GetProjectWithResponse(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get project: %w", err)
	}
	if response.StatusCode() == 400 || response.StatusCode() == 404 {
		absent, err := projectAbsent(ctx, client, id, workspaceID)
		if err != nil {
			return nil, err
		}
		if absent {
			return nil, nil
		}
	}
	if response.StatusCode() != 200 {
		return nil, apiResponseError("get project", response.StatusCode(), response.Body)
	}
	if response.JSON200 == nil || response.JSON200.Id == "" {
		return nil, fmt.Errorf("get project: response did not contain a project")
	}
	if response.JSON200.Id != id || (workspaceID != "" && response.JSON200.WorkspaceId != workspaceID) {
		return nil, fmt.Errorf("get project: response contained an unexpected project or workspace ID")
	}
	return response.JSON200, nil
}

func findProjectBySlug(ctx context.Context, client *kaneoclient.ClientWithResponses, workspaceID, slug string) (*kaneoclient.Project, error) {
	projects, err := listProjects(ctx, client, workspaceID)
	if err != nil {
		return nil, err
	}
	var id string
	for _, project := range projects {
		if project.Slug != slug {
			continue
		}
		if id != "" {
			return nil, fmt.Errorf("multiple projects have slug %q in workspace %q; use the project ID instead", slug, workspaceID)
		}
		id = project.Id
	}
	if id == "" {
		return nil, nil
	}
	return getProject(ctx, client, id, workspaceID)
}

func projectModelFromAPI(project kaneoclient.Project) projectModel {
	archivedAt := types.StringNull()
	if project.ArchivedAt.IsSpecified() && !project.ArchivedAt.IsNull() {
		archivedAt = types.StringValue(project.ArchivedAt.GetOrEmpty().Format(time.RFC3339Nano))
	}
	return projectModel{
		ID: types.StringValue(project.Id), WorkspaceID: types.StringValue(project.WorkspaceId),
		Name: types.StringValue(project.Name), Slug: types.StringValue(project.Slug),
		Icon: types.StringValue(project.Icon.GetOrEmpty()),
		// Updates require a string and boolean, even though responses allow null.
		Description: types.StringValue(project.Description.GetOrEmpty()),
		IsPublic:    types.BoolValue(project.IsPublic.GetOrEmpty()),
		CreatedAt:   types.StringValue(project.CreatedAt.Format(time.RFC3339Nano)), ArchivedAt: archivedAt,
	}
}

func updateProject(ctx context.Context, client *kaneoclient.ClientWithResponses, plan projectModel) (*kaneoclient.Project, error) {
	response, err := client.UpdateProjectWithResponse(ctx, plan.ID.ValueString(), kaneoclient.UpdateProjectJSONRequestBody{
		Name: plan.Name.ValueString(), Slug: plan.Slug.ValueString(), Icon: plan.Icon.ValueString(),
		Description: plan.Description.ValueString(), IsPublic: plan.IsPublic.ValueBool(),
	})
	if err != nil {
		return nil, fmt.Errorf("update project: %w", err)
	}
	if response.StatusCode() != 200 {
		return nil, apiResponseError("update project", response.StatusCode(), response.Body)
	}
	if response.JSON200 == nil || response.JSON200.Id == "" {
		return nil, fmt.Errorf("update project: response did not contain a project")
	}
	return response.JSON200, nil
}
