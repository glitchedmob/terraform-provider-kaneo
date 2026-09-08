// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	kaneoclient "github.com/glitchedmob/terraform-provider-kaneo/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const labelFixture = `{"id":"label-1","workspaceId":"workspace-1","taskId":null,"name":"Bug","color":"#ef4444","createdAt":"2026-09-01T12:00:00Z","updatedAt":"2026-09-01T12:00:00Z"}`
const taskLabelFixture = `{"id":"copy-1","workspaceId":"workspace-1","taskId":"task-1","name":"Bug","color":"#ef4444","createdAt":"2026-09-01T12:00:00Z","updatedAt":"2026-09-01T12:00:00Z"}`

func labelTestAPIValue(t *testing.T, body string) kaneoclient.Label {
	t.Helper()
	var label kaneoclient.Label
	testFixture(t, body, &label)
	return label
}

func TestLabelMutationWireShapes(t *testing.T) {
	for _, create := range []bool{true, false} {
		t.Run(fmt.Sprintf("create=%t", create), func(t *testing.T) {
			var calls []string
			r := &labelResource{client: testClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls = append(calls, r.Method+" "+r.URL.Path)
				result := labelFixture
				if r.Method == http.MethodGet {
					if r.URL.Path == "/label/workspace/workspace-1" {
						result = "[]"
					}
				} else {
					var body map[string]any
					if !testDecodeRequest(t, w, r, &body) {
						return
					}
					want := map[string]any{"name": "Bug", "color": "#ef4444"}
					if create {
						want["workspaceId"] = "workspace-1"
					}
					if !reflect.DeepEqual(body, want) {
						t.Errorf("workspace label body = %v, want %v; taskId must be absent", body, want)
					}
				}
				if _, err := fmt.Fprint(w, result); err != nil {
					t.Error(err)
				}
			})}
			plan := testPlan(t, r, labelModelFromAPI(labelTestAPIValue(t, labelFixture)))
			wantCalls := "GET /label/label-1,PUT /label/label-1"
			if create {
				response := resource.CreateResponse{State: tfsdk.State{Schema: plan.Schema}}
				r.Create(t.Context(), resource.CreateRequest{Plan: plan}, &response)
				if response.Diagnostics.HasError() {
					t.Fatal(response.Diagnostics)
				}
				wantCalls = "GET /label/workspace/workspace-1,POST /label"
			} else {
				response := resource.UpdateResponse{State: tfsdk.State(plan)}
				r.Update(t.Context(), resource.UpdateRequest{Plan: plan, State: tfsdk.State(plan)}, &response)
				if response.Diagnostics.HasError() {
					t.Fatal(response.Diagnostics)
				}
			}
			if strings.Join(calls, ",") != wantCalls {
				t.Fatalf("scope check must precede mutation: %v", calls)
			}
		})
	}
}

func TestLabelReadAndDeleteErrors(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     int
		body       string
		listStatus int
		list       string
		absent     bool
	}{
		{"missing400", 400, `{}`, 200, `[]`, true},
		{"missing404", 404, `{}`, 200, `[` + taskLabelFixture + `]`, true},
		{"stillExists", 400, `{}`, 200, `[` + labelFixture + `]`, false},
		{"forbidden", 403, `{}`, 0, "", false},
		{"unauthorized", 401, `{}`, 0, "", false},
		{"serverError", 500, `{}`, 0, "", false},
		{"listForbidden", 400, `{}`, 403, `{}`, false},
		{"missingWorkspace", 400, `{}`, 400, `{}`, false},
		{"nullList", 400, `{}`, 200, `null`, false},
		{"invalidList", 400, `{}`, 200, `{`, false},
		{"invalidListItem", 400, `{}`, 200, `[{}]`, false},
		{"duplicateListItem", 400, `{}`, 200, `[` + taskLabelFixture + `,` + taskLabelFixture + `]`, false},
		{"wrongListWorkspace", 400, `{}`, 200, `[` + strings.Replace(taskLabelFixture, "workspace-1", "workspace-2", 1) + `]`, false},
		{"nullLabel", 200, `null`, 0, "", false},
		{"invalidJSON", 200, `{`, 0, "", false},
		{"wrongID", 200, strings.Replace(labelFixture, "label-1", "label-2", 1), 0, "", false},
		{"wrongWorkspace", 200, strings.Replace(labelFixture, "workspace-1", "workspace-2", 1), 0, "", false},
		{"missingTaskID", 200, strings.Replace(labelFixture, `"taskId":null,`, "", 1), 0, "", false},
		{"taskCopy", 200, strings.Replace(labelFixture, `"taskId":null`, `"taskId":"task-1"`, 1), 0, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("unexpected mutation: %s", r.Method)
				}
				if r.URL.Path == "/label/workspace/workspace-1" {
					if tc.listStatus == 0 {
						t.Error("unexpected list lookup")
						w.WriteHeader(500)
						return
					}
					w.WriteHeader(tc.listStatus)
					if _, err := fmt.Fprint(w, tc.list); err != nil {
						t.Error(err)
					}
					return
				}
				w.WriteHeader(tc.status)
				if _, err := fmt.Fprint(w, tc.body); err != nil {
					t.Error(err)
				}
			})
			r := &labelResource{client: client}
			plan := testPlan(t, r, labelModelFromAPI(labelTestAPIValue(t, labelFixture)))
			state := tfsdk.State(plan)
			read := resource.ReadResponse{State: state}
			r.Read(t.Context(), resource.ReadRequest{State: state}, &read)
			if read.Diagnostics.HasError() == tc.absent || read.State.Raw.IsNull() != tc.absent {
				t.Fatalf("read removed=%t, diagnostics=%v", read.State.Raw.IsNull(), read.Diagnostics)
			}
			if !tc.absent && !read.State.Raw.Equal(state.Raw) {
				t.Fatal("error changed state")
			}
			deleted := resource.DeleteResponse{State: state}
			r.Delete(t.Context(), resource.DeleteRequest{State: state}, &deleted)
			if deleted.Diagnostics.HasError() == tc.absent {
				t.Fatalf("delete diagnostics=%v", deleted.Diagnostics)
			}
			// ID-only lookup cannot infer workspace scope, so a different workspace is valid.
			if tc.name != "wrongWorkspace" {
				config := testConfig(t, &labelDataSource{}, labelModel{ID: types.StringValue("label-1")})
				lookup := datasource.ReadResponse{State: tfsdk.State{Schema: config.Schema}}
				(&labelDataSource{client: client}).Read(t.Context(), datasource.ReadRequest{Config: config}, &lookup)
				if !lookup.Diagnostics.HasError() {
					t.Fatal("expected lookup error")
				}
			}
		})
	}
}

