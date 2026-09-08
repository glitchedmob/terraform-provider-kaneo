// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	kaneoclient "github.com/glitchedmob/terraform-provider-kaneo/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/oapi-codegen/nullable"
)

const taskFixture = `{"id":"task-1","projectId":"project-1","title":"Testing","description":"Description","status":"testing","priority":"high","userId":"user-1","startDate":"2026-09-01T12:00:00.12Z","dueDate":"2026-09-02T12:00:00.001Z","number":16777217,"position":16777217,"createdAt":"2026-09-01T12:00:00Z"}`
const emptyTaskBoard = `{"data":{"id":"project-1","columns":[{"tasks":[]}],"archivedTasks":[],"plannedTasks":[]},"pagination":{"page":1,"totalPages":1,"total":0}}`

func taskTestPlan(t *testing.T, model taskModel) tfsdk.Plan {
	t.Helper()
	var response resource.SchemaResponse
	(&taskResource{}).Schema(t.Context(), resource.SchemaRequest{}, &response)
	plan := tfsdk.Plan{Schema: response.Schema}
	if diags := plan.Set(t.Context(), model); diags.HasError() {
		t.Fatal(diags)
	}
	return plan
}

func taskTestState(t *testing.T) tfsdk.State {
	t.Helper()
	var task kaneoclient.Task
	if err := json.Unmarshal([]byte(taskFixture), &task); err != nil {
		t.Fatal(err)
	}
	plan := taskTestPlan(t, taskModelFromAPI(task, taskModel{}))
	return tfsdk.State(plan)
}

func taskTestConfig(t *testing.T, model taskModel) tfsdk.Config {
	t.Helper()
	var response datasource.SchemaResponse
	(&taskDataSource{}).Schema(t.Context(), datasource.SchemaRequest{}, &response)
	state := tfsdk.State{Schema: response.Schema}
	if diags := state.Set(t.Context(), model); diags.HasError() {
		t.Fatal(diags)
	}
	return tfsdk.Config{Schema: response.Schema, Raw: state.Raw}
}

func TestTaskResourceLifecycle(t *testing.T) {
	var remote map[string]any
	if err := json.Unmarshal([]byte(taskFixture), &remote); err != nil {
		t.Fatal(err)
	}
	var calls []string
	client := projectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch r.Method + " " + r.URL.Path {
		case "POST /task/project-1", "PUT /task/task-1":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			for _, field := range []string{"title", "description", "status", "priority"} {
				value, ok := body[field]
				if !ok {
					t.Errorf("missing %s", field)
				}
				remote[field] = value
			}
			for _, field := range []string{"userId", "startDate", "dueDate"} {
				remote[field] = body[field]
			}
			if r.Method == http.MethodPost {
				if body["startDate"] != "2026-09-01T08:00:00.120-04:00" || body["userId"] != "user-1" {
					t.Errorf("unexpected create body: %v", body)
				}
				if _, ok := body["position"]; ok {
					t.Error("create must not send a position")
				}
				remote["startDate"] = "2026-09-01T12:00:00.12Z"
			} else {
				if body["position"] != float64(16777217) || body["projectId"] != "project-1" {
					t.Errorf("update lost refreshed position or project: %v", body)
				}
				for _, field := range []string{"userId", "startDate", "dueDate"} {
					if _, ok := body[field]; ok {
						t.Errorf("clearing must omit %s", field)
					}
				}
				if body["description"] != "" {
					t.Error("description was not cleared")
				}
			}
		case "GET /task/task-1", "DELETE /task/task-1":
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewEncoder(w).Encode(remote); err != nil {
			t.Error(err)
		}
	})
	r := &taskResource{client: client}
	var model taskModel
	state := taskTestState(t)
	if diags := state.Get(t.Context(), &model); diags.HasError() {
		t.Fatal(diags)
	}
	model.StartDate = types.StringValue("2026-09-01T08:00:00.120-04:00")
	plan := taskTestPlan(t, model)
	created := resource.CreateResponse{State: tfsdk.State{Schema: plan.Schema}}
	r.Create(t.Context(), resource.CreateRequest{Plan: plan}, &created)
	if created.Diagnostics.HasError() {
		t.Fatal(created.Diagnostics)
	}
	var got taskModel
	if diags := created.State.Get(t.Context(), &got); diags.HasError() {
		t.Fatal(diags)
	}
	if got != model {
		t.Fatalf("created state = %+v, want %+v", got, model)
	}
	if got.Number.ValueInt64() != 16777217 || got.Position.ValueInt64() != 16777217 {
		t.Fatal("integer precision lost")
	}
	model.Title = types.StringValue("Renamed")
	model.Description = types.StringValue("")
	model.Status = types.StringValue("planned")
	model.Priority = types.StringValue("no-priority")
	model.AssigneeID, model.StartDate, model.DueDate = types.StringNull(), types.StringNull(), types.StringNull()
	model.Position = types.Int64Unknown()
	updated := resource.UpdateResponse{State: created.State}
	r.Update(t.Context(), resource.UpdateRequest{Plan: taskTestPlan(t, model), State: created.State}, &updated)
	if updated.Diagnostics.HasError() {
		t.Fatal(updated.Diagnostics)
	}
	if diags := updated.State.Get(t.Context(), &got); diags.HasError() {
		t.Fatal(diags)
	}
	model.Position = types.Int64Value(16777217)
	if got != model {
		t.Fatalf("updated state = %+v, want %+v", got, model)
	}
	imported := resource.ImportStateResponse{State: tfsdk.State{Schema: plan.Schema}}
	if diags := imported.State.Set(t.Context(), taskModel{}); diags.HasError() {
		t.Fatal(diags)
	}
	r.ImportState(t.Context(), resource.ImportStateRequest{ID: "task-1"}, &imported)
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
		t.Fatalf("import state = %+v, want %+v", got, model)
	}
	d := &taskDataSource{client: client}
	config := taskTestConfig(t, taskModel{ID: types.StringValue("task-1")})
	lookup := datasource.ReadResponse{State: tfsdk.State{Schema: config.Schema}}
	d.Read(t.Context(), datasource.ReadRequest{Config: config}, &lookup)
	if lookup.Diagnostics.HasError() {
		t.Fatal(lookup.Diagnostics)
	}
	if diags := lookup.State.Get(t.Context(), &got); diags.HasError() {
		t.Fatal(diags)
	}
	if got != model {
		t.Fatalf("data source state = %+v, want %+v", got, model)
	}
	deleted := resource.DeleteResponse{State: read.State}
	r.Delete(t.Context(), resource.DeleteRequest{State: read.State}, &deleted)
	if deleted.Diagnostics.HasError() {
		t.Fatal(deleted.Diagnostics)
	}
	if strings.Join(calls, ",") != "POST /task/project-1,PUT /task/task-1,GET /task/task-1,GET /task/task-1,DELETE /task/task-1" {
		t.Fatalf("unexpected calls: %v", calls)
	}
}

