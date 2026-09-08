// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"time"

	kaneoclient "github.com/glitchedmob/terraform-provider-kaneo/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type columnModel struct {
	ID        types.String `tfsdk:"id"`
	ProjectID types.String `tfsdk:"project_id"`
	Name      types.String `tfsdk:"name"`
	Slug      types.String `tfsdk:"slug"`
	Icon      types.String `tfsdk:"icon"`
	Color     types.String `tfsdk:"color"`
	IsFinal   types.Bool   `tfsdk:"is_final"`
	Position  types.Int64  `tfsdk:"position"`
	CreatedAt types.String `tfsdk:"created_at"`
	UpdatedAt types.String `tfsdk:"updated_at"`
}

func columnFromList(columns []kaneoclient.Column, projectID, id, slug string) (*kaneoclient.Column, error) {
	if columns == nil {
		return nil, fmt.Errorf("column list response was null")
	}
	var match *kaneoclient.Column
	for i := range columns {
		column := &columns[i]
		if column.Id == "" || column.ProjectId != projectID {
			return nil, fmt.Errorf("column list contained an invalid column or project ID")
		}
		if (id != "" && column.Id == id) || (id == "" && column.Slug == slug) {
			if match != nil {
				return nil, fmt.Errorf("multiple columns matched in project %q; use a unique column ID", projectID)
			}
			match = column
		}
	}
	return match, nil
}

func findColumn(ctx context.Context, client *kaneoclient.ClientWithResponses, projectID, id, slug string) (*kaneoclient.Column, error) {
	response, err := client.GetColumnsWithResponse(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("get columns: %w", err)
	}
	if response.StatusCode() != 200 {
		return nil, apiResponseError("get columns", response.StatusCode(), response.Body)
	}
	if response.JSON200 == nil {
		return nil, fmt.Errorf("get columns: response did not contain a column list")
	}
	return columnFromList(*response.JSON200, projectID, id, slug)
}

func columnModelFromAPI(column kaneoclient.Column) columnModel {
	return columnModel{
		ID: types.StringValue(column.Id), ProjectID: types.StringValue(column.ProjectId),
		Name: types.StringValue(column.Name), Slug: types.StringValue(column.Slug),
		Icon: nullableStringToTerraform(column.Icon), Color: nullableStringToTerraform(column.Color),
		IsFinal: types.BoolValue(column.IsFinal), Position: types.Int64Value(int64(column.Position)),
		CreatedAt: types.StringValue(column.CreatedAt.Format(time.RFC3339Nano)),
		UpdatedAt: types.StringValue(column.UpdatedAt.Format(time.RFC3339Nano)),
	}
}

func setColumnPosition(ctx context.Context, client *kaneoclient.ClientWithResponses, column kaneoclient.Column, position types.Int64) (*kaneoclient.Column, error) {
	if position.IsNull() || position.IsUnknown() || position.ValueInt64() == int64(column.Position) {
		return &column, nil
	}
	// The endpoint updates only the submitted columns. Sending one ID avoids
	// overwriting the positions of columns managed elsewhere.
	response, err := client.ReorderColumnsWithResponse(ctx, column.ProjectId, kaneoclient.ReorderColumnsJSONRequestBody{
		Columns: []struct {
			Id       string `json:"id"`
			Position int32  `json:"position"`
		}{{Id: column.Id, Position: int32(position.ValueInt64())}},
	})
	if err != nil {
		return nil, fmt.Errorf("reorder column: %w", err)
	}
	if response.StatusCode() != 200 {
		return nil, apiResponseError("reorder column", response.StatusCode(), response.Body)
	}
	if response.JSON200 == nil {
		return nil, fmt.Errorf("reorder column: response did not contain a column list")
	}
	updated, err := columnFromList(*response.JSON200, column.ProjectId, column.Id, "")
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return nil, fmt.Errorf("reorder column: response did not contain column %q", column.Id)
	}
	return updated, nil
}
