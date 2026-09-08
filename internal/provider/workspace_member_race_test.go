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

func TestWorkspaceMemberReconciliation(t *testing.T) {
	for _, mode := range []string{"cancel acceptance update", "cancel acceptance delete", "invite acceptance", "replacement denied", "cancel denied", "cancel wrong shape", "update wrong envelope", "remove wrong shape", "remove missing target", "remove missing operator", "update missing operator", "remove denied", "update denied"} {
		t.Run(mode, func(t *testing.T) {
			i := invitationWire("invite", "pending", "member")
			var m map[string]any
			accepted := strings.HasPrefix(mode, "update") || strings.HasPrefix(mode, "remove")
			if accepted {
				m = memberWire("target", "target@example.com", "member")
				i["status"] = "accepted"
			}
			forbidden := false
			mutations := 0
			c := projectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if r.Method == "POST" {
					mutations++
					json.NewDecoder(r.Body).Decode(&body)
				}
				var v any
				switch strings.TrimPrefix(r.URL.Path, "/auth/organization/") {
				case "get-full-organization":
					if forbidden {
						w.WriteHeader(403)
						fmt.Fprint(w, `{"code":"USER_IS_NOT_A_MEMBER_OF_THE_ORGANIZATION"}`)
						return
					}
					v = map[string]string{"id": "ws"}
				case "list-members":
					members := []any{}
					if m != nil {
						members = append(members, m)
					}
					v = map[string]any{"members": members, "total": len(members)}
				case "list-invitations":
					v = []any{i}
				case "cancel-invitation":
					if mode == "cancel denied" {
						w.WriteHeader(403)
						fmt.Fprint(w, `{"code":"YOU_ARE_NOT_ALLOWED_TO_CANCEL_THIS_INVITATION"}`)
						return
					}
					i["status"] = "canceled"
					if strings.HasPrefix(mode, "cancel acceptance") {
						m = memberWire("target", "target@example.com", "member")
					}
					v = i
					if mode == "cancel wrong shape" {
						v = map[string]any{"invitation": i}
					}
				case "invite-member":
					if mode == "replacement denied" {
						w.WriteHeader(403)
						fmt.Fprint(w, `{"code":"FORBIDDEN"}`)
						return
					}
					if mode == "invite acceptance" {
						m = memberWire("target", "target@example.com", "member")
						i["status"] = "accepted"
						w.WriteHeader(400)
						fmt.Fprint(w, `{"code":"USER_IS_ALREADY_A_MEMBER_OF_THIS_ORGANIZATION"}`)
						return
					}
					t.Error("unexpected reinvite")
				case "update-member-role":
					if mode == "update denied" {
						w.WriteHeader(403)
						fmt.Fprint(w, `{"code":"YOU_ARE_NOT_ALLOWED_TO_UPDATE_THIS_MEMBER"}`)
						return
					}
					if mode == "update missing operator" {
						forbidden = true
						w.WriteHeader(400)
						fmt.Fprint(w, `{"code":"MEMBER_NOT_FOUND"}`)
						return
					}
					m["role"] = body["role"]
					v = m
					if mode == "update wrong envelope" {
						v = map[string]any{"member": m}
					}
				case "remove-member":
					if mode == "remove denied" {
						w.WriteHeader(401)
						fmt.Fprint(w, `{"code":"YOU_ARE_NOT_ALLOWED_TO_DELETE_THIS_MEMBER"}`)
						return
					}
					if mode == "remove missing target" || mode == "remove missing operator" {
						m = nil
						forbidden = mode == "remove missing operator"
						w.WriteHeader(400)
						fmt.Fprint(w, `{"code":"MEMBER_NOT_FOUND"}`)
						return
					}
					v = map[string]any{"member": m}
					m = nil
					if mode == "remove wrong shape" {
						v = map[string]any{"success": true}
					}
				default:
					t.Errorf("unexpected endpoint %s", r.URL.Path)
				}
				json.NewEncoder(w).Encode(v)
			})
			r := &workspaceMemberResource{client: c}
			model := memberTestModel()
			model.Status = types.StringValue("pending")
			model.InvitationID = types.StringValue("invite")
			model.MemberID = types.StringNull()
			if accepted {
				model.Status = types.StringValue("accepted")
				model.MemberID = types.StringValue("target")
				model.InvitationID = types.StringNull()
			}
			p := memberTestPlan(t, model)
			state := tfsdk.State{Schema: p.Schema, Raw: p.Raw}
			deleting := strings.HasPrefix(mode, "remove") || mode == "cancel acceptance delete"
			success := strings.Contains(mode, "acceptance") || mode == "remove missing target"
			if deleting {
				resp := resource.DeleteResponse{State: state}
				r.Delete(t.Context(), resource.DeleteRequest{State: state}, &resp)
				if resp.Diagnostics.HasError() == success {
					t.Fatalf("unexpected delete diagnostics %v", resp.Diagnostics)
				}
				if !success && !resp.State.Raw.Equal(state.Raw) {
					t.Fatal("partial failure lost state")
				}
				if success && m != nil {
					t.Fatal("acceptance race left member behind")
				}
			} else {
				model.Role = types.StringValue("custom")
				resp := resource.UpdateResponse{State: state}
				r.Update(t.Context(), resource.UpdateRequest{State: state, Plan: memberTestPlan(t, model)}, &resp)
				if resp.Diagnostics.HasError() == success {
					t.Fatalf("unexpected update diagnostics %v", resp.Diagnostics)
				}
				if !success && !resp.State.Raw.Equal(state.Raw) {
					t.Fatal("partial failure wrote desired state")
				}
				if success && (m == nil || m["role"] != "custom") {
					t.Fatal("acceptance race did not update member")
				}
			}
			if mutations == 0 || mutations > 3 {
				t.Fatalf("unbounded or missing mutations: %d", mutations)
			}
		})
	}
}

