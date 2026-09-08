// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	kaneoclient "github.com/glitchedmob/terraform-provider-kaneo/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const projectFixture = `{"id":"project-1","workspaceId":"workspace-1","name":"Engineering","slug":"ENG","icon":"Layout","description":null,"isPublic":false,"createdAt":"2026-09-01T12:00:00Z","archivedAt":null,"position":0,"lastTaskNumber":0}`

func projectTestClient(t *testing.T, handler http.HandlerFunc) *kaneoclient.ClientWithResponses {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("missing API key")
		}
		w.Header().Set("Content-Type", "application/json")
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	client, err := newAPIClient(server.URL, "test-key", "test")
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func projectTestPlan(t *testing.T, r *projectResource, model projectModel) tfsdk.Plan {
	t.Helper()
	var response resource.SchemaResponse
	r.Schema(t.Context(), resource.SchemaRequest{}, &response)
	if response.Diagnostics.HasError() {
		t.Fatal(response.Diagnostics)
	}
	plan := tfsdk.Plan{Schema: response.Schema}
	if diags := plan.Set(t.Context(), model); diags.HasError() {
		t.Fatal(diags)
	}
	return plan
}

func projectTestConfig(t *testing.T, d *projectDataSource, model projectModel) tfsdk.Config {
	t.Helper()
	var response datasource.SchemaResponse
	d.Schema(t.Context(), datasource.SchemaRequest{}, &response)
	state := tfsdk.State{Schema: response.Schema}
	if diags := state.Set(t.Context(), model); diags.HasError() {
		t.Fatal(diags)
	}
	return tfsdk.Config{Schema: response.Schema, Raw: state.Raw}
}

func projectTestModel() projectModel {
	return projectModel{
		WorkspaceID: types.StringValue("workspace-1"), Name: types.StringValue("Engineering"), Slug: types.StringValue("ENG"),
		Icon: types.StringValue("Layout"), Description: types.StringValue(""), IsPublic: types.BoolValue(false),
	}
}

func TestProjectResourceLifecycle(t *testing.T) {
	var remote map[string]any
	if err := json.Unmarshal([]byte(projectFixture), &remote); err != nil {
		t.Fatal(err)
	}
	var calls []string
	client := projectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch r.Method {
		case http.MethodPost:
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			for _, field := range []string{"workspaceId", "name", "slug", "icon"} {
				if body[field] != remote[field] {
					t.Errorf("create %s = %v, want %v", field, body[field], remote[field])
				}
			}
			if len(body) != 4 {
				t.Errorf("unexpected create fields: %v", body)
			}
		case http.MethodPut:
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			for _, field := range []string{"name", "slug", "icon", "description", "isPublic"} {
				value, ok := body[field]
				if !ok {
					t.Errorf("update omitted %s", field)
				}
				remote[field] = value
			}
		}
		json.NewEncoder(w).Encode(remote)
	})
	r := &projectResource{client: client}
	model := projectTestModel()
	model.Description = types.StringValue("Created description")
	model.IsPublic = types.BoolValue(true)
	plan := projectTestPlan(t, r, model)
	create := resource.CreateResponse{State: tfsdk.State{Schema: plan.Schema}}
	r.Create(t.Context(), resource.CreateRequest{Plan: plan}, &create)
	if create.Diagnostics.HasError() {
		t.Fatal(create.Diagnostics)
	}
	var state projectModel
	if diags := create.State.Get(t.Context(), &state); diags.HasError() {
		t.Fatal(diags)
	}
	if state.ID.ValueString() != "project-1" || state.Description != model.Description || state.IsPublic != model.IsPublic {
		t.Fatalf("unexpected create state: %+v", state)
	}
	if strings.Join(calls, ",") != "POST /project,PUT /project/project-1" {
		t.Fatalf("create calls: %v", calls)
	}

	state.Name = types.StringValue("Renamed")
	state.Slug = types.StringValue("NEW")
	state.Icon = types.StringValue("")
	state.Description = types.StringValue("")
	state.IsPublic = types.BoolValue(false)
	update := resource.UpdateResponse{State: create.State}
	r.Update(t.Context(), resource.UpdateRequest{Plan: projectTestPlan(t, r, state)}, &update)
	if update.Diagnostics.HasError() {
		t.Fatal(update.Diagnostics)
	}
	if remote["name"] != "Renamed" || remote["slug"] != "NEW" || remote["icon"] != "" || remote["description"] != "" || remote["isPublic"] != false {
		t.Fatalf("update did not send cleared values: %v", remote)
	}

	imported := resource.ImportStateResponse{State: tfsdk.State{Schema: plan.Schema}}
	if diags := imported.State.Set(t.Context(), projectModel{}); diags.HasError() {
		t.Fatal(diags)
	}
	r.ImportState(t.Context(), resource.ImportStateRequest{ID: "project-1"}, &imported)
	if imported.Diagnostics.HasError() {
		t.Fatal(imported.Diagnostics)
	}
	read := resource.ReadResponse{State: imported.State}
	r.Read(t.Context(), resource.ReadRequest{State: imported.State}, &read)
	if read.Diagnostics.HasError() {
		t.Fatal(read.Diagnostics)
	}
	var importedModel projectModel
	if diags := read.State.Get(t.Context(), &importedModel); diags.HasError() {
		t.Fatal(diags)
	}
	if importedModel != state {
		t.Fatalf("import state = %+v, want %+v", importedModel, state)
	}

	deleted := resource.DeleteResponse{State: read.State}
	r.Delete(t.Context(), resource.DeleteRequest{State: read.State}, &deleted)
	if deleted.Diagnostics.HasError() {
		t.Fatal(deleted.Diagnostics)
	}
	if calls[len(calls)-1] != "DELETE /project/project-1" {
		t.Fatalf("delete call: %v", calls)
	}
}

