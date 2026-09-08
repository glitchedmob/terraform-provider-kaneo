// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	kaneoclient "github.com/glitchedmob/terraform-provider-kaneo/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/mapvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &workspaceRoleResource{}
	_ resource.ResourceWithConfigure   = &workspaceRoleResource{}
	_ resource.ResourceWithImportState = &workspaceRoleResource{}
)

type workspaceRoleResource struct {
	client *kaneoclient.ClientWithResponses
}
type workspaceRoleModel struct {
	ID          types.String `tfsdk:"id"`
	WorkspaceID types.String `tfsdk:"workspace_id"`
	Name        types.String `tfsdk:"name"`
	Permissions types.Map    `tfsdk:"permissions"`
}

func newWorkspaceRoleResource() resource.Resource { return &workspaceRoleResource{} }
func (r *workspaceRoleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace_role"
}
func (r *workspaceRoleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a dynamic workspace role. Requires actual workspace membership and ac permissions; instance admin grants no bypass. Static owner is not managed.",
		Attributes: map[string]schema.Attribute{
			"id":           schema.StringAttribute{Computed: true, MarkdownDescription: "Native role identifier.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"workspace_id": schema.StringAttribute{Required: true, MarkdownDescription: "Workspace identifier. Changing it replaces the role.", Validators: []validator.String{stringvalidator.LengthAtLeast(1)}, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"name":         schema.StringAttribute{Required: true, MarkdownDescription: "Lowercase role name. May be renamed in place. Static owner is reserved.", Validators: []validator.String{stringvalidator.RegexMatches(regexp.MustCompile(`^[^\p{Lu}\p{Lt},\s]+$`), "must be lowercase without whitespace or commas"), stringvalidator.NoneOf("owner")}},
			"permissions":  schema.MapAttribute{Required: true, ElementType: types.SetType{ElemType: types.StringType}, Validators: []validator.Map{mapvalidator.NoNullValues(), mapvalidator.ValueSetsAre(setvalidator.NoNullValues(), setvalidator.ValueStringsAre(stringvalidator.LengthAtLeast(1)))}, MarkdownDescription: "Permission resource names mapped to sets of actions. Replaces the complete permission map. The caller must hold every permission being granted."},
		}}
}
func (r *workspaceRoleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	configureClient(req.ProviderData, &r.client, &resp.Diagnostics)
}
func (m *workspaceRoleModel) fromAPI(ctx context.Context, role kaneoclient.WorkspaceRole) diag.Diagnostics {
	if role.Id == "" || role.OrganizationId != m.WorkspaceID.ValueString() || role.Role == "" || role.Role == "owner" || role.Permission == nil {
		return diag.Diagnostics{diag.NewErrorDiagnostic("Invalid Workspace Role Response", "Response did not contain a dynamic role in the requested workspace.")}
	}
	var diags diag.Diagnostics
	values := make(map[string]attr.Value, len(role.Permission))
	for key, actions := range role.Permission {
		value, d := types.SetValueFrom(ctx, types.StringType, actions)
		diags.Append(d...)
		values[key] = value
	}
	permissions, d := types.MapValue(types.SetType{ElemType: types.StringType}, values)
	diags.Append(d...)
	m.ID = types.StringValue(role.Id)
	m.Name = types.StringValue(role.Role)
	m.Permissions = permissions
	return diags
}
func (m workspaceRoleModel) permissionPayload(ctx context.Context) (map[string][]string, diag.Diagnostics) {
	result := make(map[string][]string)
	var diags diag.Diagnostics
	for key, value := range m.Permissions.Elements() {
		var actions []string
		diags.Append(value.(types.Set).ElementsAs(ctx, &actions, false)...)
		if actions == nil {
			actions = []string{}
		}
		result[key] = actions
	}
	return result, diags
}

