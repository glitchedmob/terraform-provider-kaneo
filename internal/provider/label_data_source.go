// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"

	kaneoclient "github.com/glitchedmob/terraform-provider-kaneo/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

var (
	_ datasource.DataSource              = &labelDataSource{}
	_ datasource.DataSourceWithConfigure = &labelDataSource{}
)

type labelDataSource struct {
	client *kaneoclient.ClientWithResponses
}

func newLabelDataSource() datasource.DataSource { return &labelDataSource{} }

func (d *labelDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_label"
}

func (d *labelDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Retrieves a workspace-level label by ID. Task-specific label copies are not accepted.",
		Attributes: map[string]schema.Attribute{
			"id":           schema.StringAttribute{MarkdownDescription: "Workspace label identifier.", Required: true, Validators: []validator.String{stringvalidator.LengthAtLeast(1)}},
			"workspace_id": schema.StringAttribute{MarkdownDescription: "Workspace identifier.", Computed: true},
			"name":         schema.StringAttribute{MarkdownDescription: "Label name.", Computed: true},
			"color":        schema.StringAttribute{MarkdownDescription: "Label color.", Computed: true},
			"created_at":   schema.StringAttribute{MarkdownDescription: "Label creation timestamp.", Computed: true},
			"updated_at":   schema.StringAttribute{MarkdownDescription: "Label last update timestamp.", Computed: true},
		},
	}
}

func (d *labelDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	configureClient(req.ProviderData, &d.client, &resp.Diagnostics)
}

func (d *labelDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config labelModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	label, err := getLabel(ctx, d.client, config.ID.ValueString(), "")
	if err != nil {
		resp.Diagnostics.AddError("Unable to Read Label", err.Error())
		return
	}
	if err := requireWorkspaceLabel(*label); err != nil {
		resp.Diagnostics.AddError("Unable to Read Label", err.Error())
		return
	}
	state := labelModelFromAPI(*label)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
