// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"

	kaneoclient "github.com/glitchedmob/terraform-provider-kaneo/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

var (
	_ datasource.DataSource              = &taskDataSource{}
	_ datasource.DataSourceWithConfigure = &taskDataSource{}
)

type taskDataSource struct {
	client *kaneoclient.ClientWithResponses
}

func newTaskDataSource() datasource.DataSource { return &taskDataSource{} }

func (d *taskDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_task"
}

func (d *taskDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Retrieves a Kaneo task by its task ID, including planned and archived tasks.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Task identifier, not the displayed project-slug/number identifier.", Required: true,
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"project_id":  schema.StringAttribute{MarkdownDescription: "Project identifier.", Computed: true},
			"title":       schema.StringAttribute{MarkdownDescription: "Task title.", Computed: true},
			"description": schema.StringAttribute{MarkdownDescription: "Task description. Null API values are returned as an empty string.", Computed: true},
			"status":      schema.StringAttribute{MarkdownDescription: "Column slug, or the virtual status planned or archived.", Computed: true},
			"priority":    schema.StringAttribute{MarkdownDescription: "Task priority.", Computed: true},
			"assignee_id": schema.StringAttribute{MarkdownDescription: "Assignee user ID, or null if unassigned.", Computed: true},
			"start_date":  schema.StringAttribute{MarkdownDescription: "Start timestamp in RFC3339 format, or null if unset.", Computed: true},
			"due_date":    schema.StringAttribute{MarkdownDescription: "Due timestamp in RFC3339 format, or null if unset.", Computed: true},
			"number":      schema.Int64Attribute{MarkdownDescription: "API-assigned per-project task number.", Computed: true},
			"position":    schema.Int64Attribute{MarkdownDescription: "Order within the column.", Computed: true},
			"created_at":  schema.StringAttribute{MarkdownDescription: "Task creation timestamp.", Computed: true},
		},
	}
}

func (d *taskDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*kaneoclient.ClientWithResponses)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Provider Data Type", fmt.Sprintf("Expected *client.ClientWithResponses, got %T.", req.ProviderData))
		return
	}
	d.client = client
}

func (d *taskDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config taskModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	task, err := getTask(ctx, d.client, config.ID.ValueString(), "")
	if err != nil {
		resp.Diagnostics.AddError("Unable to Read Task", err.Error())
		return
	}
	state := taskModelFromAPI(*task, taskModel{})
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
