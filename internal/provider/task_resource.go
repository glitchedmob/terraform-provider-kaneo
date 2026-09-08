// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	kaneoclient "github.com/glitchedmob/terraform-provider-kaneo/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_               resource.Resource                   = &taskResource{}
	_               resource.ResourceWithConfigure      = &taskResource{}
	_               resource.ResourceWithImportState    = &taskResource{}
	_               resource.ResourceWithValidateConfig = &taskResource{}
	taskDatePattern                                     = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d{1,3})?(Z|[+-]([01]\d|2[0-3]):[0-5]\d)$`)
)

type taskResource struct {
	client *kaneoclient.ClientWithResponses
}

func newTaskResource() resource.Resource { return &taskResource{} }

func (r *taskResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_task"
}

func (r *taskResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a Kaneo task. Deleting or replacing a task permanently deletes its contents, including comments and attachments.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Task identifier.", Computed: true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"project_id": schema.StringAttribute{
				MarkdownDescription: "Project identifier. Changing this replaces the task rather than moving it.", Required: true,
				Validators:    []validator.String{stringvalidator.LengthAtLeast(1)},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"title": schema.StringAttribute{
				MarkdownDescription: "Task title.", Required: true,
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Task description. Defaults to an empty string. Omit to clear it.",
				Optional:            true, Computed: true, Default: stringdefault.StaticString(""),
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "Column slug, or the virtual status planned or archived. Defaults to to-do.",
				Optional:            true, Computed: true, Default: stringdefault.StaticString("to-do"),
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"priority": schema.StringAttribute{
				MarkdownDescription: "Task priority. Defaults to no-priority.",
				Optional:            true, Computed: true, Default: stringdefault.StaticString("no-priority"),
				Validators: []validator.String{stringvalidator.OneOf("no-priority", "low", "medium", "high", "urgent")},
			},
			"assignee_id": schema.StringAttribute{
				MarkdownDescription: "Assignable user ID in the project's workspace. Omit to unassign.", Optional: true,
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"start_date": schema.StringAttribute{
				MarkdownDescription: "Start timestamp in RFC3339 format with at most millisecond precision. Omit to clear it.", Optional: true,
			},
			"due_date": schema.StringAttribute{
				MarkdownDescription: "Due timestamp in RFC3339 format with at most millisecond precision. Must not precede start_date. Omit to clear it.", Optional: true,
			},
			"number": schema.Int64Attribute{
				MarkdownDescription: "API-assigned per-project task number, displayed after the project slug.", Computed: true,
			},
			"position": schema.Int64Attribute{
				MarkdownDescription: "API-assigned order within the column. Updates preserve the refreshed position; ordering is not managed.", Computed: true,
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "Task creation timestamp.", Computed: true,
			},
		},
	}
}

func (r *taskResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *taskResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config taskModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !config.AssigneeID.IsNull() && !config.AssigneeID.IsUnknown() && strings.TrimSpace(config.AssigneeID.ValueString()) != config.AssigneeID.ValueString() {
		resp.Diagnostics.AddAttributeError(path.Root("assignee_id"), "Invalid Assignee ID", "Assignee IDs must not contain leading or trailing whitespace.")
	}
	dates := make(map[string]time.Time)
	for name, value := range map[string]types.String{"start_date": config.StartDate, "due_date": config.DueDate} {
		if value.IsNull() || value.IsUnknown() {
			continue
		}
		date, err := time.Parse(time.RFC3339Nano, value.ValueString())
		if err != nil || !taskDatePattern.MatchString(value.ValueString()) {
			resp.Diagnostics.AddAttributeError(path.Root(name), "Invalid Task Date", "Use an RFC3339 timestamp with a timezone and at most three fractional second digits, for example 2026-09-01T12:00:00.123Z.")
			continue
		}
		dates[name] = date
	}
	start, hasStart := dates["start_date"]
	due, hasDue := dates["due_date"]
	if hasStart && hasDue && start.After(due) {
		resp.Diagnostics.AddAttributeError(path.Root("due_date"), "Invalid Task Date Range", "due_date must not precede start_date.")
	}
}

func (r *taskResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan taskModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	response, err := r.client.CreateTaskWithResponse(ctx, plan.ProjectID.ValueString(), kaneoclient.CreateTaskJSONRequestBody{
		Title: plan.Title.ValueString(), Description: plan.Description.ValueString(), Status: plan.Status.ValueString(),
		Priority: kaneoclient.CreateTaskJSONBodyPriority(plan.Priority.ValueString()), UserId: plan.AssigneeID.ValueStringPointer(),
		StartDate: plan.StartDate.ValueStringPointer(), DueDate: plan.DueDate.ValueStringPointer(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to Create Task", err.Error())
		return
	}
	if response.StatusCode() != 200 {
		resp.Diagnostics.AddError("Unable to Create Task", apiResponseError("create task", response.StatusCode(), response.Body).Error())
		return
	}
	if response.JSON200 == nil || response.JSON200.Id == "" || response.JSON200.ProjectId != plan.ProjectID.ValueString() {
		resp.Diagnostics.AddError("Unable to Create Task", "Create task response did not contain a task in the requested project.")
		return
	}
	state := taskModelFromAPI(*response.JSON200, plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *taskResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state taskModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	task, err := getTask(ctx, r.client, state.ID.ValueString(), state.ProjectID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to Read Task", err.Error())
		return
	}
	if task == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	state = taskModelFromAPI(*task, state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *taskResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, prior taskModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// The full update requires a position even though Terraform does not manage it.
	// Use refreshed state, not the unknown computed value in the plan.
	if prior.Position.IsNull() || prior.Position.IsUnknown() {
		resp.Diagnostics.AddError("Unable to Update Task", "The task has no known position. Kaneo requires a position for updates; set one in Kaneo and refresh before retrying.")
		return
	}
	response, err := r.client.UpdateTaskWithResponse(ctx, plan.ID.ValueString(), kaneoclient.UpdateTaskJSONRequestBody{
		ProjectId: plan.ProjectID.ValueString(), Title: plan.Title.ValueString(), Description: plan.Description.ValueString(),
		Status: plan.Status.ValueString(), Priority: kaneoclient.UpdateTaskJSONBodyPriority(plan.Priority.ValueString()),
		Position: int32(prior.Position.ValueInt64()), UserId: plan.AssigneeID.ValueStringPointer(),
		// Omission clears these fields in Kaneo's full update endpoint.
		StartDate: plan.StartDate.ValueStringPointer(), DueDate: plan.DueDate.ValueStringPointer(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to Update Task", err.Error())
		return
	}
	if response.StatusCode() != 200 {
		resp.Diagnostics.AddError("Unable to Update Task", apiResponseError("update task", response.StatusCode(), response.Body).Error())
		return
	}
	if response.JSON200 == nil || response.JSON200.Id != plan.ID.ValueString() || response.JSON200.ProjectId != plan.ProjectID.ValueString() {
		resp.Diagnostics.AddError("Unable to Update Task", "Update task response did not contain the requested task and project.")
		return
	}
	state := taskModelFromAPI(*response.JSON200, plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *taskResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state taskModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	response, err := r.client.DeleteTaskWithResponse(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to Delete Task", err.Error())
		return
	}
	if response.StatusCode() == 200 {
		return
	}
	if response.StatusCode() == 400 || response.StatusCode() == 404 {
		absent, err := taskAbsent(ctx, r.client, state.ID.ValueString(), state.ProjectID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Unable to Delete Task", err.Error())
			return
		}
		if absent {
			return
		}
	}
	resp.Diagnostics.AddError("Unable to Delete Task", apiResponseError("delete task", response.StatusCode(), response.Body).Error())
}

func (r *taskResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if strings.TrimSpace(req.ID) == "" {
		resp.Diagnostics.AddError("Invalid Task Import ID", "Use a non-empty task ID, not the displayed project-slug/number identifier.")
		return
	}
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
