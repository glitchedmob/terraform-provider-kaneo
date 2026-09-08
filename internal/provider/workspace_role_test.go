// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func roleTestPlan(t *testing.T, model workspaceRoleModel) tfsdk.Plan {
	t.Helper()
	var schema resource.SchemaResponse
	(&workspaceRoleResource{}).Schema(t.Context(), resource.SchemaRequest{}, &schema)
	plan := tfsdk.Plan{Schema: schema.Schema}
	if d := plan.Set(t.Context(), model); d.HasError() {
		t.Fatal(d)
	}
	return plan
}
func roleTestModel(t *testing.T) workspaceRoleModel {
	t.Helper()
	m := workspaceRoleModel{WorkspaceID: types.StringValue("ws")}
	permissions, d := types.MapValueFrom(t.Context(), types.SetType{ElemType: types.StringType}, map[string][]string{"task": {"read", "update"}, "project": {}})
	if d.HasError() {
		t.Fatal(d)
	}
	m.ID, m.Name, m.Permissions = types.StringValue("role-id"), types.StringValue("custom"), permissions
	return m
}
func TestWorkspaceRoleLifecycle(t *testing.T) {
	model := roleTestModel(t)
	name := "custom"
	var renamed bool
	client := projectTestClient(t, func(w http.ResponseWriter, req *http.Request) {
		if req.Method == "GET" {
			if req.URL.Query().Get("organizationId") != "ws" || req.URL.Query().Get("roleId") != "role-id" {
				t.Error("missing scoped native IDs")
			}
		} else {
			var body map[string]any
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body["organizationId"] != "ws" {
				t.Error("missing explicit workspace")
			}
			if strings.HasSuffix(req.URL.Path, "update-role") {
				if body["roleId"] != "role-id" {
					t.Error("update not by native ID")
				}
				data := body["data"].(map[string]any)
				if renamed {
					name = data["roleName"].(string)
				} else if _, ok := data["roleName"]; ok {
					t.Error("unchanged name must be omitted")
				}
				if len(data["permission"].(map[string]any)) != 2 {
					t.Error("incomplete permissions map")
				}
			}
		}
		role := fmt.Sprintf(`{"id":"role-id","organizationId":"ws","role":%q,"permission":{"project":[],"task":["update","read"]}}`, name)
		switch {
		case strings.HasSuffix(req.URL.Path, "get-role"):
			if _, err := fmt.Fprint(w, role); err != nil {
				t.Error(err)
			}
		case strings.HasSuffix(req.URL.Path, "delete-role"):
			if _, err := fmt.Fprint(w, `{"success":true}`); err != nil {
				t.Error(err)
			}
		default:
			if _, err := fmt.Fprintf(w, `{"success":true,"roleData":%s}`, role); err != nil {
				t.Error(err)
			}
		}
	})
	r := &workspaceRoleResource{client: client}
	plan := roleTestPlan(t, model)
	created := resource.CreateResponse{State: tfsdk.State{Schema: plan.Schema}}
	r.Create(t.Context(), resource.CreateRequest{Plan: plan}, &created)
	if created.Diagnostics.HasError() {
		t.Fatal(created.Diagnostics)
	}
	var got workspaceRoleModel
	created.State.Get(t.Context(), &got)
	if !got.Permissions.Equal(model.Permissions) {
		t.Fatal("action ordering changed state")
	}
	for _, newName := range []string{"custom", "renamed"} {
		renamed = newName != "custom"
		model.Name = types.StringValue(newName)
		updated := resource.UpdateResponse{State: created.State}
		r.Update(t.Context(), resource.UpdateRequest{Plan: roleTestPlan(t, model), State: created.State}, &updated)
		if updated.Diagnostics.HasError() {
			t.Fatal(updated.Diagnostics)
		}
		created.State = updated.State
	}
	imported := resource.ImportStateResponse{State: tfsdk.State{Schema: plan.Schema}}
	if d := imported.State.Set(t.Context(), workspaceRoleModel{Permissions: types.MapNull(types.SetType{ElemType: types.StringType})}); d.HasError() {
		t.Fatal(d)
	}
	r.ImportState(t.Context(), resource.ImportStateRequest{ID: "ws/role-id"}, &imported)
	if imported.Diagnostics.HasError() {
		t.Fatal(imported.Diagnostics)
	}
	read := resource.ReadResponse{State: imported.State}
	r.Read(t.Context(), resource.ReadRequest{State: imported.State}, &read)
	if read.Diagnostics.HasError() {
		t.Fatal(read.Diagnostics)
	}
	read.State.Get(t.Context(), &got)
	if !got.Permissions.Equal(model.Permissions) || got.Name != model.Name || got.ID != model.ID {
		t.Fatalf("import mismatch: %+v", got)
	}
	deleted := resource.DeleteResponse{State: read.State}
	r.Delete(t.Context(), resource.DeleteRequest{State: read.State}, &deleted)
	if deleted.Diagnostics.HasError() {
		t.Fatal(deleted.Diagnostics)
	}
}