func getWorkspaceRole(ctx context.Context, client *kaneoclient.ClientWithResponses, workspaceID, id string) (*kaneoclient.WorkspaceRole, error) {
	response, err := client.GetWorkspaceRoleWithResponse(ctx, &kaneoclient.GetWorkspaceRoleParams{OrganizationId: workspaceID, RoleId: id})
	if err != nil {
		return nil, fmt.Errorf("get workspace role: %w", err)
	}
	// Role lookup checks ac.read before looking for the role. Its explicit
	// ROLE_NOT_FOUND therefore proves absence without hiding permission loss.
	if response.StatusCode() == 400 && authErrorCode(response.Body, "ROLE_NOT_FOUND") {
		return nil, nil
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
		return nil, apiResponseError("get workspace role", response.StatusCode(), response.Body)
	}
	if response.JSON200 == nil || response.JSON200.Id != id || response.JSON200.OrganizationId != workspaceID {
		return nil, fmt.Errorf("get workspace role: response did not contain the requested role")
	}
	return response.JSON200, nil
}
func (r *workspaceRoleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan workspaceRoleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	permissions, diags := plan.permissionPayload(ctx)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	response, err := r.client.CreateWorkspaceRoleWithResponse(ctx, kaneoclient.CreateWorkspaceRoleJSONRequestBody{OrganizationId: plan.WorkspaceID.ValueString(), Role: plan.Name.ValueString(), Permission: permissions})
	if err != nil {
		resp.Diagnostics.AddError("Unable to Create Workspace Role", err.Error())
		return
	}
	if response.StatusCode() != 200 {
		resp.Diagnostics.AddError("Unable to Create Workspace Role", apiResponseError("create workspace role", response.StatusCode(), response.Body).Error())
		return
	}
	if response.JSON200 == nil || !response.JSON200.Success {
		resp.Diagnostics.AddError("Unable to Create Workspace Role", "Response did not confirm creation. Check the server and import the role if it was created.")
		return
	}
	resp.Diagnostics.Append(plan.fromAPI(ctx, response.JSON200.RoleData)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}
func (r *workspaceRoleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state workspaceRoleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	role, err := getWorkspaceRole(ctx, r.client, state.WorkspaceID.ValueString(), state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to Read Workspace Role", err.Error())
		return
	}
	if role == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(state.fromAPI(ctx, *role)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
func (r *workspaceRoleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state workspaceRoleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	permissions, diags := plan.permissionPayload(ctx)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	data := kaneoclient.WorkspaceRoleUpdate{Permission: &permissions}
	// Better Auth rejects even an unchanged name as already taken.
	if !plan.Name.Equal(state.Name) {
		name := plan.Name.ValueString()
		data.RoleName = &name
	}
	response, err := r.client.UpdateWorkspaceRoleWithResponse(ctx, kaneoclient.UpdateWorkspaceRoleJSONRequestBody{OrganizationId: plan.WorkspaceID.ValueString(), RoleId: state.ID.ValueString(), Data: data})
	if err != nil {
		resp.Diagnostics.AddError("Unable to Update Workspace Role", err.Error())
		return
	}
	if response.StatusCode() != 200 {
		resp.Diagnostics.AddError("Unable to Update Workspace Role", apiResponseError("update workspace role", response.StatusCode(), response.Body).Error())
		return
	}
	if response.JSON200 == nil || !response.JSON200.Success || response.JSON200.RoleData.Id != state.ID.ValueString() {
		resp.Diagnostics.AddError("Unable to Update Workspace Role", "Response did not confirm the updated role.")
		return
	}
	resp.Diagnostics.Append(plan.fromAPI(ctx, response.JSON200.RoleData)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}
func (r *workspaceRoleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state workspaceRoleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	role, err := getWorkspaceRole(ctx, r.client, state.WorkspaceID.ValueString(), state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to Delete Workspace Role", err.Error())
		return
	}
	if role == nil {
		return
	}
	if role.Role == "owner" {
		resp.Diagnostics.AddError("Unable to Delete Workspace Role", "Static owner cannot be deleted.")
		return
	}
	response, err := r.client.DeleteWorkspaceRoleWithResponse(ctx, kaneoclient.DeleteWorkspaceRoleJSONRequestBody{OrganizationId: state.WorkspaceID.ValueString(), RoleId: state.ID.ValueString()})
	if err != nil {
		resp.Diagnostics.AddError("Unable to Delete Workspace Role", err.Error())
		return
	}
	if response.StatusCode() == 400 && authErrorCode(response.Body, "ROLE_NOT_FOUND") {
		return
	}
	if response.StatusCode() == 403 {
		present, err := workspacePresent(ctx, r.client, state.WorkspaceID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Unable to Delete Workspace Role", err.Error())
			return
		}
		if !present {
			return
		}
	}
	if response.StatusCode() != 200 {
		resp.Diagnostics.AddError("Unable to Delete Workspace Role", apiResponseError("delete workspace role", response.StatusCode(), response.Body).Error())
		return
	}
	if response.JSON200 == nil || !response.JSON200.Success {
		resp.Diagnostics.AddError("Unable to Delete Workspace Role", "Response did not confirm deletion.")
	}
}
func (r *workspaceRoleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, "/")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		resp.Diagnostics.AddError("Invalid Import Identifier", "Expected workspace_id/role_id using native IDs, not the role name.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("workspace_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
}
