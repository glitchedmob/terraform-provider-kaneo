// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"

	kaneoclient "github.com/glitchedmob/terraform-provider-kaneo/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/datasourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
)

var (
	_ datasource.DataSource                     = &workspaceDataSource{}
	_ datasource.DataSourceWithConfigure        = &workspaceDataSource{}
	_ datasource.DataSourceWithConfigValidators = &workspaceDataSource{}
)

type workspaceDataSource struct {
	client *kaneoclient.ClientWithResponses
}

func newWorkspaceDataSource() datasource.DataSource {
	return &workspaceDataSource{}
}

func (d *workspaceDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace"
}

func (d *workspaceDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Retrieves a Kaneo workspace by ID or slug.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Workspace identifier. Specify either `id` or `slug`.",
				Optional:            true,
				Computed:            true,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Workspace name.",
				Computed:            true,
			},
			"slug": schema.StringAttribute{
				MarkdownDescription: "Workspace slug. Specify either `id` or `slug`.",
				Optional:            true,
				Computed:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Workspace description.",
				Computed:            true,
			},
			"logo": schema.StringAttribute{
				MarkdownDescription: "Workspace logo URL.",
				Computed:            true,
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "Workspace creation timestamp.",
				Computed:            true,
			},
		},
	}
}

func (d *workspaceDataSource) ConfigValidators(context.Context) []datasource.ConfigValidator {
	return []datasource.ConfigValidator{
		datasourcevalidator.ExactlyOneOf(path.MatchRoot("id"), path.MatchRoot("slug")),
	}
}

func (d *workspaceDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*kaneoclient.ClientWithResponses)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Provider Data Type",
			fmt.Sprintf("Expected *client.ClientWithResponses, got %T.", req.ProviderData),
		)
		return
	}
	d.client = client
}

func (d *workspaceDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config workspaceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	workspace, err := findWorkspace(ctx, d.client, config.ID.ValueString(), config.Slug.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to Read Workspace", err.Error())
		return
	}
	if workspace == nil {
		lookup := fmt.Sprintf("ID %q", config.ID.ValueString())
		if config.ID.IsNull() {
			lookup = fmt.Sprintf("slug %q", config.Slug.ValueString())
		}
		resp.Diagnostics.AddError("Workspace Not Found", fmt.Sprintf("No workspace was found with %s.", lookup))
		return
	}

	state := workspaceModelFromAPI(*workspace)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
