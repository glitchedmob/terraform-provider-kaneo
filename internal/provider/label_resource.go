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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

var (
	_ resource.Resource                = &labelResource{}
	_ resource.ResourceWithConfigure   = &labelResource{}
	_ resource.ResourceWithImportState = &labelResource{}
)

type labelResource struct {
	client *kaneoclient.ClientWithResponses
}

func newLabelResource() resource.Resource { return &labelResource{} }

func (r *labelResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_label"
}

func (r *labelResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a workspace-level Kaneo label. Renaming, recoloring, or deleting it also affects same-name task copies in the workspace, including copies not managed by Terraform.",
		Attributes: map[string]schema.Attribute{
			"id":           schema.StringAttribute{MarkdownDescription: "Workspace label identifier, not a task-copy ID.", Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"workspace_id": schema.StringAttribute{MarkdownDescription: "Workspace identifier. Changing this replaces the label and deletes matching copies in the previous workspace.", Required: true, Validators: []validator.String{stringvalidator.LengthAtLeast(1)}, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"name":         schema.StringAttribute{MarkdownDescription: "Non-empty label name, unique among workspace-level labels in this workspace.", Required: true, Validators: []validator.String{stringvalidator.LengthAtLeast(1)}},
			"color":        schema.StringAttribute{MarkdownDescription: "Non-empty color string, for example `#ef4444`. Passed through to Kaneo without normalization.", Required: true, Validators: []validator.String{stringvalidator.LengthAtLeast(1)}},
			"created_at":   schema.StringAttribute{MarkdownDescription: "Label creation timestamp in RFC3339 format.", Computed: true},
			"updated_at":   schema.StringAttribute{MarkdownDescription: "Label last update timestamp in RFC3339 format.", Computed: true},
		},
	}
}

func (r *labelResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	configureClient(req.ProviderData, &r.client, &resp.Diagnostics)
}

func (r *labelResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan labelModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Kaneo resolves duplicate names to existing rows. Do not silently take ownership.
	labels, err := workspaceLabels(ctx, r.client, plan.WorkspaceID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to Create Label", err.Error())
		return
	}
	for _, label := range labels {
		if label.TaskId.IsNull() && label.Name == plan.Name.ValueString() {
			resp.Diagnostics.AddError("Label Already Exists", fmt.Sprintf("Workspace label %q already exists with ID %q. Import it instead of creating another resource.", label.Name, label.Id))
			return
		}
	}
	response, err := r.client.CreateLabelWithResponse(ctx, kaneoclient.CreateLabelJSONRequestBody{
		WorkspaceId: plan.WorkspaceID.ValueString(), Name: plan.Name.ValueString(), Color: plan.Color.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to Create Label", err.Error())
		return
	}
	if response.StatusCode() != 200 {
		resp.Diagnostics.AddError("Unable to Create Label", apiResponseError("create label", response.StatusCode(), response.Body).Error())
		return
	}
	if response.JSON200 == nil {
		resp.Diagnostics.AddError("Unable to Create Label", "Create label response did not contain a label.")
		return
	}
	if err := validateLabel(*response.JSON200, plan.WorkspaceID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Unable to Create Label", err.Error())
		return
	}
	if err := requireWorkspaceLabel(*response.JSON200); err != nil {
		resp.Diagnostics.AddError("Unable to Create Label", err.Error())
		return
	}
	state := labelModelFromAPI(*response.JSON200)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *labelResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state labelModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	label, err := getLabel(ctx, r.client, state.ID.ValueString(), state.WorkspaceID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to Read Label", err.Error())
		return
	}
	if label == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	if err := requireWorkspaceLabel(*label); err != nil {
		resp.Diagnostics.AddError("Unable to Read Label", err.Error())
		return
	}
	state = labelModelFromAPI(*label)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *labelResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan labelModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	label, err := getLabel(ctx, r.client, plan.ID.ValueString(), plan.WorkspaceID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to Update Label", err.Error())
		return
	}
	if label == nil {
		resp.Diagnostics.AddError("Unable to Update Label", "The label no longer exists. Refresh before retrying.")
		return
	}
	if err := requireWorkspaceLabel(*label); err != nil {
		resp.Diagnostics.AddError("Unable to Update Label", err.Error())
		return
	}
	response, err := r.client.UpdateLabelWithResponse(ctx, plan.ID.ValueString(), kaneoclient.UpdateLabelJSONRequestBody{Name: plan.Name.ValueString(), Color: plan.Color.ValueString()})
	if err != nil {
		resp.Diagnostics.AddError("Unable to Update Label", err.Error())
		return
	}
	if response.StatusCode() != 200 {
		resp.Diagnostics.AddError("Unable to Update Label", apiResponseError("update label", response.StatusCode(), response.Body).Error())
		return
	}
	if response.JSON200 == nil || response.JSON200.Id != plan.ID.ValueString() {
		resp.Diagnostics.AddError("Unable to Update Label", "Update label response did not contain the requested label.")
		return
	}
	if err := validateLabel(*response.JSON200, plan.WorkspaceID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Unable to Update Label", err.Error())
		return
	}
	if err := requireWorkspaceLabel(*response.JSON200); err != nil {
		resp.Diagnostics.AddError("Unable to Update Label", err.Error())
		return
	}
	state := labelModelFromAPI(*response.JSON200)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *labelResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state labelModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	label, err := getLabel(ctx, r.client, state.ID.ValueString(), state.WorkspaceID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to Delete Label", err.Error())
		return
	}
	if label == nil {
		return
	}
	if err := requireWorkspaceLabel(*label); err != nil {
		resp.Diagnostics.AddError("Unable to Delete Label", err.Error())
		return
	}
	response, err := r.client.DeleteLabelWithResponse(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to Delete Label", err.Error())
		return
	}
	if response.StatusCode() == 200 {
		return
	}
	if response.StatusCode() == 400 || response.StatusCode() == 404 {
		label, err := getLabel(ctx, r.client, state.ID.ValueString(), state.WorkspaceID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Unable to Delete Label", err.Error())
			return
		}
		if label == nil {
			return
		}
	}
	resp.Diagnostics.AddError("Unable to Delete Label", apiResponseError("delete label", response.StatusCode(), response.Body).Error())
}

func (r *labelResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if strings.TrimSpace(req.ID) == "" {
		resp.Diagnostics.AddError("Invalid Label Import ID", "Use a non-empty workspace-level label ID.")
		return
	}
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
