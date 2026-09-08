// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"

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
)

var (
	_ resource.Resource                = &projectResource{}
	_ resource.ResourceWithConfigure   = &projectResource{}
	_ resource.ResourceWithImportState = &projectResource{}
)

type projectResource struct {
	client *kaneoclient.ClientWithResponses
}

func newProjectResource() resource.Resource { return &projectResource{} }

func (r *projectResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project"
}

func (r *projectResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a Kaneo project. Deleting a project permanently deletes its tasks and other contents.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Project identifier.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"workspace_id": schema.StringAttribute{
				MarkdownDescription: "Workspace identifier. Changing this replaces the project rather than moving it and deletes its old contents without copying them.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Project name. Must not be empty.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"slug": schema.StringAttribute{
				MarkdownDescription: "Prefix used in task identifiers, for example `PLAT` in `PLAT-12`. Must not be empty.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"icon": schema.StringAttribute{
				MarkdownDescription: "Project icon name. Defaults to Layout.",
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("Layout"),
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Project description. Defaults to an empty string. Removing this argument clears the description.",
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString(""),
			},
			"is_public": schema.BoolAttribute{
				MarkdownDescription: "Whether the project board is readable without signing in. Defaults to false.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "Project creation timestamp.",
				Computed:            true,
			},
			"archived_at": schema.StringAttribute{
				MarkdownDescription: "Project archive timestamp, or null if not archived. This resource reads archived projects but does not manage archiving or sidebar order.",
				Computed:            true,
			},
		},
	}
}

func (r *projectResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	configureClient(req.ProviderData, &r.client, &resp.Diagnostics)
}

func (r *projectResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan projectModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	response, err := r.client.CreateProjectWithResponse(ctx, kaneoclient.CreateProjectJSONRequestBody{
		WorkspaceId: plan.WorkspaceID.ValueString(), Name: plan.Name.ValueString(), Slug: plan.Slug.ValueString(), Icon: plan.Icon.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to Create Project", err.Error())
		return
	}
	if response.StatusCode() != 200 {
		resp.Diagnostics.AddError("Unable to Create Project", apiResponseError("create project", response.StatusCode(), response.Body).Error())
		return
	}
	if response.JSON200 == nil || response.JSON200.Id == "" {
		resp.Diagnostics.AddError("Unable to Create Project", "Create project response did not contain a project.")
		return
	}

	// Save the ID before the second request so a failed update does not orphan
	// the project. Kaneo's create endpoint cannot set description or visibility.
	state := projectModelFromAPI(*response.JSON200)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if plan.Description.ValueString() != state.Description.ValueString() || plan.IsPublic.ValueBool() != state.IsPublic.ValueBool() {
		plan.ID = state.ID
		project, err := updateProject(ctx, r.client, plan)
		if err != nil {
			resp.Diagnostics.AddError("Unable to Configure Created Project", err.Error())
			return
		}
		state = projectModelFromAPI(*project)
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	}
}

func (r *projectResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state projectModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	project, err := getProject(ctx, r.client, state.ID.ValueString(), state.WorkspaceID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to Read Project", err.Error())
		return
	}
	if project == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	state = projectModelFromAPI(*project)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *projectResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan projectModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	project, err := updateProject(ctx, r.client, plan)
	if err != nil {
		resp.Diagnostics.AddError("Unable to Update Project", err.Error())
		return
	}
	state := projectModelFromAPI(*project)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *projectResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state projectModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	response, err := r.client.DeleteProjectWithResponse(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to Delete Project", err.Error())
		return
	}
	if response.StatusCode() == 200 {
		return
	}
	if response.StatusCode() == 400 || response.StatusCode() == 404 {
		absent, err := projectAbsent(ctx, r.client, state.ID.ValueString(), state.WorkspaceID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Unable to Delete Project", err.Error())
			return
		}
		if absent {
			return
		}
	}
	resp.Diagnostics.AddError("Unable to Delete Project", apiResponseError("delete project", response.StatusCode(), response.Body).Error())
}

func (r *projectResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
