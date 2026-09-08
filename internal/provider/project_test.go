// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const projectFixture = `{"id":"project-1","workspaceId":"workspace-1","name":"Engineering","slug":"ENG","icon":"Layout","description":null,"isPublic":false,"createdAt":"2026-09-01T12:00:00Z","archivedAt":null,"position":0,"lastTaskNumber":0}`

func projectTestModel() projectModel {
	return projectModel{
		WorkspaceID: types.StringValue("workspace-1"), Name: types.StringValue("Engineering"), Slug: types.StringValue("ENG"),
		Icon: types.StringValue("Layout"), Description: types.StringValue(""), IsPublic: types.BoolValue(false),
	}
}

func TestProjectResourceLifecycle(t *testing.T) {
	var remote map[string]any
	testFixture(t, projectFixture, &remote)
	var calls []string
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch r.Method {
		case http.MethodPost:
			var body map[string]any
			if !testDecodeRequest(t, w, r, &body) {
				return
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
			if !testDecodeRequest(t, w, r, &body) {
				return
			}
			for _, field := range []string{"name", "slug", "icon", "description", "isPublic"} {
				value, ok := body[field]
				if !ok {
					t.Errorf("update omitted %s", field)
				}
				remote[field] = value
			}
		}
		testEncodeResponse(t, w, remote)
	})
	r := &projectResource{client: client}
	model := projectTestModel()
	model.Description = types.StringValue("Created description")
	model.IsPublic = types.BoolValue(true)
	plan := testPlan(t, r, model)
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
	r.Update(t.Context(), resource.UpdateRequest{Plan: testPlan(t, r, state)}, &update)
	if update.Diagnostics.HasError() {
		t.Fatal(update.Diagnostics)
	}
	if remote["name"] != "Renamed" || remote["slug"] != "NEW" || remote["icon"] != "" || remote["description"] != "" || remote["isPublic"] != false {
		t.Fatalf("update did not send cleared values: %v", remote)
	}

	imported := resource.ImportStateResponse{State: tfsdk.State(testPlan(t, r, projectModel{}))}
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
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			if _, err := fmt.Fprint(w, projectFixture); err != nil {
				t.Error(err)
			}
			return
		}
		w.WriteHeader(http.StatusForbidden)
		if _, err := fmt.Fprint(w, `{"message":"Forbidden"}`); err != nil {
			t.Error(err)
		}
	})
	r := &projectResource{client: client}
	model := projectTestModel()
	model.Description = types.StringValue("Needs a second request")
	plan := testPlan(t, r, model)
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
			client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/project" {
					if r.URL.Query().Get("workspaceId") != "workspace-1" || r.URL.Query().Get("includeArchived") != "true" {
						t.Errorf("unexpected list query: %s", r.URL.RawQuery)
					}
					w.WriteHeader(tc.listStatus)
					if _, err := fmt.Fprint(w, tc.list); err != nil {
						t.Error(err)
					}
					return
				}
				w.WriteHeader(tc.status)
				if _, err := fmt.Fprint(w, `{"message":"Workspace ID could not be determined"}`); err != nil {
					t.Error(err)
				}
			})
			r := &projectResource{client: client}
			model := projectTestModel()
			model.ID = types.StringValue("project-1")
			plan := testPlan(t, r, model)
			state := tfsdk.State(plan)
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
			client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/project" {
					if byID {
						t.Error("ID lookup must not list projects")
					}
					if r.URL.Query().Get("workspaceId") != "workspace-1" || r.URL.Query().Get("includeArchived") != "true" {
						t.Errorf("unexpected list query: %s", r.URL.RawQuery)
					}
					if _, err := fmt.Fprintf(w, "[%s]", archived); err != nil {
						t.Error(err)
					}
					return
				}
				if r.URL.Path != "/project/project-1" {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
				if _, err := fmt.Fprint(w, archived); err != nil {
					t.Error(err)
				}
			})
			d := &projectDataSource{client: client}
			model := projectModel{}
			if byID {
				model.ID = types.StringValue("project-1")
			} else {
				model.WorkspaceID = types.StringValue("workspace-1")
				model.Slug = types.StringValue("ENG")
			}
			config := testConfig(t, d, model)
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
			client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				if _, err := fmt.Fprint(w, tc.body); err != nil {
					t.Error(err)
				}
			})
			d := &projectDataSource{client: client}
			model := projectModel{WorkspaceID: types.StringValue("workspace-1"), Slug: types.StringValue("ENG")}
			if tc.byID {
				model = projectModel{ID: types.StringValue("project-1")}
			}
			config := testConfig(t, d, model)
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
			config := testConfig(t, d, projectModel{ID: tc.id, WorkspaceID: tc.workspace, Slug: tc.slug})
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
