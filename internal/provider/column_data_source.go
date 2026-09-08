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
	_ datasource.DataSource                     = &columnDataSource{}
	_ datasource.DataSourceWithConfigure        = &columnDataSource{}
	_ datasource.DataSourceWithConfigValidators = &columnDataSource{}
)

type columnDataSource struct {
	client *kaneoclient.ClientWithResponses
}

func newColumnDataSource() datasource.DataSource { return &columnDataSource{} }

func (d *columnDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_column"
}

func (d *columnDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Retrieves a Kaneo board column by project ID and either column ID or slug, including automatically created default columns.",
		Attributes: map[string]schema.Attribute{
			"project_id": schema.StringAttribute{
				MarkdownDescription: "Project identifier. Required for both lookup forms.", Required: true,
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"id": schema.StringAttribute{
				MarkdownDescription: "Column identifier. Specify either `id` or `slug`.", Optional: true, Computed: true,
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"slug": schema.StringAttribute{
				MarkdownDescription: "Column slug. Specify either `id` or `slug`. A column's slug does not change when it is renamed.", Optional: true, Computed: true,
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Column display name.", Computed: true,
			},
			"icon": schema.StringAttribute{
				MarkdownDescription: "Column icon name, or null if unset.", Computed: true,
			},
			"color": schema.StringAttribute{
				MarkdownDescription: "Column color, or null if unset.", Computed: true,
			},
			"is_final": schema.BoolAttribute{
				MarkdownDescription: "Whether the column marks tasks as done and stops their overdue reminders.", Computed: true,
			},
			"position": schema.Int64Attribute{
				MarkdownDescription: "Absolute board position.", Computed: true,
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

func (d *columnDataSource) ConfigValidators(context.Context) []datasource.ConfigValidator {
	return []datasource.ConfigValidator{
		datasourcevalidator.ExactlyOneOf(path.MatchRoot("id"), path.MatchRoot("slug")),
	}
}

func (d *columnDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	configureClient(req.ProviderData, &d.client, &resp.Diagnostics)
}

func (d *columnDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config columnModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	column, err := findColumn(ctx, d.client, config.ProjectID.ValueString(), config.ID.ValueString(), config.Slug.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to Read Column", err.Error())
		return
	}
	if column == nil {
		lookup := fmt.Sprintf("ID %q", config.ID.ValueString())
		if config.ID.IsNull() {
			lookup = fmt.Sprintf("slug %q", config.Slug.ValueString())
		}
		resp.Diagnostics.AddError("Column Not Found", fmt.Sprintf("No column was found with %s in project %q.", lookup, config.ProjectID.ValueString()))
		return
	}
	state := columnModelFromAPI(*column)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