func TestWorkspaceMemberPresence(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		absent     bool
	}{
		{"missing", `{"code":"ORGANIZATION_NOT_FOUND"}`, 400, true},
		{"forbidden", `{"code":"USER_IS_NOT_A_MEMBER_OF_THE_ORGANIZATION"}`, 403, false},
		{"unauthorized", `{}`, 401, false},
		{"generic bad request", `{}`, 400, false},
		{"wrong id", `{"id":"different"}`, 200, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := projectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if !strings.HasSuffix(r.URL.Path, "get-full-organization") {
					t.Error("unexpected endpoint")
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			})
			model := memberTestModel()
			p := memberTestPlan(t, model)
			state := tfsdk.State{Schema: p.Schema, Raw: p.Raw}
			resp := resource.ReadResponse{State: state}
			(&workspaceMemberResource{client: c}).Read(t.Context(), resource.ReadRequest{State: state}, &resp)
			if tc.absent {
				if resp.Diagnostics.HasError() || !resp.State.Raw.IsNull() {
					t.Fatal("missing workspace did not remove state")
				}
			} else if !resp.Diagnostics.HasError() || !resp.State.Raw.Equal(state.Raw) {
				t.Fatal("error hid state")
			}
		})
	}
}

func TestWorkspaceMemberTerminalAfterAcceptedHistory(t *testing.T) {
	c := projectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var v any
		switch {
		case strings.HasSuffix(r.URL.Path, "get-full-organization"):
			v = map[string]string{"id": "ws"}
		case strings.HasSuffix(r.URL.Path, "list-members"):
			v = map[string]any{"members": []any{}, "total": 0}
		case strings.HasSuffix(r.URL.Path, "list-invitations"):
			v = []any{invitationWire("old", "accepted", "member"), invitationWire("current", "rejected", "member")}
		default:
			t.Error("refresh mutated access")
		}
		json.NewEncoder(w).Encode(v)
	})
	model := memberTestModel()
	model.InvitationID = types.StringValue("current")
	p := memberTestPlan(t, model)
	state := tfsdk.State{Schema: p.Schema, Raw: p.Raw}
	resp := resource.ReadResponse{State: state}
	(&workspaceMemberResource{client: c}).Read(t.Context(), resource.ReadRequest{State: state}, &resp)
	if resp.Diagnostics.HasError() || !resp.State.Raw.IsNull() {
		t.Fatalf("historical acceptance blocked recreation: %v", resp.Diagnostics)
	}
}
