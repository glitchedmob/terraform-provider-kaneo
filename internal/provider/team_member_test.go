// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const teamMemberFixture = `{"id":"row","teamId":"team","userId":"user"}`

func teamMemberTestModel() teamMemberModel {
	return teamMemberModel{ID: types.StringValue("ws/team/user"), WorkspaceID: types.StringValue("ws"), TeamID: types.StringValue("team"), UserID: types.StringValue("user")}
}
func TestTeamMemberLifecycle(t *testing.T) {
	present := false
	posts := 0
	r := &teamMemberResource{client: testClient(t, func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/auth/organization/list-teams":
			if req.Method != "GET" || req.URL.RawQuery != "organizationId=ws" {
				t.Error("team scope")
			}
			writeTeamFixture(t, w, "["+teamFixture+"]")
		case "/auth/organization/list-team-members":
			if req.Method != "GET" || req.URL.RawQuery != "teamId=team" {
				t.Error("member scope")
			}
			if present {
				writeTeamFixture(t, w, "["+teamMemberFixture+"]")
			} else {
				writeTeamFixture(t, w, `[]`)
			}
		case "/auth/organization/add-team-member", "/auth/organization/remove-team-member":
			posts++
			var body map[string]string
			if !testDecodeRequest(t, w, req, &body) {
				return
			}
			if req.Method != "POST" || len(body) != 3 || body["organizationId"] != "ws" || body["teamId"] != "team" || body["userId"] != "user" {
				t.Errorf("unscoped mutation: %v", body)
			}
			present = strings.HasSuffix(req.URL.Path, "add-team-member")
			if present {
				writeTeamFixture(t, w, teamMemberFixture)
			} else {
				writeTeamFixture(t, w, `{"message":"Team member removed successfully."}`)
			}
		default:
			t.Errorf("unexpected request %s", req.URL)
		}
	})}
	plan := testPlan(t, r, teamMemberTestModel())
	created := resource.CreateResponse{State: tfsdk.State{Schema: plan.Schema}}
	r.Create(t.Context(), resource.CreateRequest{Plan: plan}, &created)
	if created.Diagnostics.HasError() || posts != 1 {
		t.Fatal(created.Diagnostics)
	}
	duplicate := resource.CreateResponse{State: tfsdk.State{Schema: plan.Schema}}
	r.Create(t.Context(), resource.CreateRequest{Plan: plan}, &duplicate)
	if !duplicate.Diagnostics.HasError() || posts != 1 {
		t.Fatal("adopted existing membership")
	}
	imported := resource.ImportStateResponse{State: tfsdk.State(testPlan(t, r, teamMemberModel{}))}
	r.ImportState(t.Context(), resource.ImportStateRequest{ID: "ws/team/user"}, &imported)
	read := resource.ReadResponse{State: imported.State}
	r.Read(t.Context(), resource.ReadRequest{State: imported.State}, &read)
	if read.Diagnostics.HasError() || !read.State.Raw.Equal(created.State.Raw) {
		t.Fatal(read.Diagnostics)
	}
	deleted := resource.DeleteResponse{State: read.State}
	r.Delete(t.Context(), resource.DeleteRequest{State: read.State}, &deleted)
	if deleted.Diagnostics.HasError() || present || posts != 2 {
		t.Fatal(deleted.Diagnostics)
	}
	r.Read(t.Context(), resource.ReadRequest{State: read.State}, &read)
	if read.Diagnostics.HasError() || !read.State.Raw.IsNull() {
		t.Fatal("absence not reconciled")
	}
}
func TestTeamMemberObservation(t *testing.T) {
	for _, tc := range []struct {
		name, body         string
		status             int
		session            string
		freshStatus        int
		freshBody          string
		wantError, missing bool
	}{
		{name: "present", body: "[" + teamMemberFixture + "]"},
		{name: "absent", body: `[]`, missing: true},
		{name: "null", body: `null`, wantError: true},
		{name: "missing", body: ``, wantError: true},
		{name: "object", body: `{}`, wantError: true},
		{name: "malformed", body: `[`, wantError: true},
		{name: "invalid tail", body: "[" + teamMemberFixture + ",{}]", wantError: true},
		{name: "wrong scope", body: `[{"id":"row","teamId":"other","userId":"user"}]`, wantError: true},
		{name: "duplicate row", body: "[" + teamMemberFixture + `,{"id":"row","teamId":"team","userId":"other"}]`, wantError: true},
		{name: "duplicate user", body: "[" + teamMemberFixture + `,{"id":"other","teamId":"team","userId":"user"}]`, wantError: true},
		{name: "401", status: 401, body: `{}`, wantError: true},
		{name: "403", status: 403, body: `{}`, wantError: true},
		{name: "404", status: 404, body: `{}`, wantError: true},
		{name: "500", status: 500, body: `{}`, wantError: true},
		{name: "self absent", status: 400, body: `{"code":"USER_IS_NOT_A_MEMBER_OF_THE_TEAM"}`, session: `{"user":{"id":"user"},"session":{"token":"SECRET"}}`, missing: true},
		{name: "nonself denied", status: 400, body: `{"code":"USER_IS_NOT_A_MEMBER_OF_THE_TEAM"}`, session: `{"user":{"id":"operator"}}`, wantError: true},
		{name: "no identity", status: 400, body: `{"code":"USER_IS_NOT_A_MEMBER_OF_THE_TEAM"}`, session: `{"session":{"token":"SECRET"}}`, wantError: true},
		{name: "malformed identity", status: 400, body: `{"code":"USER_IS_NOT_A_MEMBER_OF_THE_TEAM"}`, session: `{"user":{"id":123},"session":{"token":"SECRET"}}`, wantError: true},
		{name: "self workspace access lost", status: 400, body: `{"code":"USER_IS_NOT_A_MEMBER_OF_THE_TEAM"}`, session: `{"user":{"id":"user"}}`, freshStatus: 403, freshBody: `{}`, wantError: true},
		{name: "self team gone", status: 400, body: `{"code":"USER_IS_NOT_A_MEMBER_OF_THE_TEAM"}`, session: `{"user":{"id":"user"}}`, freshBody: `[]`, missing: true},
		{name: "team deletion race", status: 400, body: `{"code":"TEAM_NOT_FOUND"}`, freshBody: `[]`, missing: true},
		{name: "unproven team missing", status: 400, body: `{"code":"TEAM_NOT_FOUND"}`, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lists := 0
			r := &teamMemberResource{client: testClient(t, func(w http.ResponseWriter, req *http.Request) {
				if req.Method != "GET" {
					t.Error("unexpected mutation")
				}
				switch req.URL.Path {
				case "/auth/organization/list-teams":
					lists++
					if lists > 1 && tc.freshStatus != 0 {
						w.WriteHeader(tc.freshStatus)
						writeTeamFixture(t, w, tc.freshBody)
					} else if lists > 1 && tc.freshBody != "" {
						writeTeamFixture(t, w, tc.freshBody)
					} else {
						writeTeamFixture(t, w, "["+teamFixture+"]")
					}
				case "/auth/organization/get-full-organization":
					w.WriteHeader(403)
					writeTeamFixture(t, w, `{}`)
				case "/auth/get-session":
					writeTeamFixture(t, w, tc.session)
				case "/auth/organization/list-team-members":
					if tc.status != 0 {
						w.WriteHeader(tc.status)
					}
					writeTeamFixture(t, w, tc.body)
				default:
					t.Errorf("unexpected %s", req.URL)
				}
			})}
			state := tfsdk.State(testPlan(t, r, teamMemberTestModel()))
			read := resource.ReadResponse{State: state}
			r.Read(t.Context(), resource.ReadRequest{State: state}, &read)
			if read.Diagnostics.HasError() != tc.wantError || read.State.Raw.IsNull() != tc.missing {
				t.Fatalf("%v null=%v", read.Diagnostics, read.State.Raw.IsNull())
			}
			if tc.wantError && !read.State.Raw.Equal(state.Raw) {
				t.Error("state lost")
			}
			if strings.Contains(fmt.Sprint(read.Diagnostics), "SECRET") {
				t.Fatal("session leaked")
			}
		})
	}
}
func TestTeamMemberListCap(t *testing.T) {
	for _, n := range []int{99, 100, 101} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			rows := make([]string, n)
			for i := range rows {
				rows[i] = fmt.Sprintf(`{"id":"r%d","teamId":"team","userId":"u%d"}`, i, i)
			}
			c := testClient(t, func(w http.ResponseWriter, req *http.Request) {
				if strings.HasSuffix(req.URL.Path, "list-teams") {
					writeTeamFixture(t, w, "["+teamFixture+"]")
				} else {
					writeTeamFixture(t, w, "["+strings.Join(rows, ",")+"]")
				}
			})
			_, _, err := observeTeamMember(t.Context(), c, "ws", "team", "user")
			if (err != nil) != (n >= 100) {
				t.Fatal(err)
			}
		})
	}
}
func TestTeamMemberMutationFailures(t *testing.T) {
	for _, operation := range []string{"add", "remove"} {
		for _, tc := range []struct {
			name, body string
			status     int
			gone       bool
		}{
			{"pending or orphan", `{"code":"USER_IS_NOT_A_MEMBER_OF_THE_ORGANIZATION"}`, 400, false},
			{"denied", `{}`, 403, false}, {"server", `{}`, 500, false}, {"malformed", `!`, 200, false}, {"empty", `{}`, 200, false}, {"null", `null`, 200, false},
			{"wrong user", `{"id":"row","teamId":"team","userId":"other"}`, 200, false},
			{"wrong team", `{"id":"row","teamId":"other","userId":"user"}`, 200, false},
			{"wrong message", `{"message":"removed"}`, 200, false},
			{"ambiguous absence", `{"code":"USER_IS_NOT_A_MEMBER_OF_THE_TEAM"}`, 400, true},
		} {
			t.Run(operation+"/"+tc.name, func(t *testing.T) {
				mutated := false
				r := &teamMemberResource{client: testClient(t, func(w http.ResponseWriter, req *http.Request) {
					if req.Method == "POST" {
						mutated = true
						w.WriteHeader(tc.status)
						writeTeamFixture(t, w, tc.body)
						return
					}
					if strings.HasSuffix(req.URL.Path, "list-teams") {
						writeTeamFixture(t, w, "["+teamFixture+"]")
						return
					}
					if operation == "remove" && (!mutated || !tc.gone) {
						writeTeamFixture(t, w, "["+teamMemberFixture+"]")
					} else {
						writeTeamFixture(t, w, `[]`)
					}
				})}
				plan := testPlan(t, r, teamMemberTestModel())
				state := tfsdk.State(plan)
				if operation == "add" {
					resp := resource.CreateResponse{State: tfsdk.State{Schema: plan.Schema}}
					r.Create(t.Context(), resource.CreateRequest{Plan: plan}, &resp)
					if !resp.Diagnostics.HasError() {
						t.Fatal("accepted invalid add")
					}
				} else {
					resp := resource.DeleteResponse{State: state}
					r.Delete(t.Context(), resource.DeleteRequest{State: state}, &resp)
					if resp.Diagnostics.HasError() == tc.gone || !resp.State.Raw.Equal(state.Raw) {
						t.Fatalf("%v", resp.Diagnostics)
					}
				}
				if !mutated {
					t.Fatal("mutation not tested")
				}
			})
		}
	}
}
func TestTeamMemberImport(t *testing.T) {
	for _, id := range []string{"", "ws/team", "ws/team/user/extra", "/team/user", "ws//user", "ws/team/", "ws/team/%", "ws/team/%75ser", "ws/team/a%2fb", "ws/team/a b", "ws/team/a%20b"} {
		if _, _, _, err := parseTeamMemberID(id); err == nil {
			t.Errorf("accepted %q", id)
		}
	}
	id := teamMemberID("w/s", "t%", "u/s")
	ws, team, user, err := parseTeamMemberID(id)
	if err != nil || ws != "w/s" || team != "t%" || user != "u/s" {
		t.Fatal(id, err)
	}
}
