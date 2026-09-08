// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"strings"

	kaneoclient "github.com/glitchedmob/terraform-provider-kaneo/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &userResource{}
	_ resource.ResourceWithConfigure   = &userResource{}
	_ resource.ResourceWithImportState = &userResource{}
)

type userResource struct {
	client *kaneoclient.ClientWithResponses
}
type userModel struct {
	ID            types.String `tfsdk:"id"`
	Email         types.String `tfsdk:"email"`
	Name          types.String `tfsdk:"name"`
	Role          types.String `tfsdk:"role"`
	EmailVerified types.Bool   `tfsdk:"email_verified"`
	Password      types.String `tfsdk:"password"`
}

func newUserResource() resource.Resource { return &userResource{} }
func (r *userResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user"
}
func (r *userResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a Kaneo instance user. Requires an instance admin. Does not create workspace memberships.",
		Attributes: map[string]schema.Attribute{
			"id":             schema.StringAttribute{Computed: true, MarkdownDescription: "User identifier.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"email":          schema.StringAttribute{Required: true, MarkdownDescription: "User email. Kaneo stores email in lowercase; equivalent casing is preserved in state.", Validators: []validator.String{stringvalidator.LengthAtLeast(1)}},
			"name":           schema.StringAttribute{Required: true, MarkdownDescription: "Display name.", Validators: []validator.String{stringvalidator.LengthAtLeast(1)}},
			"role":           schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("user"), MarkdownDescription: "Instance role: user or admin. Defaults to user.", Validators: []validator.String{stringvalidator.OneOf("user", "admin")}},
			"email_verified": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false), MarkdownDescription: "Whether the email is verified. Defaults to false. Set true only for identities you have verified; this can enable OIDC account linking depending on server settings."},
			"password":       schema.StringAttribute{Optional: true, Sensitive: true, MarkdownDescription: "Credential password, stored in Terraform state. Omit to create without password login. Changes set a new password. Cannot be read or imported; removing this attribute does not remove the remote password.", Validators: []validator.String{stringvalidator.LengthBetween(8, 128)}},
		},
	}
}
func (r *userResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*kaneoclient.ClientWithResponses)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Provider Data Type", fmt.Sprintf("Expected *client.ClientWithResponses, got %T.", req.ProviderData))
		return
	}
	r.client = client
}

