// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const columnFixture = `{"id":"column-1","projectId":"project-1","name":"Testing","slug":"testing","position":4,"icon":null,"color":null,"isFinal":false,"createdAt":"2026-09-01T12:00:00Z","updatedAt":"2026-09-01T12:00:00Z"}`

func columnTestPlan(t *testing.T, r *columnResource, model columnModel) tfsdk.Plan {
	t.Helper()
	var response resource.SchemaResponse
	r.Schema(t.Context(), resource.SchemaRequest{}, &response)
	plan := tfsdk.Plan{Schema: response.Schema}
	if diags := plan.Set(t.Context(), model); diags.HasError() {
		t.Fatal(diags)
	}
	return plan
}

func columnTestConfig(t *testing.T, d *columnDataSource, model columnModel) tfsdk.Config {
	t.Helper()
	var response datasource.SchemaResponse
	d.Schema(t.Context(), datasource.SchemaRequest{}, &response)
	state := tfsdk.State{Schema: response.Schema}
	if diags := state.Set(t.Context(), model); diags.HasError() {
		t.Fatal(diags)
	}
	return tfsdk.Config{Schema: response.Schema, Raw: state.Raw}
}

func columnTestModel() columnModel {
	return columnModel{ProjectID: types.StringValue("project-1"), Name: types.StringValue("Testing"), IsFinal: types.BoolValue(false)}
}

func TestColumnAttributeValidators(t *testing.T) {
	var response resource.SchemaResponse
	(&columnResource{}).Schema(t.Context(), resource.SchemaRequest{}, &response)
	position := response.Schema.Attributes["position"].(resourceschema.Int64Attribute)
	for _, value := range []int64{-1, 0, math.MaxInt32, math.MaxInt32 + 1} {
		var result validator.Int64Response
		position.Validators[0].ValidateInt64(t.Context(), validator.Int64Request{ConfigValue: types.Int64Value(value)}, &result)
		if result.Diagnostics.HasError() != (value < 0 || value > math.MaxInt32) {
			t.Fatalf("position %d: %v", value, result.Diagnostics)
		}
	}
	for _, field := range []string{"icon", "color"} {
		attribute := response.Schema.Attributes[field].(resourceschema.StringAttribute)
		for _, value := range []types.String{types.StringNull(), types.StringValue(""), types.StringValue("value")} {
			var result validator.StringResponse
			attribute.Validators[0].ValidateString(t.Context(), validator.StringRequest{ConfigValue: value}, &result)
			if result.Diagnostics.HasError() != (!value.IsNull() && value.ValueString() == "") {
				t.Fatalf("%s %s: %v", field, value, result.Diagnostics)
			}
		}
	}
}

