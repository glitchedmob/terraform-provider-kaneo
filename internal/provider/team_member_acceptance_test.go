// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"testing"
	"uuid"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccTeamMemberSelfBootstrap(t *testing.T) {
	api := newAcceptanceAPI(t)
	base := api.providerConfig() + acceptanceWorkspaceConfig("test", "Self bootstrap") + `
resource "kaneo_team" "test" {
 workspace_id = kaneo_workspace.test.id
 name = "new empty team"
}
`
	config := base + fmt.Sprintf(`
resource "kaneo_team_member" "operator" {
 workspace_id = kaneo_workspace.test.id
 team_id = kaneo_team.test.id
 user_id = %q
}
`, api.userID)
	const address = "kaneo_team_member.operator"
	var ws, team, id string
	check := func(state *terraform.State) error {
		r := state.RootModule().Resources[address].Primary
		ws, team, id = r.Attributes["workspace_id"], r.Attributes["team_id"], r.ID
		var rows []struct{ ID, TeamID, UserID string }
		if err := api.request(http.MethodGet, "/auth/organization/list-team-members?teamId="+url.QueryEscape(team), nil, &rows); err != nil {
			return err
		}
		if len(rows) != 1 || rows[0].UserID != api.userID || rows[0].TeamID != team || id != teamMemberID(ws, team, api.userID) {
			return fmt.Errorf("self membership not established")
		}
		return nil
	}
	mutate := func(route string, body map[string]string) {
		t.Helper()
		if err := api.request(http.MethodPost, "/auth/organization/"+route, body, nil); err != nil {
			t.Fatal(err)
		}
	}
	empty := resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}
	recreate := resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionCreate)}}
	resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: acceptanceProviderFactories, CheckDestroy: api.checkDestroy, Steps: []resource.TestStep{
		{Config: config, Check: check},
		{ResourceName: address, ImportState: true, ImportStateVerify: true, ImportStateIdFunc: func(_ *terraform.State) (string, error) { return id, nil }},
		{Config: config, ConfigPlanChecks: empty},
		{PreConfig: func() {
			mutate("remove-team-member", map[string]string{"organizationId": ws, "teamId": team, "userId": api.userID})
		}, Config: config, ConfigPlanChecks: recreate, Check: check},
		{Config: base, Check: func(_ *terraform.State) error {
			// A denied list after self-delete is expected. The workspace and team survive.
			var teams []struct{ ID string }
			if err := api.request(http.MethodGet, "/auth/organization/list-teams?organizationId="+url.QueryEscape(ws), nil, &teams); err != nil {
				return err
			}
			if len(teams) != 2 {
				return fmt.Errorf("self delete changed teams")
			}
			if err := api.request(http.MethodGet, "/auth/organization/list-team-members?teamId="+url.QueryEscape(team), nil, nil); err == nil {
				return fmt.Errorf("self deletion left team access")
			}
			return nil
		}},
		{Config: config, Check: check},
		{PreConfig: func() { mutate("remove-team", map[string]string{"organizationId": ws, "teamId": team}) }, Config: config, ConfigPlanChecks: recreate, Check: check},
		{PreConfig: func() { mutate("delete", map[string]string{"organizationId": ws}) }, Config: config, ConfigPlanChecks: recreate, Check: check},
	}})
}

