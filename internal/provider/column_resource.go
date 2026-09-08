// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"math"
	"strings"

	kaneoclient "github.com/glitchedmob/terraform-provider-kaneo/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

var (
	_ resource.Resource                = &columnResource{}
	_ resource.ResourceWithConfigure   = &columnResource{}
	_ resource.ResourceWithImportState = &columnResource{}
)

type columnResource struct {
	client *kaneoclient.ClientWithResponses
}

func newColumnResource() resource.Resource { return &columnResource{} }

func (r *columnResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_column"
}

func (r *columnResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a Kaneo board column. Import existing default columns rather than recreating them. Columns containing tasks cannot be deleted.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Column identifier.", Computed: true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"project_id": schema.StringAttribute{
				MarkdownDescription: "Project identifier. Changing this replaces the column; tasks are not moved.", Required: true,
				Validators:    []validator.String{stringvalidator.LengthAtLeast(1)},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Column display name. Renaming does not change the slug.", Required: true,
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"slug": schema.StringAttribute{
				MarkdownDescription: "Stable slug derived by Kaneo at creation. Renaming the column leaves it unchanged.", Computed: true,
			},
			"icon": schema.StringAttribute{
				MarkdownDescription: "Column icon name. Omit to clear it. Empty strings are not supported.", Optional: true,
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"color": schema.StringAttribute{
				MarkdownDescription: "Column color. Omit to clear it. Empty strings are not supported.", Optional: true,
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"is_final": schema.BoolAttribute{
				MarkdownDescription: "Whether the column marks tasks as done and stops their overdue reminders. Defaults to false.",
				Optional:            true, Computed: true, Default: booldefault.StaticBool(false),
			},
			"position": schema.Int64Attribute{
				MarkdownDescription: "Absolute board position, from 0 to 2147483647. New columns are appended when omitted. Setting this does not shift other columns.",
				Optional:            true, Computed: true,
				Validators: []validator.Int64{int64validator.Between(0, math.MaxInt32)},
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "Column creation timestamp.", Computed: true,
			},
			"updated_at": schema.StringAttribute{
				MarkdownDescription: "Column last update timestamp.", Computed: true,
			},
		},
	}
}

func (r *columnResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *columnResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan columnModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	response, err := r.client.CreateColumnWithResponse(ctx, plan.ProjectID.ValueString(), kaneoclient.CreateColumnJSONRequestBody{
		Name: plan.Name.ValueString(), Icon: plan.Icon.ValueStringPointer(), Color: plan.Color.ValueStringPointer(), IsFinal: plan.IsFinal.ValueBoolPointer(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to Create Column", err.Error())
		return
	}
	if response.StatusCode() != 200 {
		resp.Diagnostics.AddError("Unable to Create Column", apiResponseError("create column", response.StatusCode(), response.Body).Error())
		return
	}
	if response.JSON200 == nil || response.JSON200.Id == "" || response.JSON200.ProjectId != plan.ProjectID.ValueString() {
		resp.Diagnostics.AddError("Unable to Create Column", "Create column response did not contain a column for the requested project.")
		return
	}
	// Record creation before reordering so a failed second request leaves the ID
	// in state for Terraform to clean up or retry.
	state := columnModelFromAPI(*response.JSON200)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	column, err := setColumnPosition(ctx, r.client, *response.JSON200, plan.Position)
	if err != nil {
		resp.Diagnostics.AddError("Unable to Position Created Column", err.Error())
		return
	}
	state = columnModelFromAPI(*column)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *columnResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state columnModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	column, err := findColumn(ctx, r.client, state.ProjectID.ValueString(), state.ID.ValueString(), "")
	if err != nil {
		resp.Diagnostics.AddError("Unable to Read Column", err.Error())
		return
	}
	if column == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	state = columnModelFromAPI(*column)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *columnResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan columnModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	response, err := r.client.UpdateColumnWithResponse(ctx, plan.ID.ValueString(), kaneoclient.UpdateColumnJSONRequestBody{
		Name: new(plan.Name.ValueString()), Icon: nullableStringFromTerraform(plan.Icon), Color: nullableStringFromTerraform(plan.Color), IsFinal: new(plan.IsFinal.ValueBool()),
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to Update Column", err.Error())
		return
	}
	if response.StatusCode() != 200 {
		resp.Diagnostics.AddError("Unable to Update Column", apiResponseError("update column", response.StatusCode(), response.Body).Error())
		return
	}
	if response.JSON200 == nil || response.JSON200.Id != plan.ID.ValueString() || response.JSON200.ProjectId != plan.ProjectID.ValueString() {
		resp.Diagnostics.AddError("Unable to Update Column", "Update column response did not contain the requested column.")
		return
	}
	state := columnModelFromAPI(*response.JSON200)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	column, err := setColumnPosition(ctx, r.client, *response.JSON200, plan.Position)
	if err != nil {
		resp.Diagnostics.AddError("Unable to Position Updated Column", err.Error())
		return
	}
	state = columnModelFromAPI(*column)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *columnResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state columnModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	response, err := r.client.DeleteColumnWithResponse(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to Delete Column", err.Error())
		return
	}
	if response.StatusCode() == 200 {
		return
	}
	// Missing columns can produce HTTP 400 in workspace-access middleware.
	// Confirm absence with a successful project list, not the status alone.
	if response.StatusCode() == 400 || response.StatusCode() == 404 {
		column, err := findColumn(ctx, r.client, state.ProjectID.ValueString(), state.ID.ValueString(), "")
		if err != nil {
			resp.Diagnostics.AddError("Unable to Delete Column", err.Error())
			return
		}
		if column == nil {
			return
		}
	}
	resp.Diagnostics.AddError("Unable to Delete Column", apiResponseError("delete column", response.StatusCode(), response.Body).Error())
}

func (r *columnResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, "/")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		resp.Diagnostics.AddError("Invalid Column Import ID", "Expected project-id/column-id. Kaneo lists columns by project, so both identifiers are required.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("project_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
}