func TestWorkspaceRoleAbsenceAndErrors(t *testing.T) {
	for _, tc := range []struct {
		name               string
		status             int
		body               string
		presenceStatus     int
		presence           string
		missing, wantError bool
	}{
		{"role absent", 400, `{"code":"ROLE_NOT_FOUND"}`, 0, "", true, false},
		{"workspace absent", 403, `{"code":"YOU_ARE_NOT_A_MEMBER_OF_THIS_ORGANIZATION"}`, 400, `{"code":"ORGANIZATION_NOT_FOUND"}`, true, false},
		{"lost membership", 403, `{"code":"YOU_ARE_NOT_A_MEMBER_OF_THIS_ORGANIZATION"}`, 403, `{"code":"USER_IS_NOT_A_MEMBER_OF_THE_ORGANIZATION"}`, false, true},
		{"lost ac read", 403, `{"code":"YOU_ARE_NOT_ALLOWED_TO_READ_A_ROLE"}`, 200, `{"id":"ws"}`, false, true},
		{"unauthenticated", 401, `{}`, 0, "", false, true},
		{"generic not found", 404, `{}`, 0, "", false, true},
		{"unknown bad request", 400, `{}`, 0, "", false, true},
		{"server failure", 500, `{}`, 0, "", false, true},
		{"malformed success", 200, `{}`, 0, "", false, true},
		{"wrong workspace", 200, `{"id":"role-id","organizationId":"other"}`, 0, "", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := projectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "get-full-organization") {
					if tc.presenceStatus == 0 {
						t.Fatal("unexpected workspace probe")
					}
					if r.URL.Query().Get("organizationId") != "ws" {
						t.Error("missing workspace query")
					}
					w.WriteHeader(tc.presenceStatus)
					if _, err := fmt.Fprint(w, tc.presence); err != nil {
						t.Error(err)
					}
					return
				}
				if r.Method != "GET" {
					t.Error("must not delete after failed read")
				}
				w.WriteHeader(tc.status)
				if _, err := fmt.Fprint(w, tc.body); err != nil {
					t.Error(err)
				}
			})
			r := &workspaceRoleResource{client: client}
			plan := roleTestPlan(t, roleTestModel(t))
			state := tfsdk.State(plan)
			read := resource.ReadResponse{State: state}
			r.Read(t.Context(), resource.ReadRequest{State: state}, &read)
			if read.Diagnostics.HasError() != tc.wantError || read.State.Raw.IsNull() != tc.missing {
				t.Fatalf("read: %v, null=%v", read.Diagnostics, read.State.Raw.IsNull())
			}
			if tc.wantError && !read.State.Raw.Equal(state.Raw) {
				t.Error("failure changed state")
			}
			deleted := resource.DeleteResponse{State: state}
			r.Delete(t.Context(), resource.DeleteRequest{State: state}, &deleted)
			if deleted.Diagnostics.HasError() != tc.wantError {
				t.Fatal(deleted.Diagnostics)
			}
		})
	}
}
func TestWorkspaceRoleNameValidation(t *testing.T) {
	var response resource.SchemaResponse
	(&workspaceRoleResource{}).Schema(t.Context(), resource.SchemaRequest{}, &response)
	name := response.Schema.Attributes["name"].(schema.StringAttribute)
	for _, value := range []string{"owner", "Owner", "", "UPPER", "Équipe", "two roles", "one,two", "custom", "admin", "member", "viewer"} {
		resp := validator.StringResponse{}
		for _, v := range name.Validators {
			v.ValidateString(t.Context(), validator.StringRequest{ConfigValue: types.StringValue(value)}, &resp)
		}
		valid := value == "custom" || value == "admin" || value == "member" || value == "viewer"
		if resp.Diagnostics.HasError() == valid {
			t.Errorf("name %q: %v", value, resp.Diagnostics)
		}
	}
}

func TestWorkspaceRoleImportValidation(t *testing.T) {
	for _, id := range []string{"", "role", "/role", "ws/", "ws/role/extra"} {
		resp := resource.ImportStateResponse{}
		(&workspaceRoleResource{}).ImportState(t.Context(), resource.ImportStateRequest{ID: id}, &resp)
		if !resp.Diagnostics.HasError() {
			t.Errorf("accepted %q", id)
		}
	}
}

func TestWorkspaceRoleMutationErrors(t *testing.T) {
	for _, operation := range []string{"create", "update", "delete"} {
		for _, tc := range []struct {
			name   string
			status int
			body   string
		}{
			{"forbidden", 403, `{"code":"YOU_ARE_NOT_ALLOWED_TO_DELETE_A_ROLE"}`},
			{"assigned", 400, `{"code":"ROLE_IS_ASSIGNED_TO_MEMBERS"}`},
			{"server", 500, `{}`},
			{"unconfirmed", 200, `{"success":false}`},
			{"malformed", 200, `{}`},
		} {
			t.Run(operation+"/"+tc.name, func(t *testing.T) {
				client := projectTestClient(t, func(w http.ResponseWriter, req *http.Request) {
					if req.Method == "GET" {
						if strings.HasSuffix(req.URL.Path, "get-full-organization") {
							if _, err := fmt.Fprint(w, `{"id":"ws"}`); err != nil {
								t.Error(err)
							}
						} else {
							if _, err := fmt.Fprint(w, `{"id":"role-id","organizationId":"ws","role":"custom","permission":{}}`); err != nil {
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
				r := &workspaceRoleResource{client: client}
				plan := roleTestPlan(t, roleTestModel(t))
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