func TestTaskDateMapping(t *testing.T) {
	date, err := time.Parse(time.RFC3339Nano, "2026-09-01T12:00:00.123Z")
	if err != nil {
		t.Fatal(err)
	}
	remote := nullable.NewNullableWithValue(date)
	for _, value := range []string{"2026-09-01T12:00:00.123Z", "2026-09-01T08:00:00.123-04:00", "2026-09-01T12:00:00.123+00:00"} {
		prior := types.StringValue(value)
		if got := taskDateFromAPI(remote, prior); got != prior {
			t.Fatalf("equivalent date rewritten: %s", got)
		}
	}
	for _, prior := range []types.String{types.StringNull(), types.StringUnknown(), types.StringValue("2026-09-01T12:00:00.124Z"), types.StringValue("2026-09-02T12:00:00.123Z")} {
		if got := taskDateFromAPI(remote, prior); got.ValueString() != "2026-09-01T12:00:00.123Z" {
			t.Fatalf("date drift hidden: %s", got)
		}
	}
	for _, value := range []nullable.Nullable[time.Time]{nil, nullable.NewNullNullable[time.Time]()} {
		if !taskDateFromAPI(value, types.StringValue("2026-09-01T12:00:00.123Z")).IsNull() {
			t.Fatal("date deletion hidden")
		}
	}
	var task kaneoclient.Task
	if err := json.Unmarshal([]byte(`{"id":"task-1","projectId":"project-1","description":null,"position":null,"number":null,"userId":null}`), &task); err != nil {
		t.Fatal(err)
	}
	model := taskModelFromAPI(task, taskModel{})
	if !model.Position.IsNull() || !model.Number.IsNull() || !model.AssigneeID.IsNull() || !model.StartDate.IsNull() || model.Description.ValueString() != "" {
		t.Fatalf("unexpected null mapping: %+v", model)
	}
}