func TestColumnResourceLifecycle(t *testing.T) {
	var remote map[string]any
	if err := json.Unmarshal([]byte(columnFixture), &remote); err != nil {
		t.Fatal(err)
	}
	var calls []string
	client := projectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch r.Method + " " + r.URL.Path {
		case "POST /column/project-1":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body["name"] != "Testing" || body["isFinal"] != false || len(body) != 2 {
				t.Errorf("unexpected create body: %v", body)
			}
		case "PUT /column/column-1":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			for _, field := range []string{"name", "icon", "color", "isFinal"} {
				value, ok := body[field]
				if !ok {
					t.Errorf("update omitted %s", field)
				}
				remote[field] = value
			}
		case "PUT /column/reorder/project-1":
			var body struct {
				Columns []struct {
					ID       string `json:"id"`
					Position int32  `json:"position"`
				} `json:"columns"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if len(body.Columns) != 1 || body.Columns[0].ID != "column-1" {
				t.Errorf("reorder must update only the managed column: %+v", body)
				w.WriteHeader(500)
				return
			}
			remote["position"] = body.Columns[0].Position
			json.NewEncoder(w).Encode([]any{remote})
			return
		case "GET /column/project-1":
			json.NewEncoder(w).Encode([]any{remote})
			return
		case "DELETE /column/column-1":
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		json.NewEncoder(w).Encode(remote)
	})
	r := &columnResource{client: client}
	model := columnTestModel()
	// This value cannot round-trip through float32; the OpenAPI overlay must
	// keep positions as integers in both request and response types.
	model.Position = types.Int64Value(16777217)
	plan := columnTestPlan(t, r, model)
	created := resource.CreateResponse{State: tfsdk.State{Schema: plan.Schema}}
	r.Create(t.Context(), resource.CreateRequest{Plan: plan}, &created)
	if created.Diagnostics.HasError() {
		t.Fatal(created.Diagnostics)
	}
	var state columnModel
	if diags := created.State.Get(t.Context(), &state); diags.HasError() {
		t.Fatal(diags)
	}
	if state.ID.ValueString() != "column-1" || state.Position != model.Position || !state.Icon.IsNull() || !state.Color.IsNull() {
		t.Fatalf("unexpected create state: %+v", state)
	}
	if strings.Join(calls, ",") != "POST /column/project-1,PUT /column/reorder/project-1" {
		t.Fatalf("unexpected create calls: %v", calls)
	}

	for _, clear := range []bool{false, true} {
		state.Name = types.StringValue("Renamed")
		state.Icon = types.StringValue("Check")
		state.Color = types.StringValue("#abcdef")
		state.IsFinal = types.BoolValue(true)
		state.Position = types.Int64Value(8)
		if clear {
			state.Icon = types.StringNull()
			state.Color = types.StringNull()
			state.IsFinal = types.BoolValue(false)
		}
		update := resource.UpdateResponse{State: created.State}
		r.Update(t.Context(), resource.UpdateRequest{Plan: columnTestPlan(t, r, state)}, &update)
		if update.Diagnostics.HasError() {
			t.Fatal(update.Diagnostics)
		}
		var got columnModel
		if diags := update.State.Get(t.Context(), &got); diags.HasError() {
			t.Fatal(diags)
		}
		if got != state {
			t.Fatalf("updated state = %+v, want %+v", got, state)
		}
		if got.Slug.ValueString() != "testing" {
			t.Fatal("rename must preserve slug")
		}
		if clear && (remote["icon"] != nil || remote["color"] != nil || remote["isFinal"] != false) {
			t.Fatalf("clear did not send null and false: %v", remote)
		}
		created.State = update.State
	}
	imported := resource.ImportStateResponse{State: tfsdk.State{Schema: plan.Schema}}
	if diags := imported.State.Set(t.Context(), columnModel{}); diags.HasError() {
		t.Fatal(diags)
	}
	r.ImportState(t.Context(), resource.ImportStateRequest{ID: "project-1/column-1"}, &imported)
	if imported.Diagnostics.HasError() {
		t.Fatal(imported.Diagnostics)
	}
	read := resource.ReadResponse{State: imported.State}
	r.Read(t.Context(), resource.ReadRequest{State: imported.State}, &read)
	if read.Diagnostics.HasError() {
		t.Fatal(read.Diagnostics)
	}
	var got columnModel
	if diags := read.State.Get(t.Context(), &got); diags.HasError() {
		t.Fatal(diags)
	}
	if got != state {
		t.Fatalf("import state = %+v, want %+v", got, state)
	}
	deleted := resource.DeleteResponse{State: read.State}
	r.Delete(t.Context(), resource.DeleteRequest{State: read.State}, &deleted)
	if deleted.Diagnostics.HasError() {
		t.Fatal(deleted.Diagnostics)
	}
}

func TestColumnPartialReorderFailure(t *testing.T) {
	for _, create := range []bool{true, false} {
		t.Run(fmt.Sprintf("create=%t", create), func(t *testing.T) {
			client := projectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "/reorder/") {
					w.WriteHeader(403)
					fmt.Fprint(w, `{"message":"Forbidden"}`)
					return
				}
				fmt.Fprint(w, columnFixture)
			})
			r := &columnResource{client: client}
			model := columnTestModel()
			model.ID = types.StringValue("column-1")
			model.Position = types.Int64Value(10)
			plan := columnTestPlan(t, r, model)
			state := tfsdk.State{Schema: plan.Schema}
			if create {
				response := resource.CreateResponse{State: state}
				r.Create(t.Context(), resource.CreateRequest{Plan: plan}, &response)
				if !response.Diagnostics.HasError() {
					t.Fatal("expected reorder failure")
				}
				state = response.State
			} else {
				response := resource.UpdateResponse{State: state}
				r.Update(t.Context(), resource.UpdateRequest{Plan: plan}, &response)
				if !response.Diagnostics.HasError() {
					t.Fatal("expected reorder failure")
				}
				state = response.State
			}
			var got columnModel
			if diags := state.Get(t.Context(), &got); diags.HasError() {
				t.Fatal(diags)
			}
			if got.ID.ValueString() != "column-1" || got.Position.ValueInt64() != 4 {
				t.Fatalf("partial state not preserved: %+v", got)
			}
		})
	}
}

func TestColumnReadAndDeleteErrors(t *testing.T) {
	for _, tc := range []struct {
		name                           string
		getStatus, deleteStatus        int
		body                           string
		readError, deleteError, absent bool
	}{
		{"missing400", 200, 400, `[]`, false, false, true},
		{"missing404", 200, 404, `[]`, false, false, true},
		{"lookupFailureNotDeletion", 200, 400, `[` + columnFixture + `]`, false, true, false},
		{"containsTasks", 200, 409, `[` + columnFixture + `]`, false, true, false},
		{"forbidden", 403, 403, `{}`, true, true, false},
		{"serverError", 500, 500, `{}`, true, true, false},
		{"missingProject", 400, 400, `{}`, true, true, false},
		{"nullList", 200, 400, `null`, true, true, false},
		{"invalidJSON", 200, 404, `{`, true, true, false},
		{"invalidColumn", 200, 400, `[{}]`, true, true, false},
		{"wrongProject", 200, 400, `[` + strings.Replace(columnFixture, "project-1", "project-2", 1) + `]`, true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := projectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					w.WriteHeader(tc.getStatus)
					fmt.Fprint(w, tc.body)
					return
				}
				w.WriteHeader(tc.deleteStatus)
				fmt.Fprint(w, `{"message":"Cannot delete column"}`)
			})
			r := &columnResource{client: client}
			model := columnTestModel()
			model.ID = types.StringValue("column-1")
			plan := columnTestPlan(t, r, model)
			state := tfsdk.State{Schema: plan.Schema, Raw: plan.Raw}
			read := resource.ReadResponse{State: state}
			r.Read(t.Context(), resource.ReadRequest{State: state}, &read)
			if read.Diagnostics.HasError() != tc.readError || read.State.Raw.IsNull() != tc.absent {
				t.Fatalf("read removed=%t, diagnostics=%v", read.State.Raw.IsNull(), read.Diagnostics)
			}
			deleted := resource.DeleteResponse{State: state}
			r.Delete(t.Context(), resource.DeleteRequest{State: state}, &deleted)
			if deleted.Diagnostics.HasError() != tc.deleteError {
				t.Fatalf("delete diagnostics: %v", deleted.Diagnostics)
			}
		})
	}
}

func TestColumnDataSourceLookups(t *testing.T) {
	for _, byID := range []bool{false, true} {
		t.Run(fmt.Sprintf("byID=%t", byID), func(t *testing.T) {
			client := projectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/column/project-1" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				fmt.Fprintf(w, "[%s]", columnFixture)
			})
			d := &columnDataSource{client: client}
			model := columnModel{ProjectID: types.StringValue("project-1")}
			if byID {
				model.ID = types.StringValue("column-1")
			} else {
				model.Slug = types.StringValue("testing")
			}
			config := columnTestConfig(t, d, model)
			response := datasource.ReadResponse{State: tfsdk.State{Schema: config.Schema}}
			d.Read(t.Context(), datasource.ReadRequest{Config: config}, &response)
			if response.Diagnostics.HasError() {
				t.Fatal(response.Diagnostics)
			}
			var state columnModel
			if diags := response.State.Get(t.Context(), &state); diags.HasError() {
				t.Fatal(diags)
			}
			if state.ID.ValueString() != "column-1" || state.Slug.ValueString() != "testing" || state.Position.ValueInt64() != 4 || !state.Icon.IsNull() {
				t.Fatalf("unexpected state: %+v", state)
			}
		})
	}
}

func TestColumnDataSourceErrors(t *testing.T) {
	for _, body := range []string{`[]`, `null`, `{}`, `{`, `[{}]`, `[` + columnFixture + `,` + strings.Replace(columnFixture, "column-1", "column-2", 1) + `]`} {
		t.Run(body, func(t *testing.T) {
			client := projectTestClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) })
			d := &columnDataSource{client: client}
			config := columnTestConfig(t, d, columnModel{ProjectID: types.StringValue("project-1"), Slug: types.StringValue("testing")})
			response := datasource.ReadResponse{State: tfsdk.State{Schema: config.Schema}}
			d.Read(t.Context(), datasource.ReadRequest{Config: config}, &response)
			if !response.Diagnostics.HasError() {
				t.Fatal("expected lookup error")
			}
		})
	}
}

func TestColumnImportInvalidIDs(t *testing.T) {
	for _, id := range []string{"", "column-1", "/column-1", "project-1/", "project-1/column-1/extra", " /column-1"} {
		t.Run(id, func(t *testing.T) {
			r := &columnResource{}
			response := resource.ImportStateResponse{}
			r.ImportState(t.Context(), resource.ImportStateRequest{ID: id}, &response)
			if !response.Diagnostics.HasError() {
				t.Fatal("expected import format error")
			}
		})
	}
}

func TestColumnDataSourceValidators(t *testing.T) {
	for _, tc := range []struct {
		name     string
		id, slug types.String
		valid    bool
	}{
		{name: "none"},
		{name: "id", id: types.StringValue("column-1"), valid: true},
		{name: "slug", slug: types.StringValue("to-do"), valid: true},
		{name: "both", id: types.StringValue("column-1"), slug: types.StringValue("to-do")},
		{name: "unknownID", id: types.StringUnknown(), valid: true},
		{name: "unknownSlug", slug: types.StringUnknown(), valid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := &columnDataSource{}
			config := columnTestConfig(t, d, columnModel{ProjectID: types.StringValue("project-1"), ID: tc.id, Slug: tc.slug})
			response := datasource.ValidateConfigResponse{}
			d.ConfigValidators(t.Context())[0].ValidateDataSource(t.Context(), datasource.ValidateConfigRequest{Config: config}, &response)
			if response.Diagnostics.HasError() == tc.valid {
				t.Fatalf("valid=%t, diagnostics=%v", tc.valid, response.Diagnostics)
			}
		})
	}
}