func TestProjectCreatePartialFailure(t *testing.T) {
	client := projectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			fmt.Fprint(w, projectFixture)
			return
		}
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"message":"Forbidden"}`)
	})
	r := &projectResource{client: client}
	model := projectTestModel()
	model.Description = types.StringValue("Needs a second request")
	plan := projectTestPlan(t, r, model)
	response := resource.CreateResponse{State: tfsdk.State{Schema: plan.Schema}}
	r.Create(t.Context(), resource.CreateRequest{Plan: plan}, &response)
	if !response.Diagnostics.HasError() {
		t.Fatal("expected update failure")
	}
	var state projectModel
	if diags := response.State.Get(t.Context(), &state); diags.HasError() {
		t.Fatal(diags)
	}
	if state.ID.ValueString() != "project-1" || state.Description.ValueString() != "" {
		t.Fatalf("created state not preserved: %+v", state)
	}
}

func TestProjectMissingAndErrors(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     int
		listStatus int
		list       string
		absent     bool
	}{
		{"missing400", 400, 200, `[]`, true},
		{"missing404", 404, 200, `[]`, true},
		{"lookupFailureNotDeletion", 400, 200, `[` + projectFixture + `]`, false},
		{"forbidden", 403, 200, `[]`, false},
		{"serverError", 500, 200, `[]`, false},
		{"listForbidden", 400, 403, `{}`, false},
		{"listServerError", 400, 500, `{}`, false},
		{"listNull", 400, 200, `null`, false},
		{"listInvalidJSON", 400, 200, `{`, false},
		{"listInvalidProject", 400, 200, `[{}]`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := projectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/project" {
					if r.URL.Query().Get("workspaceId") != "workspace-1" || r.URL.Query().Get("includeArchived") != "true" {
						t.Errorf("unexpected list query: %s", r.URL.RawQuery)
					}
					w.WriteHeader(tc.listStatus)
					fmt.Fprint(w, tc.list)
					return
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, `{"message":"Workspace ID could not be determined"}`)
			})
			r := &projectResource{client: client}
			model := projectTestModel()
			model.ID = types.StringValue("project-1")
			plan := projectTestPlan(t, r, model)
			state := tfsdk.State{Schema: plan.Schema, Raw: plan.Raw}
			read := resource.ReadResponse{State: state}
			r.Read(t.Context(), resource.ReadRequest{State: state}, &read)
			if read.Diagnostics.HasError() == tc.absent {
				t.Fatalf("read diagnostics: %v", read.Diagnostics)
			}
			if read.State.Raw.IsNull() != tc.absent {
				t.Fatalf("read removed state = %v, want %v", read.State.Raw.IsNull(), tc.absent)
			}
			deleted := resource.DeleteResponse{State: state}
			r.Delete(t.Context(), resource.DeleteRequest{State: state}, &deleted)
			if deleted.Diagnostics.HasError() == tc.absent {
				t.Fatalf("delete diagnostics: %v", deleted.Diagnostics)
			}
		})
	}
}

func TestProjectDataSourceLookups(t *testing.T) {
	for _, byID := range []bool{true, false} {
		t.Run(fmt.Sprintf("byID=%t", byID), func(t *testing.T) {
			archived := strings.Replace(projectFixture, `"archivedAt":null`, `"archivedAt":"2026-09-02T12:00:00Z"`, 1)
			client := projectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/project" {
					if byID {
						t.Error("ID lookup must not list projects")
					}
					if r.URL.Query().Get("workspaceId") != "workspace-1" || r.URL.Query().Get("includeArchived") != "true" {
						t.Errorf("unexpected list query: %s", r.URL.RawQuery)
					}
					fmt.Fprintf(w, "[%s]", archived)
					return
				}
				if r.URL.Path != "/project/project-1" {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
				fmt.Fprint(w, archived)
			})
			d := &projectDataSource{client: client}
			model := projectModel{}
			if byID {
				model.ID = types.StringValue("project-1")
			} else {
				model.WorkspaceID = types.StringValue("workspace-1")
				model.Slug = types.StringValue("ENG")
			}
			config := projectTestConfig(t, d, model)
			response := datasource.ReadResponse{State: tfsdk.State{Schema: config.Schema}}
			d.Read(t.Context(), datasource.ReadRequest{Config: config}, &response)
			if response.Diagnostics.HasError() {
				t.Fatal(response.Diagnostics)
			}
			var state projectModel
			if diags := response.State.Get(t.Context(), &state); diags.HasError() {
				t.Fatal(diags)
			}
			if state.ID.ValueString() != "project-1" || state.WorkspaceID.ValueString() != "workspace-1" || state.ArchivedAt.ValueString() != "2026-09-02T12:00:00Z" || state.Description.ValueString() != "" || state.IsPublic.ValueBool() {
				t.Fatalf("unexpected state: %+v", state)
			}
		})
	}
}

func TestProjectDataSourceErrors(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		byID       bool
	}{
		{"missingSlug", `[]`, 200, false},
		{"duplicateSlug", `[` + projectFixture + `,` + strings.Replace(projectFixture, "project-1", "project-2", 1) + `]`, 200, false},
		{"forbidden", `{}`, 403, true},
		{"missingID", `{"message":"Workspace ID could not be determined"}`, 400, true},
		{"invalidJSON", `{`, 200, true},
		{"emptyResponse", ``, 200, true},
		{"nullResponse", `null`, 200, true},
		{"invalidProject", `{}`, 200, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := projectTestClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); fmt.Fprint(w, tc.body) })
			d := &projectDataSource{client: client}
			model := projectModel{WorkspaceID: types.StringValue("workspace-1"), Slug: types.StringValue("ENG")}
			if tc.byID {
				model = projectModel{ID: types.StringValue("project-1")}
			}
			config := projectTestConfig(t, d, model)
			response := datasource.ReadResponse{State: tfsdk.State{Schema: config.Schema}}
			d.Read(t.Context(), datasource.ReadRequest{Config: config}, &response)
			if !response.Diagnostics.HasError() {
				t.Fatal("expected lookup error")
			}
		})
	}
}

func TestProjectDataSourceValidators(t *testing.T) {
	for _, tc := range []struct {
		name                string
		id, workspace, slug types.String
		valid               bool
	}{
		{name: "none"},
		{name: "id", id: types.StringValue("project-1"), valid: true},
		{name: "slugAndWorkspace", workspace: types.StringValue("workspace-1"), slug: types.StringValue("ENG"), valid: true},
		{name: "slugOnly", slug: types.StringValue("ENG")},
		{name: "workspaceOnly", workspace: types.StringValue("workspace-1")},
		{name: "idAndWorkspace", id: types.StringValue("project-1"), workspace: types.StringValue("workspace-1")},
		{name: "all", id: types.StringValue("project-1"), workspace: types.StringValue("workspace-1"), slug: types.StringValue("ENG")},
		{name: "unknownID", id: types.StringUnknown(), valid: true},
		{name: "unknownPair", workspace: types.StringUnknown(), slug: types.StringUnknown(), valid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := &projectDataSource{}
			config := projectTestConfig(t, d, projectModel{ID: tc.id, WorkspaceID: tc.workspace, Slug: tc.slug})
			response := datasource.ValidateConfigResponse{}
			for _, validator := range d.ConfigValidators(t.Context()) {
				var result datasource.ValidateConfigResponse
				validator.ValidateDataSource(t.Context(), datasource.ValidateConfigRequest{Config: config}, &result)
				response.Diagnostics.Append(result.Diagnostics...)
			}
			if response.Diagnostics.HasError() == tc.valid {
				t.Fatalf("valid=%t: %v", tc.valid, response.Diagnostics)
			}
		})
	}
}
