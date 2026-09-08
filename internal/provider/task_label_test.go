// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestTaskLabelLifecycle(t *testing.T) {
	attaches, detaches := 0, 0
	client := projectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /label/label-1":
			if _, err := fmt.Fprint(w, labelFixture); err != nil {
				t.Error(err)
			}
		case "GET /label/copy-1":
			if _, err := fmt.Fprint(w, taskLabelFixture); err != nil {
				t.Error(err)
			}
		case "GET /label/task/task-1":
			if _, err := fmt.Fprint(w, `[`+strings.ReplaceAll(strings.ReplaceAll(taskLabelFixture, "copy-1", "other-1"), "Bug", "Unrelated")+`]`); err != nil {
				t.Error(err)
			}
		case "PUT /label/label-1/task":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if len(body) != 1 || body["taskId"] != "task-1" {
				t.Errorf("unexpected attach body: %v", body)
			}
			attaches++
			if _, err := fmt.Fprint(w, taskLabelFixture); err != nil {
				t.Error(err)
			}
		case "DELETE /label/copy-1/task":
			detaches++
			if _, err := fmt.Fprint(w, taskLabelFixture); err != nil {
				t.Error(err)
			}
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	})
	r := &taskLabelResource{client: client}
	model := taskLabelModelFromAPI(labelTestAPIValue(t, taskLabelFixture), types.StringValue("label-1"))
	plan := labelTestPlan(t, r, model)
	created := resource.CreateResponse{State: tfsdk.State{Schema: plan.Schema}}
	r.Create(t.Context(), resource.CreateRequest{Plan: plan}, &created)
	if created.Diagnostics.HasError() {
		t.Fatal(created.Diagnostics)
	}
	var got taskLabelModel
	if diags := created.State.Get(t.Context(), &got); diags.HasError() {
		t.Fatal(diags)
	}
	if got != model || got.ID == got.LabelID {
		t.Fatalf("wrong attachment state: %+v", got)
	}
	imported := resource.ImportStateResponse{State: tfsdk.State{Schema: plan.Schema}}
	if diags := imported.State.Set(t.Context(), taskLabelModel{}); diags.HasError() {
		t.Fatal(diags)
	}
	r.ImportState(t.Context(), resource.ImportStateRequest{ID: "label-1/copy-1"}, &imported)
	if imported.Diagnostics.HasError() {
		t.Fatal(imported.Diagnostics)
	}
	read := resource.ReadResponse{State: imported.State}
	r.Read(t.Context(), resource.ReadRequest{State: imported.State}, &read)
	if read.Diagnostics.HasError() {
		t.Fatal(read.Diagnostics)
	}
	if diags := read.State.Get(t.Context(), &got); diags.HasError() {
		t.Fatal(diags)
	}
	if got != model {
		t.Fatalf("import state=%+v, want %+v", got, model)
	}
	deleted := resource.DeleteResponse{State: read.State}
	r.Delete(t.Context(), resource.DeleteRequest{State: read.State}, &deleted)
	if deleted.Diagnostics.HasError() {
		t.Fatal(deleted.Diagnostics)
	}
	if attaches != 1 || detaches != 1 {
		t.Fatalf("attaches=%d detaches=%d", attaches, detaches)
	}
}

func TestTaskLabelCreateSafety(t *testing.T) {
	for _, tc := range []struct {
		name, source, list string
		listStatus         int
	}{
		{"taskSpecificSource", strings.Replace(labelFixture, `"taskId":null`, `"taskId":"other-task"`, 1), "", 0},
		{"existingCopy", labelFixture, `[` + taskLabelFixture + `]`, 200},
		{"nullList", labelFixture, `null`, 200},
		{"invalidList", labelFixture, `{`, 200},
		{"malformedLabel", labelFixture, `[{}]`, 200},
		{"otherTask", labelFixture, `[` + strings.Replace(taskLabelFixture, "task-1", "task-2", 1) + `]`, 200},
		{"otherWorkspace", labelFixture, `[` + strings.Replace(taskLabelFixture, "workspace-1", "workspace-2", 1) + `]`, 200},
		{"forbiddenList", labelFixture, `{}`, 403},
		{"missingTask", labelFixture, `{}`, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := projectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Error("unsafe mutation")
					w.WriteHeader(500)
					return
				}
				if r.URL.Path == "/label/label-1" {
					if _, err := fmt.Fprint(w, tc.source); err != nil {
						t.Error(err)
					}
					return
				}
				if tc.listStatus == 0 {
					t.Error("task-specific source must be rejected before listing")
					w.WriteHeader(500)
					return
				}
				w.WriteHeader(tc.listStatus)
				if _, err := fmt.Fprint(w, tc.list); err != nil {
					t.Error(err)
				}
			})
			r := &taskLabelResource{client: client}
			plan := labelTestPlan(t, r, taskLabelModel{LabelID: types.StringValue("label-1"), TaskID: types.StringValue("task-1")})
			response := resource.CreateResponse{State: tfsdk.State{Schema: plan.Schema}}
			r.Create(t.Context(), resource.CreateRequest{Plan: plan}, &response)
			if !response.Diagnostics.HasError() {
				t.Fatal("expected attachment error")
			}
		})
	}
}