func TestTaskValidation(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		start, due, assignee types.String
		valid                bool
	}{
		{name: "unset", valid: true},
		{name: "unknown", start: types.StringUnknown(), due: types.StringUnknown(), assignee: types.StringUnknown(), valid: true},
		{name: "validOffset", start: types.StringValue("2026-09-01T08:00:00.120-04:00"), due: types.StringValue("2026-09-01T12:00:00.120Z"), valid: true},
		{name: "oneDate", due: types.StringValue("2026-09-01T12:00:00Z"), valid: true},
		{name: "unknownStart", start: types.StringUnknown(), due: types.StringValue("2026-09-01T12:00:00Z"), valid: true},
		{name: "reversed", start: types.StringValue("2026-09-02T12:00:00Z"), due: types.StringValue("2026-09-01T12:00:00Z")},
		{name: "fractionReversed", start: types.StringValue("2026-09-01T12:00:00.124Z"), due: types.StringValue("2026-09-01T12:00:00.123Z")},
		{name: "empty", start: types.StringValue("")},
		{name: "dateOnly", due: types.StringValue("2026-09-01")},
		{name: "invalidCalendar", start: types.StringValue("2026-02-30T12:00:00Z")},
		{name: "missingTimezone", start: types.StringValue("2026-09-01T12:00:00")},
		{name: "invalidOffsetHour", start: types.StringValue("2026-09-01T12:00:00+24:00")},
		{name: "invalidOffsetMinute", start: types.StringValue("2026-09-01T12:00:00+01:60")},
		{name: "microseconds", start: types.StringValue("2026-09-01T12:00:00.123456Z")},
		{name: "excessivePrecision", start: types.StringValue("2026-09-01T12:00:00.0000000001Z")},
		{name: "trimmedAssignee", assignee: types.StringValue(" user-1 ")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan := taskTestPlan(t, taskModel{StartDate: tc.start, DueDate: tc.due, AssigneeID: tc.assignee})
			response := resource.ValidateConfigResponse{}
			(&taskResource{}).ValidateConfig(t.Context(), resource.ValidateConfigRequest{Config: tfsdk.Config(plan)}, &response)
			if response.Diagnostics.HasError() == tc.valid {
				t.Fatalf("valid=%t, diagnostics=%v", tc.valid, response.Diagnostics)
			}
		})
	}
	var response resource.SchemaResponse
	(&taskResource{}).Schema(t.Context(), resource.SchemaRequest{}, &response)
	priority := response.Schema.Attributes["priority"].(resourceschema.StringAttribute)
	for _, value := range []string{"no-priority", "low", "medium", "high", "urgent", "invalid", ""} {
		var result validator.StringResponse
		priority.Validators[0].ValidateString(t.Context(), validator.StringRequest{ConfigValue: types.StringValue(value)}, &result)
		if result.Diagnostics.HasError() != (value == "invalid" || value == "") {
			t.Fatalf("priority %q: %v", value, result.Diagnostics)
		}
	}
}

func TestTaskImportInvalidIDs(t *testing.T) {
	for _, id := range []string{"", " ", "\t\n"} {
		response := resource.ImportStateResponse{}
		(&taskResource{}).ImportState(t.Context(), resource.ImportStateRequest{ID: id}, &response)
		if !response.Diagnostics.HasError() {
			t.Fatalf("expected import error for %q", id)
		}
	}
}

func TestTaskMutationErrors(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
	}{
		{400, `{"message":"Invalid task"}`}, {401, `{}`}, {403, `{}`}, {500, `{}`},
		{200, `null`}, {200, `{}`}, {200, `{`}, {200, strings.Replace(taskFixture, "project-1", "project-2", 1)},
	} {
		t.Run(fmt.Sprintf("%d/%s", tc.status, tc.body), func(t *testing.T) {
			client := projectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				if _, err := fmt.Fprint(w, tc.body); err != nil {
					t.Error(err)
				}
			})
			r := &taskResource{client: client}
			state := taskTestState(t)
			var model taskModel
			if diags := state.Get(t.Context(), &model); diags.HasError() {
				t.Fatal(diags)
			}
			plan := taskTestPlan(t, model)
			created := resource.CreateResponse{State: tfsdk.State{Schema: state.Schema}}
			r.Create(t.Context(), resource.CreateRequest{Plan: plan}, &created)
			if !created.Diagnostics.HasError() {
				t.Fatal("expected create error")
			}
			updated := resource.UpdateResponse{State: state}
			r.Update(t.Context(), resource.UpdateRequest{Plan: plan, State: state}, &updated)
			if !updated.Diagnostics.HasError() || !updated.State.Raw.Equal(state.Raw) {
				t.Fatalf("update lost prior state: %v", updated.Diagnostics)
			}
		})
	}
	// A nullable legacy position cannot be preserved by the full update endpoint.
	state := taskTestState(t)
	var model taskModel
	if diags := state.Get(t.Context(), &model); diags.HasError() {
		t.Fatal(diags)
	}
	model.Position = types.Int64Null()
	if diags := state.Set(t.Context(), model); diags.HasError() {
		t.Fatal(diags)
	}
	response := resource.UpdateResponse{State: state}
	(&taskResource{}).Update(t.Context(), resource.UpdateRequest{Plan: taskTestPlan(t, model), State: state}, &response)
	if !response.Diagnostics.HasError() {
		t.Fatal("expected missing position error before making an API call")
	}
}

