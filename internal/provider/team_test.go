// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const teamFixture = `{"id":"team","organizationId":"ws","name":"test"}`

func teamTestModel() teamModel {
	return teamModel{ID: types.StringValue("team"), WorkspaceID: types.StringValue("ws"), Name: types.StringValue("test")}
}
func writeTeamFixture(t *testing.T, w http.ResponseWriter, body string) {
	t.Helper()
	if _, err := fmt.Fprint(w, body); err != nil {
		t.Error(err)
	}
}
func TestTeamLifecycle(t *testing.T) {
	name := "test"
	r := &teamResource{client: testClient(t, func(w http.ResponseWriter, req *http.Request) {
		if req.Method == "GET" {
			if req.URL.Path != "/auth/organization/list-teams" || req.URL.Query().Get("organizationId") != "ws" {
				t.Error("missing scoped list")
			}
			writeTeamFixture(t, w, fmt.Sprintf(`[{"id":"seed","organizationId":"ws","name":"default"},{"id":"team","organizationId":"ws","name":%q}]`, name))
			return
		}
		var body map[string]any
		if !testDecodeRequest(t, w, req, &body) {
			return
		}
		switch req.URL.Path {
		case "/auth/organization/create-team":
			if body["organizationId"] != "ws" || body["name"] != "test" || len(body) != 2 {
				t.Errorf("create: %v", body)
			}
		case "/auth/organization/update-team":
			data := body["data"].(map[string]any)
			if body["teamId"] != "team" || data["organizationId"] != "ws" || len(data) != 2 || len(body) != 2 {
				t.Errorf("update: %v", body)
			}
			name = data["name"].(string)
		case "/auth/organization/remove-team":
			if body["organizationId"] != "ws" || body["teamId"] != "team" || len(body) != 2 {
				t.Errorf("delete: %v", body)
			}
			writeTeamFixture(t, w, `{"message":"Team removed successfully."}`)
			return
		default:
			t.Errorf("unexpected %s", req.URL.Path)
		}
		writeTeamFixture(t, w, fmt.Sprintf(`{"id":"team","organizationId":"ws","name":%q}`, name))
	})}
	model := teamTestModel()
	plan := testPlan(t, r, model)
	created := resource.CreateResponse{State: tfsdk.State{Schema: plan.Schema}}
	r.Create(t.Context(), resource.CreateRequest{Plan: plan}, &created)
	if created.Diagnostics.HasError() {
		t.Fatal(created.Diagnostics)
	}
	model.Name = types.StringValue("renamed")
	updated := resource.UpdateResponse{State: created.State}
	r.Update(t.Context(), resource.UpdateRequest{Plan: testPlan(t, r, model), State: created.State}, &updated)
	if updated.Diagnostics.HasError() {
		t.Fatal(updated.Diagnostics)
	}
	imported := resource.ImportStateResponse{State: tfsdk.State(testPlan(t, r, teamModel{}))}
	r.ImportState(t.Context(), resource.ImportStateRequest{ID: "ws/team"}, &imported)
	if imported.Diagnostics.HasError() {
		t.Fatal(imported.Diagnostics)
	}
	read := resource.ReadResponse{State: imported.State}
	r.Read(t.Context(), resource.ReadRequest{State: imported.State}, &read)
	if read.Diagnostics.HasError() || !read.State.Raw.Equal(updated.State.Raw) {
		t.Fatalf("import/read mismatch: %v", read.Diagnostics)
	}
	name = "drift"
	r.Read(t.Context(), resource.ReadRequest{State: read.State}, &read)
	var got teamModel
	read.State.Get(t.Context(), &got)
	if read.Diagnostics.HasError() || got.Name.ValueString() != "drift" {
		t.Fatalf("drift: %v %+v", read.Diagnostics, got)
	}
	deleted := resource.DeleteResponse{State: read.State}
	r.Delete(t.Context(), resource.DeleteRequest{State: read.State}, &deleted)
	if deleted.Diagnostics.HasError() {
		t.Fatal(deleted.Diagnostics)
	}
}
func TestTeamLookupFailures(t *testing.T) {
	capped := make([]string, 100)
	for i := range capped {
		capped[i] = fmt.Sprintf(`{"id":"t%d","organizationId":"ws","name":""}`, i)
	}
	for _, tc := range []struct {
		name               string
		status             int
		body               string
		presenceStatus     int
		presence           string
		missing, wantError bool
	}{
		{"absent", 200, `[]`, 0, "", true, false},
		{"other team", 200, `[{"id":"seed","organizationId":"ws","name":""}]`, 0, "", true, false},
		{"parent absent", 403, `{}`, 400, `{"code":"ORGANIZATION_NOT_FOUND"}`, true, false},
		{"read denied", 403, `{}`, 403, `{}`, false, true},
		{"denied with parent", 403, `{}`, 200, `{"id":"ws"}`, false, true},
		{"invalid parent", 403, `{}`, 200, `{"id":"other"}`, false, true},
		{"parent server error", 403, `{}`, 500, `{}`, false, true},
		{"unauthenticated", 401, `{}`, 0, "", false, true},
		{"generic not found", 404, `{}`, 0, "", false, true},
		{"unproven parent", 400, `{"code":"ORGANIZATION_NOT_FOUND"}`, 0, "", false, true},
		{"server", 500, `{}`, 0, "", false, true},
		{"object", 200, `{}`, 0, "", false, true},
		{"null", 200, `null`, 0, "", false, true},
		{"malformed json", 200, `[`, 0, "", false, true},
		{"missing id", 200, `[{"organizationId":"ws","name":"test"}]`, 0, "", false, true},
		{"missing name", 200, `[{"id":"team","organizationId":"ws"}]`, 0, "", false, true},
		{"null name", 200, `[{"id":"team","organizationId":"ws","name":null}]`, 0, "", false, true},
		{"wrong scope", 200, `[{"id":"team","organizationId":"other","name":"test"}]`, 0, "", false, true},
		{"invalid tail after match", 200, `[` + teamFixture + `,{}]`, 0, "", false, true},
		{"duplicate target", 200, `[` + teamFixture + `,` + teamFixture + `]`, 0, "", false, true},
		{"duplicate unrelated", 200, `[` + teamFixture + `,{"id":"seed","organizationId":"ws","name":"x"},{"id":"seed","organizationId":"ws","name":"y"}]`, 0, "", false, true},
		{"list capped", 200, "[" + strings.Join(capped, ",") + "]", 0, "", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &teamResource{client: testClient(t, func(w http.ResponseWriter, req *http.Request) {
				if req.Method != "GET" {
					t.Error("must not mutate after absence or lookup failure")
				}
				if req.URL.Query().Get("organizationId") != "ws" {
					t.Error("missing scope")
				}
				if strings.HasSuffix(req.URL.Path, "get-full-organization") {
					if tc.presenceStatus == 0 {
						t.Error("unexpected presence probe")
						w.WriteHeader(500)
						return
					}
					w.WriteHeader(tc.presenceStatus)
					writeTeamFixture(t, w, tc.presence)
					return
				}
				w.WriteHeader(tc.status)
				writeTeamFixture(t, w, tc.body)
			})}
			state := tfsdk.State(testPlan(t, r, teamTestModel()))
			read := resource.ReadResponse{State: state}
			r.Read(t.Context(), resource.ReadRequest{State: state}, &read)
			if read.Diagnostics.HasError() != tc.wantError || read.State.Raw.IsNull() != tc.missing {
				t.Fatalf("read: %v null=%v", read.Diagnostics, read.State.Raw.IsNull())
			}
			if tc.wantError && !read.State.Raw.Equal(state.Raw) {
				t.Error("read changed state on failure")
			}
			deleted := resource.DeleteResponse{State: state}
			r.Delete(t.Context(), resource.DeleteRequest{State: state}, &deleted)
			if deleted.Diagnostics.HasError() != tc.wantError || !deleted.State.Raw.Equal(state.Raw) {
				t.Fatalf("delete: %v", deleted.Diagnostics)
			}
		})
	}
}
func TestTeamMutationFailures(t *testing.T) {
	for _, operation := range []string{"create", "update", "delete"} {
		for _, tc := range []struct {
			name   string
			status int
			body   string
		}{
			{"forbidden", 403, `{}`}, {"server", 500, `{}`}, {"last team", 400, `{"code":"UNABLE_TO_REMOVE_LAST_TEAM"}`},
			{"maximum teams", 400, `{"code":"YOU_HAVE_REACHED_THE_MAXIMUM_NUMBER_OF_TEAMS"}`},
			{"team not found but still listed", 400, `{"code":"TEAM_NOT_FOUND"}`},
			{"parent not found but still listed", 400, `{"code":"ORGANIZATION_NOT_FOUND"}`},
			{"malformed", 200, `{}`}, {"null", 200, `null`}, {"invalid json", 200, `!`},
			{"wrong scope", 200, `{"id":"team","organizationId":"other","name":"test"}`},
			{"wrong name", 200, `{"id":"team","organizationId":"ws","name":"wrong"}`},
			{"empty id", 200, `{"id":"","organizationId":"ws","name":"test"}`},
			{"unconfirmed", 200, `{"message":"wrong"}`},
		} {
			t.Run(operation+"/"+tc.name, func(t *testing.T) {
				r := &teamResource{client: testClient(t, func(w http.ResponseWriter, req *http.Request) {
					if req.Method == "GET" {
						writeTeamFixture(t, w, "["+teamFixture+"]")
						return
					}
					w.WriteHeader(tc.status)
					writeTeamFixture(t, w, tc.body)
				})}
				plan := testPlan(t, r, teamTestModel())
				state := tfsdk.State(plan)
				switch operation {
				case "create":
					resp := resource.CreateResponse{State: tfsdk.State{Schema: plan.Schema}}
					r.Create(t.Context(), resource.CreateRequest{Plan: plan}, &resp)
					if !resp.Diagnostics.HasError() {
						t.Fatal("create swallowed failure")
					}
				case "update":
					resp := resource.UpdateResponse{State: state}
					r.Update(t.Context(), resource.UpdateRequest{Plan: plan, State: state}, &resp)
					if !resp.Diagnostics.HasError() || !resp.State.Raw.Equal(state.Raw) {
						t.Fatal("update swallowed failure or changed state")
					}
				case "delete":
					resp := resource.DeleteResponse{State: state}
					r.Delete(t.Context(), resource.DeleteRequest{State: state}, &resp)
					if !resp.Diagnostics.HasError() || !resp.State.Raw.Equal(state.Raw) {
						t.Fatal("delete swallowed failure or changed state")
					}
				}
			})
		}
	}
}
func TestTeamUpdateRejectsWrongIdentity(t *testing.T) {
	r := &teamResource{client: testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		writeTeamFixture(t, w, `{"id":"other","organizationId":"ws","name":"test"}`)
	})}
	plan := testPlan(t, r, teamTestModel())
	state := tfsdk.State(plan)
	resp := resource.UpdateResponse{State: state}
	r.Update(t.Context(), resource.UpdateRequest{Plan: plan, State: state}, &resp)
	if !resp.Diagnostics.HasError() || !resp.State.Raw.Equal(state.Raw) {
		t.Fatal("wrong identity accepted")
	}
}
func TestTeamTransportFailures(t *testing.T) {
	r := &teamResource{client: testClient(t, func(_ http.ResponseWriter, _ *http.Request) { t.Error("canceled request reached server") })}
	plan := testPlan(t, r, teamTestModel())
	state := tfsdk.State(plan)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	created := resource.CreateResponse{State: tfsdk.State{Schema: plan.Schema}}
	r.Create(ctx, resource.CreateRequest{Plan: plan}, &created)
	if !created.Diagnostics.HasError() {
		t.Fatal("create swallowed transport failure")
	}
	read := resource.ReadResponse{State: state}
	r.Read(ctx, resource.ReadRequest{State: state}, &read)
	if !read.Diagnostics.HasError() || !read.State.Raw.Equal(state.Raw) {
		t.Fatal("read swallowed transport failure or lost state")
	}
	updated := resource.UpdateResponse{State: state}
	r.Update(ctx, resource.UpdateRequest{Plan: plan, State: state}, &updated)
	if !updated.Diagnostics.HasError() || !updated.State.Raw.Equal(state.Raw) {
		t.Fatal("update swallowed transport failure or lost state")
	}
	deleted := resource.DeleteResponse{State: state}
	r.Delete(ctx, resource.DeleteRequest{State: state}, &deleted)
	if !deleted.Diagnostics.HasError() || !deleted.State.Raw.Equal(state.Raw) {
		t.Fatal("delete swallowed transport failure or lost state")
	}
}

