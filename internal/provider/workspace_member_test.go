// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func memberTestModel() workspaceMemberModel {
	return workspaceMemberModel{ID: types.StringValue("ws/target@example.com"), WorkspaceID: types.StringValue("ws"), Email: types.StringValue("target@example.com"), Role: types.StringValue("member"), Status: types.StringUnknown(), MemberID: types.StringUnknown(), InvitationID: types.StringUnknown()}
}
func memberWire(id, email, role string) map[string]any {
	return map[string]any{"id": id, "organizationId": "ws", "userId": "u-" + id, "role": role, "user": map[string]string{"id": "u-" + id, "email": email}}
}
func invitationWire(id, status, role string) map[string]any {
	return map[string]any{"id": id, "organizationId": "ws", "email": "target@example.com", "role": role, "status": status, "expiresAt": time.Now().Add(time.Hour).Format(time.RFC3339Nano)}
}
func TestWorkspaceMemberLifecycle(t *testing.T) {
	for _, accept := range []bool{false, true} {
		t.Run(fmt.Sprint("accepted=", accept), func(t *testing.T) {
			invitations := []map[string]any{}
			members := []map[string]any{memberWire("owner", "owner@example.com", "owner")}
			invites := 0
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if r.Method == "POST" {
					if !testDecodeRequest(t, w, r, &body) {
						return
					}
				} else if r.URL.Query().Get("organizationId") != "ws" {
					t.Error("unscoped query")
				}
				var result any
				switch strings.TrimPrefix(r.URL.Path, "/auth/organization/") {
				case "get-full-organization":
					result = map[string]string{"id": "ws"}
				case "list-members":
					result = map[string]any{"members": members, "total": len(members)}
				case "list-invitations":
					result = invitations
				case "invite-member":
					if body["organizationId"] != "ws" || body["email"] != "target@example.com" || body["resend"] != nil {
						t.Error("wrong invite payload")
					}
					invites++
					i := invitationWire(fmt.Sprint(invites), "pending", body["role"].(string))
					invitations = append(invitations, i)
					result = i
				case "cancel-invitation":
					for _, i := range invitations {
						if i["id"] == body["invitationId"] {
							i["status"] = "canceled"
							result = i
						}
					}
				case "update-member-role":
					if body["memberId"] != "target" || body["organizationId"] != "ws" {
						t.Error("wrong update scope")
					}
					members[1]["role"] = body["role"]
					result = members[1]
				case "remove-member":
					if body["memberIdOrEmail"] != "target" || body["organizationId"] != "ws" {
						t.Error("wrong delete scope")
					}
					result = map[string]any{"member": members[1]}
					members = members[:1]
				default:
					t.Errorf("unexpected operation %s", r.URL.Path)
				}
				testEncodeResponse(t, w, result)
			})
			r := &workspaceMemberResource{client: c}
			m := memberTestModel()
			p := testPlan(t, &workspaceMemberResource{}, m)
			created := resource.CreateResponse{State: tfsdk.State{Schema: p.Schema}}
			r.Create(t.Context(), resource.CreateRequest{Plan: p}, &created)
			if created.Diagnostics.HasError() {
				t.Fatal(created.Diagnostics)
			}
			created.State.Get(t.Context(), &m)
			if m.Status.ValueString() != "pending" || invites != 1 {
				t.Fatal("create did not return pending")
			}
			duplicate := resource.CreateResponse{State: tfsdk.State{Schema: p.Schema}}
			r.Create(t.Context(), resource.CreateRequest{Plan: p}, &duplicate)
			if !duplicate.Diagnostics.HasError() || invites != 1 {
				t.Fatal("create stole existing invite")
			}
			if accept {
				invitations[0]["status"] = "accepted"
				members = append(members, memberWire("target", "TARGET@example.com", "member"))
				duplicate = resource.CreateResponse{State: tfsdk.State{Schema: p.Schema}}
				r.Create(t.Context(), resource.CreateRequest{Plan: p}, &duplicate)
				if !duplicate.Diagnostics.HasError() || invites != 1 {
					t.Fatal("create stole existing member")
				}
			}
			read := resource.ReadResponse{State: created.State}
			r.Read(t.Context(), resource.ReadRequest{State: created.State}, &read)
			if read.Diagnostics.HasError() {
				t.Fatal(read.Diagnostics)
			}
			read.State.Get(t.Context(), &m)
			if m.ID.ValueString() != "ws/target@example.com" || invites != 1 {
				t.Fatal("refresh changed identity or resent")
			}
			m.Role = types.StringValue("custom")
			updated := resource.UpdateResponse{State: read.State}
			r.Update(t.Context(), resource.UpdateRequest{Plan: testPlan(t, &workspaceMemberResource{}, m), State: read.State}, &updated)
			if updated.Diagnostics.HasError() {
				t.Fatal(updated.Diagnostics)
			}
			updated.State.Get(t.Context(), &m)
			if m.Role.ValueString() != "custom" {
				t.Fatal("role not updated")
			}
			// Both member and an outstanding expired invitation must be removed.
			expired := invitationWire("expired", "pending", "member")
			expired["expiresAt"] = time.Now().Add(-time.Hour).Format(time.RFC3339)
			invitations = append(invitations, expired)
			deleted := resource.DeleteResponse{State: updated.State}
			r.Delete(t.Context(), resource.DeleteRequest{State: updated.State}, &deleted)
			if deleted.Diagnostics.HasError() {
				t.Fatal(deleted.Diagnostics)
			}
			if len(members) != 1 {
				t.Fatal("target not removed or unrelated member touched")
			}
			for _, i := range invitations {
				if i["status"] == "pending" {
					t.Fatal("pending invite left behind")
				}
			}
		})
	}
}