func TestLabelMutationErrors(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
	}{
		{400, `{}`}, {401, `{}`}, {403, `{}`}, {500, `{}`}, {200, `null`}, {200, `{}`}, {200, `{`},
		{200, strings.Replace(labelFixture, "workspace-1", "workspace-2", 1)},
		{200, strings.Replace(labelFixture, `"taskId":null`, `"taskId":"task-1"`, 1)},
	} {
		t.Run(fmt.Sprintf("%d/%s", tc.status, tc.body), func(t *testing.T) {
			client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					if strings.Contains(r.URL.Path, "/workspace/") {
						if _, err := fmt.Fprint(w, "[]"); err != nil {
							t.Error(err)
						}
					} else {
						if _, err := fmt.Fprint(w, labelFixture); err != nil {
							t.Error(err)
						}
					}
					return
				}
				w.WriteHeader(tc.status)
				if _, err := fmt.Fprint(w, tc.body); err != nil {
					t.Error(err)
				}
			})
			r := &labelResource{client: client}
			plan := testPlan(t, r, labelModelFromAPI(labelTestAPIValue(t, labelFixture)))
			state := tfsdk.State(plan)
			created := resource.CreateResponse{State: tfsdk.State{Schema: plan.Schema}}
			r.Create(t.Context(), resource.CreateRequest{Plan: plan}, &created)
			if !created.Diagnostics.HasError() {
				t.Fatal("expected create error")
			}
			updated := resource.UpdateResponse{State: state}
			r.Update(t.Context(), resource.UpdateRequest{Plan: plan, State: state}, &updated)
			if !updated.Diagnostics.HasError() || !updated.State.Raw.Equal(state.Raw) {
				t.Fatalf("update diagnostics=%v", updated.Diagnostics)
			}
			if tc.status != 200 {
				deleted := resource.DeleteResponse{State: state}
				r.Delete(t.Context(), resource.DeleteRequest{State: state}, &deleted)
				if !deleted.Diagnostics.HasError() {
					t.Fatal("expected delete error")
				}
			}
		})
	}
}

func TestLabelCollision(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Error("existing label must not be adopted")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if _, err := fmt.Fprint(w, `[`+labelFixture+`]`); err != nil {
			t.Error(err)
		}
	})
	r := &labelResource{client: client}
	plan := testPlan(t, r, labelModelFromAPI(labelTestAPIValue(t, labelFixture)))
	response := resource.CreateResponse{State: tfsdk.State{Schema: plan.Schema}}
	r.Create(t.Context(), resource.CreateRequest{Plan: plan}, &response)
	if !response.Diagnostics.HasError() {
		t.Fatal("expected collision diagnostic")
	}
}

func TestLabelImportInvalidIDs(t *testing.T) {
	for _, id := range []string{"", " ", "\t\n"} {
		response := resource.ImportStateResponse{}
		(&labelResource{}).ImportState(t.Context(), resource.ImportStateRequest{ID: id}, &response)
		if !response.Diagnostics.HasError() {
			t.Fatalf("accepted import ID %q", id)
		}
	}
}