func TestAccTeamMemberLifecycle(t *testing.T) {
	api := newAcceptanceAPI(t)
	recipient := userAcceptanceSession(api, "team-member-"+uuid.NewV4().String()+"@example.com", uuid.NewV4().String())
	var signup struct{ User struct{ ID string } }
	if err := recipient.request(http.MethodPost, "/auth/sign-up/email", map[string]string{"email": recipient.username, "password": recipient.password, "name": "Team recipient"}, &signup); err != nil {
		t.Fatal(err)
	}
	user := signup.User.ID
	base := api.providerConfig() + acceptanceWorkspaceConfig("test", "Team members") + acceptanceWorkspaceConfig("other", "Unrelated memberships") + `
resource "kaneo_team" "test" {
 workspace_id = kaneo_workspace.test.id
 name = "new team"
}
` + fmt.Sprintf(`
resource "kaneo_team_member" "operator" {
 workspace_id = kaneo_workspace.test.id
 team_id = kaneo_team.test.id
 user_id = %q
}
`, api.userID)
	config := base + fmt.Sprintf(`
resource "kaneo_team_member" "test" {
 workspace_id = kaneo_workspace.test.id
 team_id = kaneo_team.test.id
 user_id = %q
 depends_on = [kaneo_team_member.operator]
}
`, user)
	const address = "kaneo_team_member.test"
	var ws, team, other, seed, invitation, id string
	mutate := func(a *acceptanceAPI, route string, body map[string]string) {
		t.Helper()
		if err := a.request(http.MethodPost, "/auth/organization/"+route, body, nil); err != nil {
			t.Fatal(err)
		}
	}
	membership := func(route, uid string) {
		mutate(api, route, map[string]string{"organizationId": ws, "teamId": team, "userId": uid})
	}
	checkRows := func(teamID string, expected ...string) error {
		var rows []struct{ ID, TeamID, UserID string }
		if err := api.request(http.MethodGet, "/auth/organization/list-team-members?teamId="+url.QueryEscape(teamID), nil, &rows); err != nil {
			return err
		}
		if len(rows) != len(expected) {
			return fmt.Errorf("unexpected team member count %d, want %d", len(rows), len(expected))
		}
		found := map[string]bool{}
		for _, row := range rows {
			if row.ID == "" || row.TeamID != teamID || found[row.UserID] {
				return fmt.Errorf("invalid member")
			}
			found[row.UserID] = true
		}
		for _, uid := range expected {
			if !found[uid] {
				return fmt.Errorf("missing expected member")
			}
		}
		return nil
	}
	check := func(state *terraform.State) error {
		id = state.RootModule().Resources[address].Primary.ID
		return checkRows(team, api.userID, user)
	}
	empty := resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}
	resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: acceptanceProviderFactories, CheckDestroy: api.checkDestroy, Steps: []resource.TestStep{
		{Config: base, Check: func(state *terraform.State) error {
			ws = state.RootModule().Resources["kaneo_workspace.test"].Primary.ID
			other = state.RootModule().Resources["kaneo_workspace.other"].Primary.ID
			team = state.RootModule().Resources["kaneo_team.test"].Primary.ID
			var invite struct{ ID string }
			if err := api.request(http.MethodPost, "/auth/organization/invite-member", map[string]string{"organizationId": ws, "email": recipient.username, "role": "member"}, &invite); err != nil {
				return err
			}
			invitation = invite.ID
			return nil
		}},
		{Config: config, ExpectError: regexp.MustCompile("pending invitation is not membership")},
		{Config: base, Check: func(_ *terraform.State) error {
			if err := checkRows(team, api.userID); err != nil {
				return err
			}
			var invites []struct{ ID, Status string }
			if err := api.request(http.MethodGet, "/auth/organization/list-invitations?organizationId="+url.QueryEscape(ws), nil, &invites); err != nil {
				return err
			}
			if len(invites) != 1 || invites[0].ID != invitation || invites[0].Status != "pending" {
				return fmt.Errorf("invitation changed")
			}
			var members struct{ Total int }
			if err := api.request(http.MethodGet, "/auth/organization/list-members?organizationId="+url.QueryEscape(ws), nil, &members); err != nil {
				return err
			}
			if members.Total != 1 {
				return fmt.Errorf("pending invitation granted workspace access")
			}
			return nil
		}},
		{PreConfig: func() {
			mutate(recipient, "accept-invitation", map[string]string{"invitationId": invitation})
			var teams []struct{ ID string }
			if err := api.request(http.MethodGet, "/auth/organization/list-teams?organizationId="+url.QueryEscape(ws), nil, &teams); err != nil {
				t.Fatal(err)
			}
			for _, row := range teams {
				if row.ID != team {
					seed = row.ID
				}
			}
			mutate(api, "add-team-member", map[string]string{"organizationId": ws, "teamId": seed, "userId": user})
			// Accepted membership in another workspace must survive target destruction.
			var invite struct{ ID string }
			if err := api.request(http.MethodPost, "/auth/organization/invite-member", map[string]string{"organizationId": other, "email": recipient.username, "role": "member"}, &invite); err != nil {
				t.Fatal(err)
			}
			mutate(recipient, "accept-invitation", map[string]string{"invitationId": invite.ID})
		}, Config: config, Check: check},
		{ResourceName: address, ImportState: true, ImportStateVerify: true, ImportStateIdFunc: func(_ *terraform.State) (string, error) { return id, nil }},
		{ResourceName: address, ImportState: true, ImportStateIdFunc: func(_ *terraform.State) (string, error) { return teamMemberID(other, team, user), nil }, ExpectError: regexp.MustCompile("non-existent remote object")},
		{Config: config, ConfigPlanChecks: empty},
		{PreConfig: func() { membership("remove-team-member", user) }, Config: config, ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionCreate)}}, Check: check},
		{PreConfig: func() { membership("remove-team-member", api.userID) }, Config: config, PlanOnly: true, ExpectError: regexp.MustCompile("operator cannot list")},
		{PreConfig: func() { membership("add-team-member", api.userID) }, Config: config, ConfigPlanChecks: empty, Check: check},
		{Config: base, Check: func(_ *terraform.State) error {
			if err := checkRows(team, api.userID); err != nil {
				return err
			}
			if err := checkRows(seed, api.userID, user); err != nil {
				return err
			}
			for _, workspace := range []string{ws, other} {
				var members struct {
					Members []struct{ UserID, Role string }
					Total   int
				}
				if err := api.request(http.MethodGet, "/auth/organization/list-members?organizationId="+url.QueryEscape(workspace), nil, &members); err != nil {
					return err
				}
				if members.Total != 2 {
					return fmt.Errorf("workspace members changed")
				}
				for _, m := range members.Members {
					if (m.UserID == user && m.Role != "member") || (m.UserID == api.userID && m.Role != "owner") {
						return fmt.Errorf("workspace roles changed")
					}
				}
			}
			// The recipient's account/session remains usable.
			return recipient.request(http.MethodGet, "/auth/get-session", nil, nil)
		}},
		{Config: config, Check: check}, // Final destroy must remove dependent before operator.
	}})
}