func TestTaskLabelMutationErrors(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
	}{
		{400, `{}`}, {401, `{}`}, {403, `{}`}, {404, `{}`}, {500, `{}`}, {200, `null`}, {200, `{}`}, {200, `{`},
		{200, strings.Replace(taskLabelFixture, "copy-1", "label-1", 1)},
		{200, strings.Replace(taskLabelFixture, "task-1", "task-2", 1)},
		{200, strings.Replace(taskLabelFixture, "workspace-1", "workspace-2", 1)},
	} {
		t.Run(fmt.Sprintf("%d/%s", tc.status, tc.body), func(t *testing.T) {
			client := projectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					switch r.URL.Path {
					case "/label/label-1":
						if _, err := fmt.Fprint(w, labelFixture); err != nil {
							t.Error(err)
						}
					case "/label/copy-1":
						if _, err := fmt.Fprint(w, taskLabelFixture); err != nil {
							t.Error(err)
						}
					case "/label/task/task-1":
						if _, err := fmt.Fprint(w, "[]"); err != nil {
							t.Error(err)
						}
					default:
						t.Errorf("unexpected lookup %s", r.URL.Path)
					}
					return
				}
				w.WriteHeader(tc.status)
				if _, err := fmt.Fprint(w, tc.body); err != nil {
					t.Error(err)
				}
			})
			r := &taskLabelResource{client: client}
			model := taskLabelModelFromAPI(labelTestAPIValue(t, taskLabelFixture), types.StringValue("label-1"))
			plan := labelTestPlan(t, r, model)
			state := tfsdk.State(plan)
			created := resource.CreateResponse{State: tfsdk.State{Schema: plan.Schema}}
			r.Create(t.Context(), resource.CreateRequest{Plan: plan}, &created)
			if !created.Diagnostics.HasError() {
				t.Fatal("expected attach error")
			}
			if tc.status != 200 {
				deleted := resource.DeleteResponse{State: state}
				r.Delete(t.Context(), resource.DeleteRequest{State: state}, &deleted)
				if !deleted.Diagnostics.HasError() || !deleted.State.Raw.Equal(state.Raw) {
					t.Fatalf("detach diagnostics=%v", deleted.Diagnostics)
				}
			}
		})
	}
}

func TestTaskLabelReadAndDeleteSafety(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     int
		body       string
		listStatus int
		list       string
		absent     bool
	}{
		{"missing400", 400, `{}`, 200, `[` + labelFixture + `]`, true},
		{"missing404", 404, `{}`, 200, `[]`, true},
		{"stillExists", 400, `{}`, 200, `[` + taskLabelFixture + `]`, false},
		{"forbidden", 403, `{}`, 0, "", false},
		{"unauthorized", 401, `{}`, 0, "", false},
		{"serverError", 500, `{}`, 0, "", false},
		{"listForbidden", 400, `{}`, 403, `{}`, false},
		{"missingWorkspace", 400, `{}`, 400, `{}`, false},
		{"nullList", 400, `{}`, 200, `null`, false},
		{"nullCopy", 200, `null`, 0, "", false},
		{"wrongID", 200, strings.Replace(taskLabelFixture, "copy-1", "copy-2", 1), 0, "", false},
		{"wrongWorkspace", 200, strings.Replace(taskLabelFixture, "workspace-1", "workspace-2", 1), 0, "", false},
		{"movedTask", 200, strings.Replace(taskLabelFixture, "task-1", "task-2", 1), 0, "", true},
		{"becameWorkspaceLabel", 200, strings.Replace(taskLabelFixture, `"taskId":"task-1"`, `"taskId":null`, 1), 0, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := projectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Error("must not detach a missing, inaccessible, or moved copy")
					w.WriteHeader(500)
					return
				}
				if strings.Contains(r.URL.Path, "/workspace/") {
					if tc.listStatus == 0 {
						t.Error("unexpected list")
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
			r := &taskLabelResource{client: client}
			plan := labelTestPlan(t, r, taskLabelModelFromAPI(labelTestAPIValue(t, taskLabelFixture), types.StringValue("label-1")))
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
				t.Fatalf("detach diagnostics=%v", deleted.Diagnostics)
			}
		})
	}
}

func TestTaskLabelImportInvalidIDs(t *testing.T) {
	for _, id := range []string{"", "source", "/copy", "source/", "source/ ", "a/b/c", "same/same"} {
		response := resource.ImportStateResponse{}
		(&taskLabelResource{}).ImportState(t.Context(), resource.ImportStateRequest{ID: id}, &response)
		if !response.Diagnostics.HasError() {
			t.Fatalf("accepted import ID %q", id)
		}
	}
	for _, tc := range []struct{ name, source, copy string }{
		{"workspaceCopy", labelFixture, strings.Replace(labelFixture, "label-1", "copy-1", 1)},
		{"taskSource", strings.Replace(labelFixture, `"taskId":null`, `"taskId":"task-2"`, 1), taskLabelFixture},
		{"crossWorkspace", strings.Replace(labelFixture, "workspace-1", "workspace-2", 1), taskLabelFixture},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := projectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/label/label-1" {
					if _, err := fmt.Fprint(w, tc.source); err != nil {
						t.Error(err)
					}
				} else {
					if _, err := fmt.Fprint(w, tc.copy); err != nil {
						t.Error(err)
					}
				}
			})
			r := &taskLabelResource{client: client}
			plan := labelTestPlan(t, r, taskLabelModel{LabelID: types.StringValue("label-1"), ID: types.StringValue("copy-1")})
			state := tfsdk.State(plan)
			response := resource.ReadResponse{State: state}
			r.Read(t.Context(), resource.ReadRequest{State: state}, &response)
			if !response.Diagnostics.HasError() || !response.State.Raw.Equal(state.Raw) {
				t.Fatalf("import diagnostics=%v", response.Diagnostics)
			}
		})
	}
}