func TestWorkspaceMemberDiscovery(t *testing.T) {
	tests := []struct {
		name, members, invitations string
		status                     int
		want                       string
		absent                     bool
	}{
		{name: "empty", members: `{"members":[],"total":0}`, invitations: `[]`, absent: true},
		{name: "missing total", members: `{"members":[]}`, invitations: `[]`, want: "members/total"},
		{name: "wrong envelope", members: `{"member":{}}`, invitations: `[]`, want: "members/total"},
		{name: "null list", members: `{"members":null,"total":0}`, invitations: `[]`, want: "members/total"},
		{name: "premature page", members: `{"members":[],"total":1}`, invitations: `[]`, want: "progress"},
		{name: "wrong invitations", members: `{"members":[],"total":0}`, invitations: `{}`, want: "list invitations"},
		{name: "null invitations", members: `{"members":[],"total":0}`, invitations: `null`, want: "array"},
		{name: "forbidden", status: 403, members: `{"code":"YOU_ARE_NOT_A_MEMBER_OF_THIS_ORGANIZATION"}`, want: "403"},
		{name: "unauthorized", status: 401, members: `{}`, want: "401"},
		{name: "bad request", status: 400, members: `{"code":"MEMBER_NOT_FOUND"}`, want: "400"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "get-full-organization"):
					if _, err := fmt.Fprint(w, `{"id":"ws"}`); err != nil {
						t.Error(err)
					}
				case strings.HasSuffix(r.URL.Path, "list-members"):
					if tc.status != 0 {
						w.WriteHeader(tc.status)
					}
					if _, err := fmt.Fprint(w, tc.members); err != nil {
						t.Error(err)
					}
				case strings.HasSuffix(r.URL.Path, "list-invitations"):
					if _, err := fmt.Fprint(w, tc.invitations); err != nil {
						t.Error(err)
					}
				default:
					t.Fatal("refresh mutated remote")
				}
			})
			o, e := observeWorkspaceMember(t.Context(), c, "ws", "target@example.com")
			if tc.want != "" {
				if e == nil || !strings.Contains(e.Error(), tc.want) {
					t.Fatalf("got %v, want %s", e, tc.want)
				}
				return
			}
			if e != nil || o.absent() != tc.absent {
				t.Fatalf("got %+v %v", o, e)
			}
		})
	}
}
func TestWorkspaceMemberInvitationSafety(t *testing.T) {
	for _, kind := range []string{"cap", "duplicates", "expired", "rejected", "canceled", "bad status", "bad time", "null role", "multi role", "accepted gap"} {
		t.Run(kind, func(t *testing.T) {
			i := invitationWire("invite", "pending", "member")
			invitations := []map[string]any{i}
			wantError := true
			switch kind {
			case "cap":
				invitations = nil
				for n := 0; n < 100; n++ {
					invitations = append(invitations, invitationWire(fmt.Sprint(n), "canceled", "member"))
				}
			case "duplicates":
				invitations = append(invitations, invitationWire("second", "pending", "member"))
			case "expired":
				i["expiresAt"] = time.Now().Add(-time.Hour).Format(time.RFC3339)
				wantError = false
			case "rejected", "canceled":
				i["status"] = kind
				wantError = false
			case "bad status":
				i["status"] = "mystery"
			case "bad time":
				i["expiresAt"] = "not a timestamp"
			case "null role":
				i["role"] = nil
			case "multi role":
				i["role"] = "admin,member"
			case "accepted gap":
				i["status"] = "accepted"
			}
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				var v any
				switch {
				case strings.HasSuffix(r.URL.Path, "get-full-organization"):
					v = map[string]string{"id": "ws"}
				case strings.HasSuffix(r.URL.Path, "list-members"):
					v = map[string]any{"members": []any{}, "total": 0}
				case strings.HasSuffix(r.URL.Path, "list-invitations"):
					v = invitations
				default:
					t.Error("read must not mutate")
				}
				testEncodeResponse(t, w, v)
			})
			m := memberTestModel()
			p := testPlan(t, &workspaceMemberResource{}, m)
			state := tfsdk.State(p)
			resp := resource.ReadResponse{State: state}
			(&workspaceMemberResource{client: c}).Read(t.Context(), resource.ReadRequest{State: state}, &resp)
			if resp.Diagnostics.HasError() != wantError {
				t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
			}
			if wantError && !resp.State.Raw.Equal(state.Raw) {
				t.Fatal("error dropped state")
			}
			if !wantError && !resp.State.Raw.IsNull() {
				t.Fatal("terminal invitation did not allow recreation")
			}
		})
	}
}
func TestWorkspaceMemberPaging(t *testing.T) {
	for _, mode := range []string{"complete", "moving total", "repeat", "duplicate target", "multi role"} {
		t.Run(mode, func(t *testing.T) {
			pages := 0
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "get-full-organization"):
					if _, err := fmt.Fprint(w, `{"id":"ws"}`); err != nil {
						t.Error(err)
					}
				case strings.HasSuffix(r.URL.Path, "list-invitations"):
					if _, err := fmt.Fprint(w, `[]`); err != nil {
						t.Error(err)
					}
				case strings.HasSuffix(r.URL.Path, "list-members"):
					q := r.URL.Query()
					if q.Get("limit") != "100" || q.Get("offset") != fmt.Sprint(pages*100) || q.Get("sortBy") != "id" || q.Get("sortDirection") != "asc" {
						t.Errorf("invalid pagination %v", q)
					}
					count, total := 100, 101
					if pages == 1 {
						count = 1
					}
					if pages == 1 && mode == "moving total" {
						total = 102
					}
					members := []map[string]any{}
					for n := 0; n < count; n++ {
						id := fmt.Sprintf("%03d", pages*100+n)
						email := "other" + id + "@example.com"
						role := "member"
						if pages == 1 {
							email = "target@example.com"
						}
						if mode == "repeat" && pages == 1 {
							id = "000"
						}
						if mode == "duplicate target" && n == 0 {
							email = "target@example.com"
						}
						if mode == "multi role" && pages == 1 {
							role = "admin,member"
						}
						members = append(members, memberWire(id, email, role))
					}
					pages++
					testEncodeResponse(t, w, map[string]any{"members": members, "total": total})
				default:
					t.Error("unexpected request")
				}
			})
			o, e := observeWorkspaceMember(t.Context(), c, "ws", "target@example.com")
			if mode == "complete" {
				if e != nil || o.member == nil || o.member.Id != "100" || pages != 2 {
					t.Fatalf("missed page two: %+v %v", o, e)
				}
			} else if e == nil {
				t.Fatal("unsafe pagination/role accepted")
			}
		})
	}
}
func TestWorkspaceMemberValidation(t *testing.T) {
	var s resource.SchemaResponse
	(&workspaceMemberResource{}).Schema(t.Context(), resource.SchemaRequest{}, &s)
	for name, values := range map[string]map[string]bool{
		"email":        {"target@example.com": true, "Upper@example.com": false, "a@example.com ": false, "Name <a@example.com>": false, "invalid": false},
		"role":         {"custom": true, "viewer": true, "owner": true, "": false, "admin,member": false, "two roles": false},
		"workspace_id": {"ws": true, "": false, " ws": false},
	} {
		attribute := s.Schema.Attributes[name].(schema.StringAttribute)
		for value, valid := range values {
			resp := validator.StringResponse{}
			for _, v := range attribute.Validators {
				v.ValidateString(t.Context(), validator.StringRequest{ConfigValue: types.StringValue(value)}, &resp)
			}
			if resp.Diagnostics.HasError() == valid {
				t.Errorf("%s=%q: %v", name, value, resp.Diagnostics)
			}
		}
	}
}

func TestWorkspaceMemberIdentity(t *testing.T) {
	for _, email := range []string{"target@example.com", "slash/name+tag@example.com"} {
		id := memberIdentity("ws/escaped", email)
		ws, got, e := parseMemberIdentity(id)
		if e != nil || ws != "ws/escaped" || got != email {
			t.Fatalf("roundtrip %q: %v", id, e)
		}
	}
	for _, id := range []string{"ws", "/a@example.com", "ws/UPPER@example.com", "ws/a@example.com/extra", "ws/%zz", "ws/a%40example.com", "ws/name <a@example.com>", "ws/a@example.com "} {
		if _, _, e := parseMemberIdentity(id); e == nil {
			t.Errorf("accepted invalid %q", id)
		}
	}
}
