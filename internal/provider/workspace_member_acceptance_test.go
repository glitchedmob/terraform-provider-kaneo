// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"uuid"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccWorkspaceMemberLifecycle(t *testing.T) {
	api := newAcceptanceAPI(t)
	email, password := "member-"+uuid.NewV4().String()+"@example.com", uuid.NewV4().String()
	recipient := userAcceptanceSession(api, email, password)
	const address = "kaneo_workspace_member.test"
	base := api.providerConfig() + fmt.Sprintf(`
resource "kaneo_workspace" "test" {
 name = "Terraform membership acceptance"
 slug = %q
}
resource "kaneo_workspace" "other" {
 name = "Unrelated workspace"
 slug = %q
}
resource "kaneo_user" "recipient" {
 name = "Independent recipient"
 email = %q
 password_wo = %q
 password_wo_version = 1
 email_verified = true
}
resource "kaneo_workspace_role" "test" {
 workspace_id = kaneo_workspace.test.id
 name = "reviewer"
 permissions = {task = ["read"]}
}
`, "member-"+uuid.NewV4().String(), "other-"+uuid.NewV4().String(), email, password)
	config := func(role string) string {
		return base + fmt.Sprintf(`
resource "kaneo_workspace_member" "test" {
 workspace_id = kaneo_workspace.test.id
 email = kaneo_user.recipient.email
 role = %s
}
`, role)
	}
	var id, ws, other, invitation, member, userID string
	var priorInvites int
	check := func(status, role string, stable bool) resource.TestCheckFunc {
		return func(state *terraform.State) error {
			r := state.RootModule().Resources[address].Primary
			if stable && id != "" && id != r.ID {
				return fmt.Errorf("pending to accepted transition replaced logical ID")
			}
			id, ws = r.ID, r.Attributes["workspace_id"]
			other = state.RootModule().Resources["kaneo_workspace.other"].Primary.ID
			userID = state.RootModule().Resources["kaneo_user.recipient"].Primary.ID
			if r.Attributes["status"] != status || r.Attributes["role"] != role {
				return fmt.Errorf("unexpected membership state")
			}
			var invites []struct{ ID, Email, Status, Role, OrganizationID string }
			if err := api.request(http.MethodGet, "/auth/organization/list-invitations?organizationId="+url.QueryEscape(ws), nil, &invites); err != nil {
				return err
			}
			priorInvites = len(invites)
			invitation = ""
			for _, i := range invites {
				if i.Email == email && i.Status == "pending" {
					if invitation != "" || i.Role != role || i.OrganizationID != ws {
						return fmt.Errorf("ambiguous or wrong pending invitation")
					}
					invitation = i.ID
				}
			}
			var members struct {
				Members []struct{ ID, UserID, OrganizationID, Role string }
				Total   int
			}
			if err := api.request(http.MethodGet, "/auth/organization/list-members?organizationId="+url.QueryEscape(ws)+"&limit=100&offset=0&sortBy=id&sortDirection=asc", nil, &members); err != nil {
				return err
			}
			member = ""
			ownerFound := false
			for _, m := range members.Members {
				if m.UserID == api.userID {
					ownerFound = true
				}
				if m.UserID == userID {
					member = m.ID
					if m.Role != role || m.OrganizationID != ws {
						return fmt.Errorf("accepted role differs")
					}
				}
			}
			if !ownerFound || (status == "pending" && (invitation == "" || member != "")) || (status == "accepted" && (member == "" || invitation != "")) {
				return fmt.Errorf("HTTP membership does not match lifecycle")
			}
			if r.Attributes["member_id"] != member || r.Attributes["invitation_id"] != invitation {
				return fmt.Errorf("backing IDs differ from HTTP")
			}
			// The recipient remains absent from a different workspace throughout.
			if err := api.request(http.MethodGet, "/auth/organization/list-members?organizationId="+url.QueryEscape(other)+"&limit=100&offset=0&sortBy=id&sortDirection=asc", nil, &members); err != nil {
				return err
			}
			if members.Total != 1 || len(members.Members) != 1 || members.Members[0].UserID != api.userID {
				return fmt.Errorf("unrelated workspace members changed")
			}
			return nil
		}
	}
	mutate := func(operator *acceptanceAPI, path string, body map[string]string) {
		t.Helper()
		if err := operator.request(http.MethodPost, "/auth/organization/"+path, body, nil); err != nil {
			t.Fatal(err)
		}
	}
	accept := func() {
		if err := recipient.request(http.MethodPost, "/auth/sign-in/email", map[string]string{"email": email, "password": password}, nil); err != nil {
			t.Fatal(err)
		}
		var result struct {
			Invitation struct{ ID, Status string }
			Member     struct{ ID, UserID, OrganizationID string }
		}
		if err := recipient.request(http.MethodPost, "/auth/organization/accept-invitation", map[string]string{"invitationId": invitation}, &result); err != nil {
			t.Fatal(err)
		}
		if result.Invitation.ID != invitation || result.Invitation.Status != "accepted" || result.Member.UserID != userID || result.Member.OrganizationID != ws || result.Member.ID == "" {
			t.Fatal("recipient acceptance did not establish expected member")
		}
	}
	absent := func(_ *terraform.State) error {
		var invitations []struct{ Email, Status string }
		if err := api.request(http.MethodGet, "/auth/organization/list-invitations?organizationId="+url.QueryEscape(ws), nil, &invitations); err != nil {
			return err
		}
		for _, i := range invitations {
			if i.Email == email && i.Status == "pending" {
				return fmt.Errorf("pending invite survived destroy")
			}
		}
		var members struct {
			Members []struct{ UserID string }
			Total   int
		}
		if err := api.request(http.MethodGet, "/auth/organization/list-members?organizationId="+url.QueryEscape(ws), nil, &members); err != nil {
			return err
		}
		if members.Total != 1 || len(members.Members) != 1 || members.Members[0].UserID != api.userID {
			return fmt.Errorf("target survived removal or unrelated owner was removed")
		}
		var user struct{ ID string }
		if err := api.request(http.MethodGet, "/auth/admin/get-user?id="+url.QueryEscape(userID), nil, &user); err != nil {
			return err
		}
		if user.ID != userID {
			return fmt.Errorf("membership destroy touched user")
		}
		return nil
	}
	empty := resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}
	resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: acceptanceProviderFactories, CheckDestroy: api.checkDestroy, Steps: []resource.TestStep{
		{Config: config(`"member"`), Check: check("pending", "member", false)},
		{ResourceName: address, ImportState: true, ImportStateVerify: true, ImportStateIdFunc: func(_ *terraform.State) (string, error) { return id, nil }},
		{Config: config(`kaneo_workspace_role.test.name`), Check: check("pending", "reviewer", true)},
		{Config: config(`kaneo_workspace_role.test.name`), ConfigPlanChecks: empty, Check: func(state *terraform.State) error {
			before := priorInvites
			if err := check("pending", "reviewer", true)(state); err != nil {
				return err
			}
			if before != priorInvites {
				return fmt.Errorf("refresh resent invite")
			}
			return nil
		}},
		{Config: base, Check: absent}, // Pending destroy, without deleting the workspace or user.
		{Config: config(`"member"`), Check: check("pending", "member", true)},
		{PreConfig: func() { mutate(api, "cancel-invitation", map[string]string{"invitationId": invitation}) }, Config: config(`"member"`), ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionCreate)}}, Check: check("pending", "member", true)},
		{PreConfig: func() {
			if err := recipient.request(http.MethodPost, "/auth/sign-in/email", map[string]string{"email": email, "password": password}, nil); err != nil {
				t.Fatal(err)
			}
			mutate(recipient, "reject-invitation", map[string]string{"invitationId": invitation})
		}, Config: config(`"member"`), Check: check("pending", "member", true)},
		{PreConfig: accept, Config: config(`"member"`), ConfigPlanChecks: empty, Check: check("accepted", "member", true)},
		{ResourceName: address, ImportState: true, ImportStateVerify: true, ImportStateIdFunc: func(_ *terraform.State) (string, error) { return id, nil }},
		{Config: config(`kaneo_workspace_role.test.name`), Check: check("accepted", "reviewer", true)},
		{Config: config(`kaneo_workspace_role.test.name`), ConfigPlanChecks: empty, Check: check("accepted", "reviewer", true)},
		{PreConfig: func() {
			mutate(api, "remove-member", map[string]string{"organizationId": ws, "memberIdOrEmail": member})
		}, Config: config(`kaneo_workspace_role.test.name`), Check: check("pending", "reviewer", true)},
		{PreConfig: accept, Config: config(`kaneo_workspace_role.test.name`), ConfigPlanChecks: empty, Check: check("accepted", "reviewer", true)},
		{Config: base, Check: absent}, // Accepted destroy confirmed before workspace deletion.
		{Config: config(`"member"`), Check: check("pending", "member", true)},
		{PreConfig: func() { mutate(api, "delete", map[string]string{"organizationId": ws}) }, Config: config(`"member"`), Check: check("pending", "member", false)},
	}})
}
