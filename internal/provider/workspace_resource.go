// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"

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
	_ resource.Resource                = &workspaceResource{}
	_ resource.ResourceWithConfigure   = &workspaceResource{}
	_ resource.ResourceWithImportState = &workspaceResource{}
)

type workspaceResource struct {
	client *kaneoclient.ClientWithResponses
}

func newWorkspaceResource() resource.Resource {
	return &workspaceResource{}
}

func (r *workspaceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace"
}

func (r *workspaceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a Kaneo workspace.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Workspace identifier.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Workspace name.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"slug": schema.StringAttribute{
				MarkdownDescription: "Workspace slug.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Workspace description.",
				Optional:            true,
			},
			"logo": schema.StringAttribute{
				MarkdownDescription: "Workspace logo URL.",
				Optional:            true,
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "Workspace creation timestamp.",
				Computed:            true,
			},
		},
	}
}

func (r *workspaceResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	configureClient(req.ProviderData, &r.client, &resp.Diagnostics)
}

func (r *workspaceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan workspaceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	response, err := r.client.CreateOrganizationWithResponse(ctx, kaneoclient.CreateOrganizationJSONRequestBody{
		Name:        plan.Name.ValueString(),
		Slug:        plan.Slug.ValueString(),
		Description: nullableStringFromTerraform(plan.Description),
		Logo:        nullableStringFromTerraform(plan.Logo),
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to Create Workspace", fmt.Sprintf("Create workspace request failed: %s", err))
		return
	}
	if response.StatusCode() != 200 {
		resp.Diagnostics.AddError("Unable to Create Workspace", apiResponseError("create workspace", response.StatusCode(), response.Body).Error())
		return
	}
	if response.JSON200 == nil {
		resp.Diagnostics.AddError("Unable to Create Workspace", "Create workspace response did not contain a workspace.")
		return
	}

	state := workspaceModelFromAPI(*response.JSON200)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *workspaceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state workspaceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	workspace, err := findWorkspace(ctx, r.client, state.ID.ValueString(), "")
	if err != nil {
		resp.Diagnostics.AddError("Unable to Read Workspace", err.Error())
		return
	}
	if workspace == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	updatedState := workspaceModelFromAPI(*workspace)
	resp.Diagnostics.Append(resp.State.Set(ctx, &updatedState)...)
}

func (r *workspaceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan workspaceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := kaneoclient.UpdateOrganizationJSONRequestBody{OrganizationId: new(plan.ID.ValueString())}
	body.Data.Name = new(plan.Name.ValueString())
	body.Data.Slug = new(plan.Slug.ValueString())
	body.Data.Description = nullableStringFromTerraform(plan.Description)
	body.Data.Logo = nullableStringFromTerraform(plan.Logo)

	response, err := r.client.UpdateOrganizationWithResponse(ctx, body)
	if err != nil {
		resp.Diagnostics.AddError("Unable to Update Workspace", fmt.Sprintf("Update workspace request failed: %s", err))
		return
	}
	if response.StatusCode() != 200 {
		resp.Diagnostics.AddError("Unable to Update Workspace", apiResponseError("update workspace", response.StatusCode(), response.Body).Error())
		return
	}
	if response.JSON200 == nil {
		resp.Diagnostics.AddError("Unable to Update Workspace", "Update workspace response did not contain a workspace.")
		return
	}

	state := workspaceModelFromAPI(*response.JSON200)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *workspaceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state workspaceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	response, err := r.client.DeleteOrganizationWithResponse(ctx, kaneoclient.DeleteOrganizationJSONRequestBody{
		OrganizationId: state.ID.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to Delete Workspace", fmt.Sprintf("Delete workspace request failed: %s", err))
		return
	}
	if response.StatusCode() != 200 {
		resp.Diagnostics.AddError("Unable to Delete Workspace", apiResponseError("delete workspace", response.StatusCode(), response.Body).Error())
	}
}

func (r *workspaceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