func TestTaskMissingAndReadErrors(t *testing.T) {
	withTask := func(group string) string {
		board := strings.Replace(emptyTaskBoard, `"`+group+`":[]`, `"`+group+`":[`+taskFixture+`]`, 1)
		return strings.Replace(board, `"total":0`, `"total":1`, 1)
	}
	for _, tc := range []struct {
		name        string
		status      int
		body        string
		boardStatus int
		board       string
		absent      bool
	}{
		{"missing400", 400, `{}`, 200, emptyTaskBoard, true},
		{"missing404", 404, `{}`, 200, emptyTaskBoard, true},
		{"exists", 400, `{}`, 200, withTask("tasks"), false},
		{"archivedExists", 400, `{}`, 200, withTask("archivedTasks"), false},
		{"plannedExists", 404, `{}`, 200, withTask("plannedTasks"), false},
		{"forbidden", 403, `{}`, 0, "", false},
		{"unauthorized", 401, `{}`, 0, "", false},
		{"serverError", 500, `{}`, 0, "", false},
		{"boardForbidden", 400, `{}`, 403, `{}`, false},
		{"boardServerError", 400, `{}`, 500, `{}`, false},
		{"missingProject", 400, `{}`, 400, `{}`, false},
		{"nullBoard", 400, `{}`, 200, `null`, false},
		{"invalidBoard", 400, `{}`, 200, `{`, false},
		{"incompleteBoard", 400, `{}`, 200, `{"data":{"id":"project-1"}}`, false},
		{"wrongProject", 400, `{}`, 200, strings.Replace(emptyTaskBoard, "project-1", "project-2", 1), false},
		{"paginated", 400, `{}`, 200, strings.Replace(emptyTaskBoard, `"totalPages":1`, `"totalPages":2`, 1), false},
		{"wrongCount", 400, `{}`, 200, strings.Replace(emptyTaskBoard, `"total":0`, `"total":1`, 1), false},
		{"invalidTask", 400, `{}`, 200, strings.Replace(withTask("tasks"), `"id":"task-1"`, `"id":""`, 1), false},
		{"duplicateTask", 400, `{}`, 200, strings.Replace(withTask("tasks"), `"archivedTasks":[]`, `"archivedTasks":[`+taskFixture+`]`, 1), false},
		{"nullTask", 200, `null`, 0, "", false},
		{"invalidJSON", 200, `{`, 0, "", false},
		{"wrongID", 200, strings.Replace(taskFixture, "task-1", "task-2", 1), 0, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := projectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/task/tasks/project-1" {
					if tc.boardStatus == 0 {
						t.Error("unexpected board lookup")
						w.WriteHeader(500)
						return
					}
					if r.URL.RawQuery != "" {
						t.Errorf("absence lookup must be unfiltered and unpaginated: %s", r.URL.RawQuery)
					}
					w.WriteHeader(tc.boardStatus)
					if _, err := fmt.Fprint(w, tc.board); err != nil {
						t.Error(err)
					}
					return
				}
				w.WriteHeader(tc.status)
				if _, err := fmt.Fprint(w, tc.body); err != nil {
					t.Error(err)
				}
			})
			r := &taskResource{client: client}
			state := taskTestState(t)
			read := resource.ReadResponse{State: state}
			r.Read(t.Context(), resource.ReadRequest{State: state}, &read)
			if read.Diagnostics.HasError() == tc.absent || read.State.Raw.IsNull() != tc.absent {
				t.Fatalf("read removed=%t, diagnostics=%v", read.State.Raw.IsNull(), read.Diagnostics)
			}
			if tc.status != 200 {
				deleted := resource.DeleteResponse{State: state}
				r.Delete(t.Context(), resource.DeleteRequest{State: state}, &deleted)
				if deleted.Diagnostics.HasError() == tc.absent {
					t.Fatalf("delete diagnostics: %v", deleted.Diagnostics)
				}
			}
			d := &taskDataSource{client: client}
			config := taskTestConfig(t, taskModel{ID: types.StringValue("task-1")})
			lookup := datasource.ReadResponse{State: tfsdk.State{Schema: config.Schema}}
			d.Read(t.Context(), datasource.ReadRequest{Config: config}, &lookup)
			if !lookup.Diagnostics.HasError() {
				t.Fatal("expected data source error")
			}
		})
	}
}
