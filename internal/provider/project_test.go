// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"net/http"
	"reflect"
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

func TestProjectMutationWireShapes(t *testing.T) {
	for _, create := range []bool{true, false} {
		t.Run(fmt.Sprintf("create=%t", create), func(t *testing.T) {
			model := projectTestModel()
			model.ID = types.StringValue("project-1")
			if create {
				model.Description, model.IsPublic = types.StringValue("Created description"), types.BoolValue(true)
			} else {
				model.Name, model.Slug, model.Icon = types.StringValue("Renamed"), types.StringValue("NEW"), types.StringValue("")
			}
			var calls []string
			r := &projectResource{client: testClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls = append(calls, r.Method+" "+r.URL.Path)
				var body map[string]any
				if !testDecodeRequest(t, w, r, &body) {
					return
				}
				want := map[string]any{"name": model.Name.ValueString(), "slug": model.Slug.ValueString(), "icon": model.Icon.ValueString(), "description": model.Description.ValueString(), "isPublic": model.IsPublic.ValueBool()}
				if r.Method == http.MethodPost {
					want = map[string]any{"workspaceId": "workspace-1", "name": "Engineering", "slug": "ENG", "icon": "Layout"}
				}
				if !reflect.DeepEqual(body, want) {
					t.Errorf("body = %v, want %v", body, want)
				}
				result := projectFixture
				if create && r.Method == http.MethodPut {
					result = strings.ReplaceAll(strings.ReplaceAll(result, `"description":null`, `"description":"Created description"`), `"isPublic":false`, `"isPublic":true`)
				}
				if _, err := fmt.Fprint(w, result); err != nil {
					t.Error(err)
				}
			})}
			plan := testPlan(t, r, model)
			wantCalls := "PUT /project/project-1"
			if create {
				response := resource.CreateResponse{State: tfsdk.State{Schema: plan.Schema}}
				r.Create(t.Context(), resource.CreateRequest{Plan: plan}, &response)
				if response.Diagnostics.HasError() {
					t.Fatal(response.Diagnostics)
				}
				var got projectModel
				if diags := response.State.Get(t.Context(), &got); diags.HasError() {
					t.Fatal(diags)
				}
				if got.ID != model.ID || got.Description != model.Description || got.IsPublic != model.IsPublic {
					t.Fatalf("second response not saved: %+v", got)
				}
				wantCalls = "POST /project,PUT /project/project-1"
			} else {
				response := resource.UpdateResponse{State: tfsdk.State(plan)}
				r.Update(t.Context(), resource.UpdateRequest{Plan: plan}, &response)
				if response.Diagnostics.HasError() {
					t.Fatal(response.Diagnostics)
				}
			}
			if strings.Join(calls, ",") != wantCalls {
				t.Fatalf("calls = %v, want %s", calls, wantCalls)
			}
		})
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
