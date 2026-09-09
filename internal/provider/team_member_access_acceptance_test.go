// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"net/http"
	"testing"
	"uuid"

	kaneoclient "github.com/glitchedmob/terraform-provider-kaneo/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Direct framework calls let us assert the exact retained state after failed
// refresh/delete, without another resource's refresh masking the diagnostic.
func TestAccTeamMemberAccessLoss(t *testing.T) {
	api := newAcceptanceAPI(t)
	owner := userAcceptanceSession(api, "team-owner-"+uuid.NewV4().String()+"@example.com", uuid.NewV4().String())
	var signup struct{ User struct{ ID string } }
	if err := owner.request(http.MethodPost, "/auth/sign-up/email", map[string]string{"email": owner.username, "password": owner.password, "name": "Independent owner"}, &signup); err != nil {
		t.Fatal(err)
	}
	var ws struct{ ID string }
	if err := api.request(http.MethodPost, "/auth/organization/create", map[string]string{"name": "Access loss", "slug": "access-" + uuid.NewV4().String()}, &ws); err != nil {
		t.Fatal(err)
	}
	invite := func(from, to *acceptanceAPI) {
		t.Helper()
		var row struct{ ID string }
		if err := from.request(http.MethodPost, "/auth/organization/invite-member", map[string]string{"organizationId": ws.ID, "email": to.username, "role": "owner"}, &row); err != nil {
			t.Fatal(err)
		}
		if err := to.request(http.MethodPost, "/auth/organization/accept-invitation", map[string]string{"invitationId": row.ID}, nil); err != nil {
			t.Fatal(err)
		}
	}
	invite(api, owner)
	var team struct{ ID string }
	if err := api.request(http.MethodPost, "/auth/organization/create-team", map[string]string{"organizationId": ws.ID, "name": "access"}, &team); err != nil {
		t.Fatal(err)
	}
	mutate := func(a *acceptanceAPI, route, user string) {
		t.Helper()
		if err := a.request(http.MethodPost, "/auth/organization/"+route, map[string]string{"organizationId": ws.ID, "teamId": team.ID, "userId": user}, nil); err != nil {
			t.Fatal(err)
		}
	}
	mutate(api, "add-team-member", api.userID)
	mutate(api, "add-team-member", signup.User.ID)
	client, err := kaneoclient.NewClientWithResponses(api.endpoint, kaneoclient.WithHTTPClient(api.client))
	if err != nil {
		t.Fatal(err)
	}
	r := &teamMemberResource{client: client}
	model := teamMemberModel{ID: types.StringValue(teamMemberID(ws.ID, team.ID, signup.User.ID)), WorkspaceID: types.StringValue(ws.ID), TeamID: types.StringValue(team.ID), UserID: types.StringValue(signup.User.ID)}
	state := tfsdk.State(testPlan(t, r, model))
	assertDenied := func() {
		t.Helper()
		read := resource.ReadResponse{State: state}
		r.Read(t.Context(), resource.ReadRequest{State: state}, &read)
		if !read.Diagnostics.HasError() || !read.State.Raw.Equal(state.Raw) {
			t.Fatalf("failed read lost state: %v", read.Diagnostics)
		}
		deleted := resource.DeleteResponse{State: state}
		r.Delete(t.Context(), resource.DeleteRequest{State: state}, &deleted)
		if !deleted.Diagnostics.HasError() || !deleted.State.Raw.Equal(state.Raw) {
			t.Fatalf("failed delete lost state: %v", deleted.Diagnostics)
		}
	}
	mutate(owner, "remove-team-member", api.userID)
	assertDenied()
	mutate(owner, "add-team-member", api.userID)
	if err := owner.request(http.MethodPost, "/auth/organization/remove-member", map[string]string{"organizationId": ws.ID, "memberIdOrEmail": api.username}, nil); err != nil {
		t.Fatal(err)
	}
	assertDenied()
	// Even self absence cannot bypass workspace revocation.
	self := model
	self.UserID = types.StringValue(api.userID)
	self.ID = types.StringValue(teamMemberID(ws.ID, team.ID, api.userID))
	selfState := tfsdk.State(testPlan(t, r, self))
	selfRead := resource.ReadResponse{State: selfState}
	r.Read(t.Context(), resource.ReadRequest{State: selfState}, &selfRead)
	if !selfRead.Diagnostics.HasError() || !selfRead.State.Raw.Equal(selfState.Raw) {
		t.Fatal("self read cleared revoked workspace state")
	}
	invite(owner, api)
	mutate(owner, "add-team-member", api.userID)
	read := resource.ReadResponse{State: state}
	r.Read(t.Context(), resource.ReadRequest{State: state}, &read)
	if read.Diagnostics.HasError() || !read.State.Raw.Equal(state.Raw) {
		t.Fatal(read.Diagnostics)
	}
	// Explicit adoption only, even though runtime add itself is idempotent.
	created := resource.CreateResponse{State: tfsdk.State{Schema: state.Schema}}
	r.Create(t.Context(), resource.CreateRequest{Plan: testPlan(t, r, model)}, &created)
	if !created.Diagnostics.HasError() {
		t.Fatal("adopted existing target")
	}
	if err := owner.request(http.MethodPost, "/auth/organization/delete", map[string]string{"organizationId": ws.ID}, nil); err != nil {
		t.Fatal(err)
	}
	read = resource.ReadResponse{State: state}
	r.Read(t.Context(), resource.ReadRequest{State: state}, &read)
	if read.Diagnostics.HasError() || !read.State.Raw.IsNull() {
		t.Fatal(fmt.Sprint("parent deletion: ", read.Diagnostics))
	}
	deleted := resource.DeleteResponse{State: state}
	r.Delete(t.Context(), resource.DeleteRequest{State: state}, &deleted)
	if deleted.Diagnostics.HasError() {
		t.Fatal(deleted.Diagnostics)
	}
}
