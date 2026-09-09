// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"regexp"

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
	_ resource.Resource                = &teamMemberResource{}
	_ resource.ResourceWithConfigure   = &teamMemberResource{}
	_ resource.ResourceWithImportState = &teamMemberResource{}
)

type teamMemberResource struct {
	client *kaneoclient.ClientWithResponses
}
type teamMemberModel struct {
	ID          types.String `tfsdk:"id"`
	WorkspaceID types.String `tfsdk:"workspace_id"`
	TeamID      types.String `tfsdk:"team_id"`
	UserID      types.String `tfsdk:"user_id"`
}

func newTeamMemberResource() resource.Resource { return &teamMemberResource{} }
func (r *teamMemberResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_team_member"
}
func (r *teamMemberResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	attrs := map[string]schema.Attribute{"id": schema.StringAttribute{Computed: true, MarkdownDescription: "Logical workspace_id/team_id/user_id, with each native ID URL path-escaped.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}}}
	for _, name := range []string{"workspace_id", "team_id", "user_id"} {
		attrs[name] = schema.StringAttribute{Required: true, MarkdownDescription: fmt.Sprintf("Native %s. Changing it replaces the relationship.", name), Validators: []validator.String{stringvalidator.LengthAtLeast(1), stringvalidator.RegexMatches(regexp.MustCompile(`^[^\s\p{Z}\x{0085}\x{000B}]+$`), "must not contain whitespace")}, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}}
	}
	resp.Schema = schema.Schema{MarkdownDescription: "Manages one team membership. The target must already be an accepted workspace member. Manage the operator's own membership explicitly to bootstrap new teams, and use depends_on for other members. Existing relationships require import.", Attributes: attrs}
}
func (r *teamMemberResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	configureClient(req.ProviderData, &r.client, &resp.Diagnostics)
}
func (r *teamMemberResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan teamMemberModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ws, team, user := plan.WorkspaceID.ValueString(), plan.TeamID.ValueString(), plan.UserID.ValueString()
	id := teamMemberID(ws, team, user)
	parent, present, err := observeTeamMember(ctx, r.client, ws, team, user)
	if err != nil {
		resp.Diagnostics.AddError("Unable to Create Team Member", err.Error())
		return
	}
	if !parent {
		resp.Diagnostics.AddError("Unable to Create Team Member", "Team does not exist in workspace_id, or the workspace is absent.")
		return
	}
	if present {
		resp.Diagnostics.AddError("Team Membership Already Exists", "Import the existing relationship using "+id+" before managing it.")
		return
	}
	response, err := r.client.AddOrganizationTeamMemberWithResponse(ctx, kaneoclient.AddOrganizationTeamMemberJSONRequestBody{OrganizationId: ws, TeamId: team, UserId: user})
	if err != nil {
		resp.Diagnostics.AddError("Unable to Create Team Member", fmt.Sprintf("Add failed: %v. Inspect the relationship and import %s if created.", err, id))
		return
	}
	if response.StatusCode() != 200 {
		resp.Diagnostics.AddError("Unable to Create Team Member", teamMemberMutationError("add team member", response.StatusCode(), response.Body).Error())
		return
	}
	if response.JSON200 == nil || !validTeamMemberID(response.JSON200.Id) || response.JSON200.TeamId != team || response.JSON200.UserId != user {
		resp.Diagnostics.AddError("Unable to Create Team Member", "Invalid add response identity. Inspect the relationship and import "+id+" if created.")
		return
	}
	plan.ID = types.StringValue(id)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	_, present, err = observeTeamMember(ctx, r.client, ws, team, user)
	if err != nil {
		resp.Diagnostics.AddError("Unable to Confirm Team Member", err.Error())
		return
	}
	if !present {
		resp.Diagnostics.AddError("Unable to Confirm Team Member", "The added relationship disappeared before confirmation. State retained for reconciliation.")
	}
}
func (r *teamMemberResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state teamMemberModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	_, present, err := observeTeamMember(ctx, r.client, state.WorkspaceID.ValueString(), state.TeamID.ValueString(), state.UserID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to Read Team Member", err.Error())
		return
	}
	if !present {
		resp.State.RemoveResource(ctx)
		return
	}
	state.ID = types.StringValue(teamMemberID(state.WorkspaceID.ValueString(), state.TeamID.ValueString(), state.UserID.ValueString()))
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
func (r *teamMemberResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Unexpected Team Member Update", "Team membership identifiers require replacement.")
}
func (r *teamMemberResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state teamMemberModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ws, team, user := state.WorkspaceID.ValueString(), state.TeamID.ValueString(), state.UserID.ValueString()
	_, present, err := observeTeamMember(ctx, r.client, ws, team, user)
	if err != nil {
		resp.Diagnostics.AddError("Unable to Delete Team Member", err.Error())
		return
	}
	if !present {
		return
	}
	response, mutationErr := r.client.RemoveOrganizationTeamMemberWithResponse(ctx, kaneoclient.RemoveOrganizationTeamMemberJSONRequestBody{OrganizationId: ws, TeamId: team, UserId: user})
	if mutationErr == nil && response.StatusCode() == 200 && response.JSON200 != nil && response.JSON200.Message == "Team member removed successfully." {
		return
	}
	// A failed/malformed response is not absence. Reconcile once, including transport
	// failures. This also preserves orphan rows that remove refuses to delete.
	_, present, err = observeTeamMember(ctx, r.client, ws, team, user)
	if err == nil && !present {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to Delete Team Member", err.Error())
		return
	}
	if mutationErr != nil {
		resp.Diagnostics.AddError("Unable to Delete Team Member", mutationErr.Error())
		return
	}
	if response.StatusCode() == 200 {
		resp.Diagnostics.AddError("Unable to Delete Team Member", "Response did not confirm exact team member removal, and the relationship remains. State retained.")
		return
	}
	resp.Diagnostics.AddError("Unable to Delete Team Member", teamMemberMutationError("remove team member", response.StatusCode(), response.Body).Error())
}
func (r *teamMemberResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	ws, team, user, err := parseTeamMemberID(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid Import Identifier", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("workspace_id"), ws)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("team_id"), team)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("user_id"), user)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
