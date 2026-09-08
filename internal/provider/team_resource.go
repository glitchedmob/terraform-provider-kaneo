// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"strings"

	kaneoclient "github.com/glitchedmob/terraform-provider-kaneo/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &teamResource{}
	_ resource.ResourceWithConfigure   = &teamResource{}
	_ resource.ResourceWithImportState = &teamResource{}
)

type teamResource struct {
	client *kaneoclient.ClientWithResponses
}
type teamModel struct {
	ID          types.String `tfsdk:"id"`
	WorkspaceID types.String `tfsdk:"workspace_id"`
	Name        types.String `tfsdk:"name"`
}

func newTeamResource() resource.Resource { return &teamResource{} }
func (r *teamResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_team"
}
func (r *teamResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Manages a Kaneo team within a workspace. Does not manage team memberships. Kaneo seeds a default team, limits workspaces to 10 teams, and prohibits deleting the last team.", Attributes: map[string]schema.Attribute{
		"id":           schema.StringAttribute{Computed: true, MarkdownDescription: "Native team identifier.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"workspace_id": schema.StringAttribute{Required: true, MarkdownDescription: "Workspace identifier. Changing it replaces the team.", Validators: []validator.String{stringvalidator.LengthAtLeast(1)}, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"name":         schema.StringAttribute{Required: true, MarkdownDescription: "Team name. Renames preserve the native team ID. Names need not be unique."},
	}}
}
func (r *teamResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	configureClient(req.ProviderData, &r.client, &resp.Diagnostics)
}
func (r *teamResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan teamModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	response, err := r.client.CreateOrganizationTeamWithResponse(ctx, kaneoclient.CreateOrganizationTeamJSONRequestBody{OrganizationId: plan.WorkspaceID.ValueString(), Name: plan.Name.ValueString()})
	if err != nil {
		resp.Diagnostics.AddError("Unable to Create Team", err.Error())
		return
	}
	if response.StatusCode() != 200 {
		resp.Diagnostics.AddError("Unable to Create Team", apiResponseError("create team", response.StatusCode(), response.Body).Error())
		return
	}
	if !validTeam(response.JSON200, plan.WorkspaceID.ValueString(), "") || *response.JSON200.Name != plan.Name.ValueString() {
		resp.Diagnostics.AddError("Unable to Create Team", "Response did not contain the requested team name and workspace. Check the server and import the team if it was created.")
		return
	}
	plan.ID = types.StringValue(response.JSON200.Id)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}
func (r *teamResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state teamModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	team, err := getTeam(ctx, r.client, state.WorkspaceID.ValueString(), state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to Read Team", err.Error())
		return
	}
	if team == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	state.Name = types.StringValue(*team.Name)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
func (r *teamResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state teamModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body := kaneoclient.UpdateOrganizationTeamJSONRequestBody{TeamId: state.ID.ValueString()}
	body.Data.OrganizationId, body.Data.Name = plan.WorkspaceID.ValueString(), plan.Name.ValueString()
	response, err := r.client.UpdateOrganizationTeamWithResponse(ctx, body)
	if err != nil {
		resp.Diagnostics.AddError("Unable to Update Team", err.Error())
		return
	}
	if response.StatusCode() != 200 {
		resp.Diagnostics.AddError("Unable to Update Team", apiResponseError("update team", response.StatusCode(), response.Body).Error())
		return
	}
	if !validTeam(response.JSON200, plan.WorkspaceID.ValueString(), state.ID.ValueString()) || *response.JSON200.Name != plan.Name.ValueString() {
		resp.Diagnostics.AddError("Unable to Update Team", "Response did not contain the requested team identity, workspace, and name.")
		return
	}
	plan.ID = state.ID
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}
func (r *teamResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state teamModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	team, err := getTeam(ctx, r.client, state.WorkspaceID.ValueString(), state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to Delete Team", err.Error())
		return
	}
	if team == nil {
		return
	}
	response, err := r.client.RemoveOrganizationTeamWithResponse(ctx, kaneoclient.RemoveOrganizationTeamJSONRequestBody{OrganizationId: state.WorkspaceID.ValueString(), TeamId: state.ID.ValueString()})
	if err != nil {
		resp.Diagnostics.AddError("Unable to Delete Team", err.Error())
		return
	}
	// A concurrent deletion is safe only after another strict lookup. In
	// particular, neither a 403 nor ORGANIZATION_NOT_FOUND alone proves absence.
	if response.StatusCode() == 403 || (response.StatusCode() == 400 && (authErrorCode(response.Body, "TEAM_NOT_FOUND") || authErrorCode(response.Body, "ORGANIZATION_NOT_FOUND"))) {
		team, err := getTeam(ctx, r.client, state.WorkspaceID.ValueString(), state.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Unable to Delete Team", err.Error())
			return
		}
		if team == nil {
			return
		}
	}
	if response.StatusCode() == 400 && authErrorCode(response.Body, "UNABLE_TO_REMOVE_LAST_TEAM") {
		resp.Diagnostics.AddError("Unable to Delete Team", "Kaneo prohibits deleting the last team. Create another team before retrying. State has been retained.")
		return
	}
	if response.StatusCode() != 200 {
		resp.Diagnostics.AddError("Unable to Delete Team", apiResponseError("remove team", response.StatusCode(), response.Body).Error())
		return
	}
	if response.JSON200 == nil || response.JSON200.Message != "Team removed successfully." {
		resp.Diagnostics.AddError("Unable to Delete Team", "Response did not confirm team deletion.")
	}
}
func (r *teamResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, "/")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		resp.Diagnostics.AddError("Invalid Import Identifier", "Expected workspace_id/team_id using native IDs, not team names.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("workspace_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
}
