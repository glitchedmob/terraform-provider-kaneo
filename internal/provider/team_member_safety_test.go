// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestTeamMemberSchema(t *testing.T) {
	r := &teamMemberResource{}
	var resp resource.SchemaResponse
	r.Schema(t.Context(), resource.SchemaRequest{}, &resp)
	if len(resp.Schema.Attributes) != 4 {
		t.Fatal("unexpected public attributes")
	}
	for _, name := range []string{"workspace_id", "team_id", "user_id"} {
		attr := resp.Schema.Attributes[name].(schema.StringAttribute)
		if !attr.Required || attr.Computed || attr.Optional || len(attr.PlanModifiers) != 1 {
			t.Fatal("identifiers must require replacement")
		}
		for _, value := range []string{"", " ", "a b", "a\tb", "a\vb", "a\u0085b", "a\u2003b", "native-id", "w/s"} {
			result := validator.StringResponse{}
			for _, v := range attr.Validators {
				v.ValidateString(t.Context(), validator.StringRequest{ConfigValue: types.StringValue(value)}, &result)
			}
			if result.Diagnostics.HasError() == validTeamMemberID(value) {
				t.Fatalf("invalid validation for %q", value)
			}
		}
		model := teamMemberTestModel()
		state := tfsdk.State(testPlan(t, r, model))
		plan := testPlan(t, r, model)
		result := planmodifier.StringResponse{}
		attr.PlanModifiers[0].PlanModifyString(t.Context(), planmodifier.StringRequest{Path: path.Root(name), State: state, Plan: plan, StateValue: types.StringValue("old"), PlanValue: types.StringValue("new"), ConfigValue: types.StringValue("new")}, &result)
		if !result.RequiresReplace || result.Diagnostics.HasError() {
			t.Fatalf("%s does not replace", name)
		}
	}
	updated := resource.UpdateResponse{}
	r.Update(t.Context(), resource.UpdateRequest{}, &updated)
	if !updated.Diagnostics.HasError() {
		t.Fatal("in-place update allowed")
	}
	imported := resource.ImportStateResponse{}
	r.ImportState(t.Context(), resource.ImportStateRequest{ID: "invalid"}, &imported)
	if !imported.Diagnostics.HasError() {
		t.Fatal("invalid import allowed")
	}
}
func TestTeamMemberParentAbsence(t *testing.T) {
	for _, gone := range []string{"team", "workspace", "foreign"} {
		t.Run(gone, func(t *testing.T) {
			r := &teamMemberResource{client: testClient(t, func(w http.ResponseWriter, req *http.Request) {
				if req.Method != "GET" {
					t.Fatal("mutated missing parent")
				}
				if strings.HasSuffix(req.URL.Path, "get-full-organization") {
					w.WriteHeader(400)
					writeTeamFixture(t, w, `{"code":"ORGANIZATION_NOT_FOUND"}`)
					return
				}
				if !strings.HasSuffix(req.URL.Path, "list-teams") {
					t.Fatal("listed members of absent scoped team")
				}
				switch gone {
				case "team":
					writeTeamFixture(t, w, `[]`)
				case "workspace":
					w.WriteHeader(403)
					writeTeamFixture(t, w, `{}`)
				case "foreign":
					writeTeamFixture(t, w, `[{"id":"other","organizationId":"ws","name":"other team"}]`)
				}
			})}
			plan := testPlan(t, r, teamMemberTestModel())
			state := tfsdk.State(plan)
			read := resource.ReadResponse{State: state}
			r.Read(t.Context(), resource.ReadRequest{State: state}, &read)
			if read.Diagnostics.HasError() || !read.State.Raw.IsNull() {
				t.Fatal(read.Diagnostics)
			}
			created := resource.CreateResponse{State: tfsdk.State{Schema: plan.Schema}}
			r.Create(t.Context(), resource.CreateRequest{Plan: plan}, &created)
			if !created.Diagnostics.HasError() {
				t.Fatal("created in absent parent")
			}
			deleted := resource.DeleteResponse{State: state}
			r.Delete(t.Context(), resource.DeleteRequest{State: state}, &deleted)
			if deleted.Diagnostics.HasError() {
				t.Fatal(deleted.Diagnostics)
			}
		})
	}
}
func TestTeamMemberConfirmationRetainsState(t *testing.T) {
	for _, body := range []string{`[]`, `null`, `[` + teamMemberFixture + `,` + teamMemberFixture + `]`} {
		t.Run(body, func(t *testing.T) {
			added := false
			r := &teamMemberResource{client: testClient(t, func(w http.ResponseWriter, req *http.Request) {
				if req.Method == "POST" {
					added = true
					writeTeamFixture(t, w, teamMemberFixture)
					return
				}
				if strings.HasSuffix(req.URL.Path, "list-teams") {
					writeTeamFixture(t, w, "["+teamFixture+"]")
					return
				}
				if added {
					writeTeamFixture(t, w, body)
				} else {
					writeTeamFixture(t, w, `[]`)
				}
			})}
			plan := testPlan(t, r, teamMemberTestModel())
			resp := resource.CreateResponse{State: tfsdk.State{Schema: plan.Schema}}
			r.Create(t.Context(), resource.CreateRequest{Plan: plan}, &resp)
			if !resp.Diagnostics.HasError() || resp.State.Raw.IsNull() {
				t.Fatal("confirmation failure lost known state")
			}
			var model teamMemberModel
			resp.State.Get(t.Context(), &model)
			if model.ID.ValueString() != "ws/team/user" {
				t.Fatal("missing recovery ID")
			}
		})
	}
}
func TestTeamMemberDeleteAccessRace(t *testing.T) {
	for _, self := range []bool{false, true} {
		for _, lostWorkspace := range []bool{false, true} {
			t.Run(fmt.Sprintf("self=%v/workspace=%v", self, lostWorkspace), func(t *testing.T) {
				mutated := false
				r := &teamMemberResource{client: testClient(t, func(w http.ResponseWriter, req *http.Request) {
					if req.Method == "POST" {
						mutated = true
						w.WriteHeader(400)
						writeTeamFixture(t, w, `{"code":"USER_IS_NOT_A_MEMBER_OF_THE_ORGANIZATION"}`)
						return
					}
					switch req.URL.Path {
					case "/auth/get-session":
						if self {
							writeTeamFixture(t, w, `{"user":{"id":"user"}}`)
						} else {
							writeTeamFixture(t, w, `{"user":{"id":"operator"}}`)
						}
					case "/auth/organization/list-teams":
						if mutated && lostWorkspace {
							w.WriteHeader(403)
							writeTeamFixture(t, w, `{}`)
						} else {
							writeTeamFixture(t, w, "["+teamFixture+"]")
						}
					case "/auth/organization/get-full-organization":
						w.WriteHeader(403)
						writeTeamFixture(t, w, `{}`)
					case "/auth/organization/list-team-members":
						if mutated {
							w.WriteHeader(400)
							writeTeamFixture(t, w, `{"code":"USER_IS_NOT_A_MEMBER_OF_THE_TEAM"}`)
						} else {
							writeTeamFixture(t, w, "["+teamMemberFixture+"]")
						}
					default:
						t.Fatal("unexpected request")
					}
				})}
				state := tfsdk.State(testPlan(t, r, teamMemberTestModel()))
				resp := resource.DeleteResponse{State: state}
				r.Delete(t.Context(), resource.DeleteRequest{State: state}, &resp)
				if resp.Diagnostics.HasError() != (!self || lostWorkspace) || !resp.State.Raw.Equal(state.Raw) {
					t.Fatalf("%v", resp.Diagnostics)
				}
			})
		}
	}
}
func TestTeamMemberTransportFailures(t *testing.T) {
	r := &teamMemberResource{client: testClient(t, func(_ http.ResponseWriter, _ *http.Request) { t.Error("canceled request reached server") })}
	plan := testPlan(t, r, teamMemberTestModel())
	state := tfsdk.State(plan)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	read := resource.ReadResponse{State: state}
	r.Read(ctx, resource.ReadRequest{State: state}, &read)
	if !read.Diagnostics.HasError() || !read.State.Raw.Equal(state.Raw) {
		t.Fatal("read lost state")
	}
	deleted := resource.DeleteResponse{State: state}
	r.Delete(ctx, resource.DeleteRequest{State: state}, &deleted)
	if !deleted.Diagnostics.HasError() || !deleted.State.Raw.Equal(state.Raw) {
		t.Fatal("delete lost state")
	}
	created := resource.CreateResponse{State: tfsdk.State{Schema: plan.Schema}}
	r.Create(ctx, resource.CreateRequest{Plan: plan}, &created)
	if !created.Diagnostics.HasError() {
		t.Fatal("create ignored transport failure")
	}
}