func TestTeamImportValidation(t *testing.T) {
	for _, id := range []string{"", "team", "/team", "ws/", " /team", "ws/ ", "ws/team/extra"} {
		resp := resource.ImportStateResponse{}
		(&teamResource{}).ImportState(t.Context(), resource.ImportStateRequest{ID: id}, &resp)
		if !resp.Diagnostics.HasError() {
			t.Errorf("accepted %q", id)
		}
	}
}
func TestTeamDeleteRace(t *testing.T) {
	for _, parentGone := range []bool{false, true} {
		t.Run(fmt.Sprint(parentGone), func(t *testing.T) {
			mutated := false
			r := &teamResource{client: testClient(t, func(w http.ResponseWriter, req *http.Request) {
				if req.Method == "POST" {
					mutated = true
					w.WriteHeader(400)
					writeTeamFixture(t, w, `{"code":"TEAM_NOT_FOUND"}`)
					return
				}
				if !mutated {
					writeTeamFixture(t, w, "["+teamFixture+"]")
					return
				}
				if !parentGone {
					writeTeamFixture(t, w, `[]`)
					return
				}
				if strings.HasSuffix(req.URL.Path, "get-full-organization") {
					w.WriteHeader(400)
					writeTeamFixture(t, w, `{"code":"ORGANIZATION_NOT_FOUND"}`)
					return
				}
				w.WriteHeader(403)
				writeTeamFixture(t, w, `{}`)
			})}
			state := tfsdk.State(testPlan(t, r, teamTestModel()))
			resp := resource.DeleteResponse{State: state}
			r.Delete(t.Context(), resource.DeleteRequest{State: state}, &resp)
			if resp.Diagnostics.HasError() || !mutated {
				t.Fatal(resp.Diagnostics)
			}
		})
	}
}