// Admin responses and transport errors may contain credentials. Never include them in diagnostics.
func userRequestError(operation string, status int, err error) error {
	if err != nil {
		return fmt.Errorf("%s request failed; check connectivity and server logs", operation)
	}
	return fmt.Errorf("%s returned HTTP %d", operation, status)
}
func (m *userModel) fromAPI(user kaneoclient.AdminUser) {
	m.ID = types.StringValue(user.Id)
	if m.Email.IsNull() || m.Email.IsUnknown() || !strings.EqualFold(m.Email.ValueString(), user.Email) {
		m.Email = types.StringValue(user.Email)
	}
	m.Name = types.StringValue(user.Name)
	m.Role = types.StringValue(user.Role)
	m.EmailVerified = types.BoolValue(user.EmailVerified)
}
func (r *userResource) setPassword(ctx context.Context, id string, password types.String) error {
	if password.IsNull() {
		return nil
	}
	response, err := r.client.SetAdminUserPasswordWithResponse(ctx, kaneoclient.SetAdminUserPasswordJSONRequestBody{UserId: id, NewPassword: password.ValueString()})
	if err != nil {
		return userRequestError("set user password", 0, err)
	}
	if response.StatusCode() != 200 {
		return userRequestError("set user password", response.StatusCode(), nil)
	}
	if response.JSON200 == nil || !response.JSON200.Status {
		return fmt.Errorf("set user password response did not confirm success")
	}
	return nil
}
func (r *userResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan userModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body := kaneoclient.CreateAdminUserJSONRequestBody{Email: strings.ToLower(plan.Email.ValueString()), Name: plan.Name.ValueString(), Role: plan.Role.ValueString()}
	body.Data.EmailVerified = plan.EmailVerified.ValueBool()
	response, err := r.client.CreateAdminUserWithResponse(ctx, body)
	if err != nil {
		resp.Diagnostics.AddError("Unable to Create User", userRequestError("create user", 0, err).Error())
		return
	}
	if response.StatusCode() != 200 {
		resp.Diagnostics.AddError("Unable to Create User", userRequestError("create user", response.StatusCode(), nil).Error())
		return
	}
	if response.JSON200 == nil || response.JSON200.User.Id == "" {
		resp.Diagnostics.AddError("Unable to Create User", "Create response did not contain a user ID. Check the server before retrying; import the user if creation succeeded.")
		return
	}
	password := plan.Password
	plan.fromAPI(response.JSON200.User)
	// Persist the created ID before the separate password operation can fail.
	plan.Password = types.StringNull()
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.setPassword(ctx, plan.ID.ValueString(), password); err != nil {
		resp.Diagnostics.AddError("Unable to Set User Password", err.Error())
		return
	}
	plan.Password = password
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}
func (r *userResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state userModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	response, err := r.client.GetAdminUserWithResponse(ctx, &kaneoclient.GetAdminUserParams{Id: state.ID.ValueString()})
	if err != nil {
		resp.Diagnostics.AddError("Unable to Read User", userRequestError("get user", 0, err).Error())
		return
	}
	if response.StatusCode() == 404 {
		resp.State.RemoveResource(ctx)
		return
	}
	if response.StatusCode() != 200 {
		resp.Diagnostics.AddError("Unable to Read User", userRequestError("get user", response.StatusCode(), nil).Error())
		return
	}
	if response.JSON200 == nil || response.JSON200.Id == "" {
		resp.Diagnostics.AddError("Unable to Read User", "Get response did not contain a user ID.")
		return
	}
	state.fromAPI(*response.JSON200)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
func (r *userResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state userModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	response, err := r.client.UpdateAdminUserWithResponse(ctx, kaneoclient.UpdateAdminUserJSONRequestBody{UserId: plan.ID.ValueString(), Data: kaneoclient.AdminUserData{Email: strings.ToLower(plan.Email.ValueString()), Name: plan.Name.ValueString(), Role: plan.Role.ValueString(), EmailVerified: plan.EmailVerified.ValueBool()}})
	if err != nil {
		resp.Diagnostics.AddError("Unable to Update User", userRequestError("update user", 0, err).Error())
		return
	}
	if response.StatusCode() != 200 {
		resp.Diagnostics.AddError("Unable to Update User", userRequestError("update user", response.StatusCode(), nil).Error())
		return
	}
	if response.JSON200 == nil || response.JSON200.Id == "" {
		resp.Diagnostics.AddError("Unable to Update User", "Update response did not contain a user ID.")
		return
	}
	password := plan.Password
	plan.fromAPI(*response.JSON200)
	plan.Password = state.Password
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !password.Equal(state.Password) {
		if err := r.setPassword(ctx, plan.ID.ValueString(), password); err != nil {
			resp.Diagnostics.AddError("Unable to Set User Password", err.Error())
			return
		}
	}
	plan.Password = password
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}
func (r *userResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state userModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	response, err := r.client.RemoveAdminUserWithResponse(ctx, kaneoclient.RemoveAdminUserJSONRequestBody{UserId: state.ID.ValueString()})
	if err != nil {
		resp.Diagnostics.AddError("Unable to Delete User", userRequestError("remove user", 0, err).Error())
		return
	}
	if response.StatusCode() == 404 {
		return
	}
	if response.StatusCode() != 200 {
		resp.Diagnostics.AddError("Unable to Delete User", userRequestError("remove user", response.StatusCode(), nil).Error())
		return
	}
	if response.JSON200 == nil || !response.JSON200.Success {
		resp.Diagnostics.AddError("Unable to Delete User", "Remove response did not confirm success.")
	}
}
func (r *userResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
