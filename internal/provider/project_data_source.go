// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"

	kaneoclient "github.com/glitchedmob/terraform-provider-kaneo/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/datasourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

var (
	_ datasource.DataSource                     = &projectDataSource{}
	_ datasource.DataSourceWithConfigure        = &projectDataSource{}
	_ datasource.DataSourceWithConfigValidators = &projectDataSource{}
)

type projectDataSource struct {
	client *kaneoclient.ClientWithResponses
}

func newProjectDataSource() datasource.DataSource { return &projectDataSource{} }

func (d *projectDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project"
}

func (d *projectDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Retrieves a Kaneo project by ID or by workspace ID and slug, including archived projects. Ambiguous slug matches are rejected.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Project identifier. Specify either `id` alone or `workspace_id` and `slug` together.",
				Optional:            true,
				Computed:            true,
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"workspace_id": schema.StringAttribute{
				MarkdownDescription: "Workspace identifier. Required with `slug`; omit when looking up by `id`.",
				Optional:            true,
				Computed:            true,
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"slug": schema.StringAttribute{
				MarkdownDescription: "Project slug. Required with `workspace_id`; omit when looking up by `id`.",
				Optional:            true,
				Computed:            true,
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Project name.",
				Computed:            true,
			},
			"icon": schema.StringAttribute{
				MarkdownDescription: "Project icon name. Null API values are returned as an empty string.",
				Computed:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Project description. Null API values are returned as an empty string.",
				Computed:            true,
			},
			"is_public": schema.BoolAttribute{
				MarkdownDescription: "Whether the project board is readable without signing in. Null API values are returned as false.",
				Computed:            true,
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "Project creation timestamp.",
				Computed:            true,
			},
			"archived_at": schema.StringAttribute{
				MarkdownDescription: "Project archive timestamp, or null if not archived.",
				Computed:            true,
			},
		},
	}
}

func (d *projectDataSource) ConfigValidators(context.Context) []datasource.ConfigValidator {
	return []datasource.ConfigValidator{
		datasourcevalidator.ExactlyOneOf(path.MatchRoot("id"), path.MatchRoot("slug")),
		datasourcevalidator.RequiredTogether(path.MatchRoot("workspace_id"), path.MatchRoot("slug")),
	}
}

func (d *projectDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	configureClient(req.ProviderData, &d.client, &resp.Diagnostics)
}

func (d *projectDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config projectModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var project *kaneoclient.Project
	var err error
	if config.ID.ValueString() != "" {
		project, err = getProject(ctx, d.client, config.ID.ValueString(), "")
	} else {
		project, err = findProjectBySlug(ctx, d.client, config.WorkspaceID.ValueString(), config.Slug.ValueString())
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to Read Project", err.Error())
		return
	}
	if project == nil {
		resp.Diagnostics.AddError("Project Not Found", fmt.Sprintf("No project was found with slug %q in workspace %q.", config.Slug.ValueString(), config.WorkspaceID.ValueString()))
		return
	}
	state := projectModelFromAPI(*project)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
