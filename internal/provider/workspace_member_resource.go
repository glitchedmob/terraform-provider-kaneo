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
	_ resource.Resource                = &workspaceMemberResource{}
	_ resource.ResourceWithConfigure   = &workspaceMemberResource{}
	_ resource.ResourceWithImportState = &workspaceMemberResource{}
)

type workspaceMemberResource struct {
	client *kaneoclient.ClientWithResponses
}
type workspaceMemberModel struct {
	ID           types.String `tfsdk:"id"`
	WorkspaceID  types.String `tfsdk:"workspace_id"`
	Email        types.String `tfsdk:"email"`
	Role         types.String `tfsdk:"role"`
	Status       types.String `tfsdk:"status"`
	MemberID     types.String `tfsdk:"member_id"`
	InvitationID types.String `tfsdk:"invitation_id"`
}

func newWorkspaceMemberResource() resource.Resource { return &workspaceMemberResource{} }
func (r *workspaceMemberResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace_member"
}
func (r *workspaceMemberResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Manages a pending workspace invitation and its accepted membership. Create returns without waiting for recipient acceptance. Requires workspace membership and invitation/member permissions.", Attributes: map[string]schema.Attribute{
		"id":            schema.StringAttribute{Computed: true, MarkdownDescription: "Stable `workspace_id/email` identity, with each part URL path-escaped.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"workspace_id":  schema.StringAttribute{Required: true, MarkdownDescription: "Workspace ID. Changes require replacement.", Validators: []validator.String{stringvalidator.RegexMatches(regexp.MustCompile(`^\S+$`), "must be nonempty without whitespace")}, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"email":         schema.StringAttribute{Required: true, MarkdownDescription: "Canonical lowercase email without whitespace or a display name. Changes replace the resource. Mixed-case configuration is rejected, not silently rewritten. If referencing a mixed-case `kaneo_user.email`, use `lower(kaneo_user.recipient.email)` explicitly.", Validators: []validator.String{memberEmailValidator{}}, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"role":          schema.StringAttribute{Required: true, MarkdownDescription: "Single, nonempty role name without whitespace or commas. Supports custom names and built-in roles. Kaneo validates role existence. Multi-role assignments are rejected during discovery and import rather than selecting one role or silently overwriting the others.", Validators: []validator.String{stringvalidator.RegexMatches(regexp.MustCompile(`^[^,\s]+$`), "must be a nonempty role name without commas or whitespace")}},
		"status":        schema.StringAttribute{Computed: true, MarkdownDescription: "Observed pending or accepted status."},
		"member_id":     schema.StringAttribute{Computed: true, MarkdownDescription: "Accepted native member ID, or null while pending."},
		"invitation_id": schema.StringAttribute{Computed: true, MarkdownDescription: "Live pending invitation ID, or null when none is outstanding. A pending invitation can coexist with a member; destroy cleans up both."},
	}}
}

type memberEmailValidator struct{}

func (memberEmailValidator) Description(context.Context) string {
	return "must be a canonical lowercase email without display name or whitespace"
}
func (v memberEmailValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}
func (v memberEmailValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if !req.ConfigValue.IsNull() && !req.ConfigValue.IsUnknown() && !validMemberEmail(req.ConfigValue.ValueString()) {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid Email", v.Description(ctx))
	}
}
func (r *workspaceMemberResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	configureClient(req.ProviderData, &r.client, &resp.Diagnostics)
}
func (m *workspaceMemberModel) observe(o memberObservation) {
	m.ID = types.StringValue(memberIdentity(m.WorkspaceID.ValueString(), m.Email.ValueString()))
	m.MemberID = types.StringNull()
	m.InvitationID = types.StringNull()
	if o.live != nil {
		m.InvitationID = types.StringValue(o.live.Id)
		m.Role = types.StringValue(invitationRole(*o.live))
		m.Status = types.StringValue("pending")
	}
	if o.member != nil {
		m.MemberID = types.StringValue(o.member.Id)
		m.Role = types.StringValue(o.member.Role)
		m.Status = types.StringValue("accepted")
	}
}
func (r *workspaceMemberResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan workspaceMemberModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ws, email := plan.WorkspaceID.ValueString(), plan.Email.ValueString()
	o, err := observeWorkspaceMember(ctx, r.client, ws, email)
	if err == nil && o.missingWorkspace {
		err = fmt.Errorf("workspace does not exist")
	}
	// Accepted history alone is not access. Recheck immediately before creating
	// after external removal; the API still cannot exclude an in-flight acceptance.
	for attempt := 0; err == nil && o.absent() && o.acceptanceGap("") && attempt < 2; attempt++ {
		o, err = observeWorkspaceMember(ctx, r.client, ws, email)
	}
	if err == nil && !o.absent() {
		err = fmt.Errorf("membership or pending invitation already exists; inspect and import %s instead of creating", memberIdentity(ws, email))
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to Create Workspace Member", err.Error())
		return
	}
	i, err := inviteWorkspaceMember(ctx, r.client, ws, email, plan.Role.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to Create Workspace Member", err.Error())
		return
	}
	plan.observe(memberObservation{live: i})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}
func (r *workspaceMemberResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state workspaceMemberModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	o, err := observeWorkspaceMember(ctx, r.client, state.WorkspaceID.ValueString(), state.Email.ValueString())
	if err == nil && o.absent() && o.acceptanceGap(state.InvitationID.ValueString()) && state.MemberID.ValueString() == "" {
		// An acceptance status can precede the member insert. Only immediate reads,
		// never polling or sleeping for the recipient.
		for attempt := 0; attempt < 2 && o.absent() && err == nil; attempt++ {
			o, err = observeWorkspaceMember(ctx, r.client, state.WorkspaceID.ValueString(), state.Email.ValueString())
		}
		if err == nil && o.absent() && o.acceptanceGap(state.InvitationID.ValueString()) {
			err = fmt.Errorf("accepted invitation has no member yet; refresh after the external acceptance operation finishes")
		}
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to Read Workspace Member", err.Error())
		return
	}
	if o.absent() {
		resp.State.RemoveResource(ctx)
		return
	}
	state.observe(o)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
func (r *workspaceMemberResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state workspaceMemberModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	o, err := reconcileWorkspaceMember(ctx, r.client, plan.WorkspaceID.ValueString(), plan.Email.ValueString(), plan.Role.ValueString(), false, state.MemberID.ValueString() != "", state.InvitationID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to Update Workspace Member", err.Error()+" Earlier cancellations or member changes may have completed; refresh before retrying.")
		return
	}
	plan.observe(o)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}
func (r *workspaceMemberResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state workspaceMemberModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	_, err := reconcileWorkspaceMember(ctx, r.client, state.WorkspaceID.ValueString(), state.Email.ValueString(), "", true, state.MemberID.ValueString() != "", state.InvitationID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to Delete Workspace Member", err.Error()+" Earlier cleanup may have completed; state is retained. Refresh before retrying.")
	}
}
func (r *workspaceMemberResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	ws, email, err := parseMemberIdentity(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid Import Identifier", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("workspace_id"), ws)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("email"), email)...)
}
