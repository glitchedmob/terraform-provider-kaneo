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
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &taskLabelResource{}
	_ resource.ResourceWithConfigure   = &taskLabelResource{}
	_ resource.ResourceWithImportState = &taskLabelResource{}
)

type taskLabelModel struct {
	ID          types.String `tfsdk:"id"`
	LabelID     types.String `tfsdk:"label_id"`
	TaskID      types.String `tfsdk:"task_id"`
	WorkspaceID types.String `tfsdk:"workspace_id"`
}

type taskLabelResource struct {
	client *kaneoclient.ClientWithResponses
}

func newTaskLabelResource() resource.Resource { return &taskLabelResource{} }

func (r *taskLabelResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_task_label"
}

func (r *taskLabelResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Attaches a workspace-level label to a task. Kaneo creates a task-specific copy with a different ID. Destroying this resource removes only that copy, not the workspace label, task, or unrelated task labels.",
		Attributes: map[string]schema.Attribute{
			"id":           schema.StringAttribute{MarkdownDescription: "Task-label copy identifier returned by Kaneo. Different from label_id.", Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"label_id":     schema.StringAttribute{MarkdownDescription: "Non-empty source workspace-level label ID. Task-specific copies are rejected because Kaneo's attach endpoint can move them away from another task. Changing this replaces the attachment.", Required: true, Validators: []validator.String{stringvalidator.LengthAtLeast(1)}, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"task_id":      schema.StringAttribute{MarkdownDescription: "Non-empty task ID in the same workspace as the source label. Changing this replaces the attachment.", Required: true, Validators: []validator.String{stringvalidator.LengthAtLeast(1)}, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"workspace_id": schema.StringAttribute{MarkdownDescription: "Workspace identifier shared by the task and source label.", Computed: true},
		},
	}
}

func (r *taskLabelResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	configureClient(req.ProviderData, &r.client, &resp.Diagnostics)
}

func taskLabelModelFromAPI(label kaneoclient.Label, sourceID types.String) taskLabelModel {
	return taskLabelModel{
		ID: types.StringValue(label.Id), LabelID: sourceID, TaskID: types.StringValue(label.TaskId.GetOrEmpty()),
		WorkspaceID: types.StringValue(label.WorkspaceId.GetOrEmpty()),
	}
}

func (r *taskLabelResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan taskLabelModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	source, err := getLabel(ctx, r.client, plan.LabelID.ValueString(), "")
	if err != nil {
		resp.Diagnostics.AddError("Unable to Attach Label", err.Error())
		return
	}
	// Passing a task copy to this endpoint can delete it from its previous task.
	if err := requireWorkspaceLabel(*source); err != nil {
		resp.Diagnostics.AddError("Unable to Attach Label", err.Error())
		return
	}
	labels, err := r.client.GetTaskLabelsWithResponse(ctx, plan.TaskID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to Attach Label", err.Error())
		return
	}
	if labels.StatusCode() != 200 {
		resp.Diagnostics.AddError("Unable to Attach Label", apiResponseError("list task labels", labels.StatusCode(), labels.Body).Error())
		return
	}
	if labels.JSON200 == nil || *labels.JSON200 == nil {
		resp.Diagnostics.AddError("Unable to Attach Label", "List task labels response did not contain a label list.")
		return
	}
	for _, label := range *labels.JSON200 {
		if err := validateLabel(label, source.WorkspaceId.GetOrEmpty()); err != nil {
			resp.Diagnostics.AddError("Unable to Attach Label", err.Error())
			return
		}
		if label.TaskId.GetOrEmpty() != plan.TaskID.ValueString() {
			resp.Diagnostics.AddError("Unable to Attach Label", "List task labels response contained a label for another task.")
			return
		}
		if label.Name == source.Name {
			resp.Diagnostics.AddError("Task Label Already Exists", fmt.Sprintf("Task label %q already exists with ID %q. Import using %s/%s instead of taking ownership implicitly.", label.Name, label.Id, source.Id, label.Id))
			return
		}
	}
	response, err := r.client.AttachLabelToTaskWithResponse(ctx, source.Id, kaneoclient.AttachLabelToTaskJSONRequestBody{TaskId: plan.TaskID.ValueString()})
	if err != nil {
		resp.Diagnostics.AddError("Unable to Attach Label", err.Error())
		return
	}
	if response.StatusCode() != 200 {
		resp.Diagnostics.AddError("Unable to Attach Label", apiResponseError("attach label", response.StatusCode(), response.Body).Error())
		return
	}
	if response.JSON200 == nil || response.JSON200.Id == source.Id || response.JSON200.TaskId.GetOrEmpty() != plan.TaskID.ValueString() {
		resp.Diagnostics.AddError("Unable to Attach Label", "Attach label response did not contain a separate label copy for the requested task.")
		return
	}
	if err := validateLabel(*response.JSON200, source.WorkspaceId.GetOrEmpty()); err != nil {
		resp.Diagnostics.AddError("Unable to Attach Label", err.Error())
		return
	}
	state := taskLabelModelFromAPI(*response.JSON200, plan.LabelID)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *taskLabelResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state taskLabelModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	label, err := getLabel(ctx, r.client, state.ID.ValueString(), state.WorkspaceID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to Read Task Label", err.Error())
		return
	}
	if label == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	if state.TaskID.IsNull() {
		// Import supplies the source ID because Kaneo stores no source-to-copy link.
		if label.TaskId.GetOrEmpty() == "" {
			resp.Diagnostics.AddError("Invalid Task Label Import", "The imported ID must identify a task-label copy, not a workspace label.")
			return
		}
		source, err := getLabel(ctx, r.client, state.LabelID.ValueString(), "")
		if err != nil {
			resp.Diagnostics.AddError("Invalid Task Label Import", err.Error())
			return
		}
		if err := requireWorkspaceLabel(*source); err != nil {
			resp.Diagnostics.AddError("Invalid Task Label Import", err.Error())
			return
		}
		if source.WorkspaceId.GetOrEmpty() != label.WorkspaceId.GetOrEmpty() {
			resp.Diagnostics.AddError("Invalid Task Label Import", "The source label and task copy must belong to the same workspace.")
			return
		}
	} else if label.TaskId.GetOrEmpty() != state.TaskID.ValueString() {
		// Do not take ownership of a workspace label or a copy moved to another task.
		resp.State.RemoveResource(ctx)
		return
	}
	state = taskLabelModelFromAPI(*label, state.LabelID)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *taskLabelResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Unexpected Task Label Update", "Task and source-label changes must replace the attachment.")
}

func (r *taskLabelResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state taskLabelModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	label, err := getLabel(ctx, r.client, state.ID.ValueString(), state.WorkspaceID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to Detach Label", err.Error())
		return
	}
	if label == nil || label.TaskId.GetOrEmpty() != state.TaskID.ValueString() {
		return
	}
	response, err := r.client.DetachLabelFromTaskWithResponse(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to Detach Label", err.Error())
		return
	}
	if response.StatusCode() == 200 {
		return
	}
	if response.StatusCode() == 400 || response.StatusCode() == 404 {
		label, err := getLabel(ctx, r.client, state.ID.ValueString(), state.WorkspaceID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Unable to Detach Label", err.Error())
			return
		}
		if label == nil || label.TaskId.GetOrEmpty() != state.TaskID.ValueString() {
			return
		}
	}
	resp.Diagnostics.AddError("Unable to Detach Label", apiResponseError("detach label", response.StatusCode(), response.Body).Error())
}

func (r *taskLabelResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, "/")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" || parts[0] == parts[1] {
		resp.Diagnostics.AddError("Invalid Task Label Import ID", "Use source-label-id/task-label-id. Kaneo stores task copies separately without a source-label foreign key.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("label_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
}
