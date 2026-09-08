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

func TestWorkspaceMemberReconciliation(t *testing.T) {
	for _, tc := range []struct {
		name                           string
		accepted, deleting, success    bool
		cancel, invite, update, remove string
	}{
		{name: "cancel acceptance update", cancel: "acceptance", success: true},
		{name: "cancel acceptance delete", cancel: "acceptance", deleting: true, success: true},
		{name: "invite acceptance", invite: "acceptance", success: true},
		{name: "replacement denied", invite: "denied"},
		{name: "cancel denied", cancel: "denied"},
		{name: "cancel wrong shape", cancel: "wrong shape"},
		{name: "update wrong envelope", accepted: true, update: "wrong envelope"},
		{name: "remove wrong shape", accepted: true, deleting: true, remove: "wrong shape"},
		{name: "remove missing target", accepted: true, deleting: true, remove: "missing target", success: true},
		{name: "remove missing operator", accepted: true, deleting: true, remove: "missing operator"},
		{name: "update missing operator", accepted: true, update: "missing operator"},
		{name: "remove denied", accepted: true, deleting: true, remove: "denied"},
		{name: "update denied", accepted: true, update: "denied"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i := invitationWire("invite", "pending", "member")
			var m map[string]any
			if tc.accepted {
				m = memberWire("target", "target@example.com", "member")
				i["status"] = "accepted"
			}
			forbidden := false
			mutations := 0
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if r.Method == "POST" {
					mutations++
					if !testDecodeRequest(t, w, r, &body) {
						return
					}
				}
				var v any
				switch strings.TrimPrefix(r.URL.Path, "/auth/organization/") {
				case "get-full-organization":
					if forbidden {
						w.WriteHeader(403)
						if _, err := fmt.Fprint(w, `{"code":"USER_IS_NOT_A_MEMBER_OF_THE_ORGANIZATION"}`); err != nil {
							t.Error(err)
						}
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
					if tc.cancel == "denied" {
						w.WriteHeader(403)
						if _, err := fmt.Fprint(w, `{"code":"YOU_ARE_NOT_ALLOWED_TO_CANCEL_THIS_INVITATION"}`); err != nil {
							t.Error(err)
						}
						return
					}
					i["status"] = "canceled"
					if tc.cancel == "acceptance" {
						m = memberWire("target", "target@example.com", "member")
					}
					v = i
					if tc.cancel == "wrong shape" {
						v = map[string]any{"invitation": i}
					}
				case "invite-member":
					if tc.invite == "denied" {
						w.WriteHeader(403)
						if _, err := fmt.Fprint(w, `{"code":"FORBIDDEN"}`); err != nil {
							t.Error(err)
						}
						return
					}
					if tc.invite == "acceptance" {
						m = memberWire("target", "target@example.com", "member")
						i["status"] = "accepted"
						w.WriteHeader(400)
						if _, err := fmt.Fprint(w, `{"code":"USER_IS_ALREADY_A_MEMBER_OF_THIS_ORGANIZATION"}`); err != nil {
							t.Error(err)
						}
						return
					}
					t.Error("unexpected reinvite")
				case "update-member-role":
					if tc.update == "denied" {
						w.WriteHeader(403)
						if _, err := fmt.Fprint(w, `{"code":"YOU_ARE_NOT_ALLOWED_TO_UPDATE_THIS_MEMBER"}`); err != nil {
							t.Error(err)
						}
						return
					}
					if tc.update == "missing operator" {
						forbidden = true
						w.WriteHeader(400)
						if _, err := fmt.Fprint(w, `{"code":"MEMBER_NOT_FOUND"}`); err != nil {
							t.Error(err)
						}
						return
					}
					m["role"] = body["role"]
					v = m
					if tc.update == "wrong envelope" {
						v = map[string]any{"member": m}
					}
				case "remove-member":
					if tc.remove == "denied" {
						w.WriteHeader(401)
						if _, err := fmt.Fprint(w, `{"code":"YOU_ARE_NOT_ALLOWED_TO_DELETE_THIS_MEMBER"}`); err != nil {
							t.Error(err)
						}
						return
					}
					if tc.remove == "missing target" || tc.remove == "missing operator" {
						m = nil
						forbidden = tc.remove == "missing operator"
						w.WriteHeader(400)
						if _, err := fmt.Fprint(w, `{"code":"MEMBER_NOT_FOUND"}`); err != nil {
							t.Error(err)
						}
						return
					}
					v = map[string]any{"member": m}
					m = nil
					if tc.remove == "wrong shape" {
						v = map[string]any{"success": true}
					}
				default:
					t.Errorf("unexpected endpoint %s", r.URL.Path)
				}
				testEncodeResponse(t, w, v)
			})
			r := &workspaceMemberResource{client: c}
			model := memberTestModel()
			model.Status = types.StringValue("pending")
			model.InvitationID = types.StringValue("invite")
			model.MemberID = types.StringNull()
			if tc.accepted {
				model.Status = types.StringValue("accepted")
				model.MemberID = types.StringValue("target")
				model.InvitationID = types.StringNull()
			}
			p := testPlan(t, &workspaceMemberResource{}, model)
			state := tfsdk.State(p)
			success := tc.success
			if tc.deleting {
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
				r.Update(t.Context(), resource.UpdateRequest{State: state, Plan: testPlan(t, &workspaceMemberResource{}, model)}, &resp)
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
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if !strings.HasSuffix(r.URL.Path, "get-full-organization") {
					t.Error("unexpected endpoint")
				}
				w.WriteHeader(tc.status)
				if _, err := fmt.Fprint(w, tc.body); err != nil {
					t.Error(err)
				}
			})
			model := memberTestModel()
			p := testPlan(t, &workspaceMemberResource{}, model)
			state := tfsdk.State(p)
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
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
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
		testEncodeResponse(t, w, v)
	})
	model := memberTestModel()
	model.InvitationID = types.StringValue("current")
	p := testPlan(t, &workspaceMemberResource{}, model)
	state := tfsdk.State(p)
	resp := resource.ReadResponse{State: state}
	(&workspaceMemberResource{client: c}).Read(t.Context(), resource.ReadRequest{State: state}, &resp)
	if resp.Diagnostics.HasError() || !resp.State.Raw.IsNull() {
		t.Fatalf("historical acceptance blocked recreation: %v", resp.Diagnostics)
	}
}
