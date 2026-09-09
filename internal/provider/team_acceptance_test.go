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

func TestAccTeamLifecycle(t *testing.T) {
	api := newAcceptanceAPI(t)
	const address = "kaneo_team.test"
	workspaces := acceptanceWorkspaceConfig("test", "Terraform team acceptance") + acceptanceWorkspaceConfig("other", "Terraform team replacement")
	config := func(name, workspace string) string {
		return api.providerConfig() + workspaces + fmt.Sprintf(`
resource "kaneo_team" "test" {
 workspace_id = kaneo_workspace.%s.id
 name = %q
}
`, workspace, name)
	}
	var id, workspaceID, seedID string
	check := func(name string, replaced bool) resource.TestCheckFunc {
		return resource.ComposeAggregateTestCheckFunc(resource.TestCheckResourceAttr(address, "name", name), func(state *terraform.State) error {
			r := state.RootModule().Resources[address]
			if id != "" && (r.Primary.ID != id) != replaced {
				return fmt.Errorf("unexpected team replacement")
			}
			id, workspaceID = r.Primary.ID, r.Primary.Attributes["workspace_id"]
			var teams []struct{ ID, OrganizationID, Name string }
			if err := api.request(http.MethodGet, "/auth/organization/list-teams?organizationId="+url.QueryEscape(workspaceID), nil, &teams); err != nil {
				return err
			}
			found := false
			for _, team := range teams {
				if team.OrganizationID != workspaceID {
					return fmt.Errorf("team outside workspace")
				}
				if team.ID == id {
					found = team.Name == name
				} else {
					seedID = team.ID
				}
			}
			if !found || len(teams) != 2 {
				return fmt.Errorf("expected managed and seeded team: %+v", teams)
			}
			return nil
		})
	}
	mutate := func(route string, data map[string]string) {
		body := map[string]any{"teamId": id, "organizationId": workspaceID}
		if data != nil {
			data["organizationId"] = workspaceID
			body = map[string]any{"teamId": id, "data": data}
		}
		if err := api.request(http.MethodPost, "/auth/organization/"+route, body, nil); err != nil {
			t.Fatal(err)
		}
	}
	action := func(a plancheck.ResourceActionType) resource.ConfigPlanChecks {
		return resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, a)}}
	}
	initial, renamed := config("test", "test"), config("renamed", "test")
	// Instance admin is not workspace membership and grants no team access.
	outsider := userAcceptanceSession(api, "team-outsider-"+uuid.NewV4().String()+"@example.com", uuid.NewV4().String())
	var signup struct{ User struct{ ID string } }
	if err := outsider.request(http.MethodPost, "/auth/sign-up/email", map[string]string{"email": outsider.username, "password": outsider.password, "name": "Team outsider"}, &signup); err != nil {
		t.Fatal(err)
	}
	if err := api.request(http.MethodPost, "/auth/admin/set-role", map[string]string{"userId": signup.User.ID, "role": "admin"}, nil); err != nil {
		t.Fatal(err)
	}
	denied := func() string {
		return outsider.providerConfig() + workspaces + `
resource "kaneo_team" "test" {
 workspace_id = kaneo_workspace.test.id
 name = "renamed"
}
`
	}
	resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: acceptanceProviderFactories, CheckDestroy: api.checkDestroy, Steps: []resource.TestStep{
		{Config: initial, Check: check("test", false)},
		{ResourceName: address, ImportState: true, ImportStateVerify: true, ImportStateIdFunc: func(_ *terraform.State) (string, error) { return workspaceID + "/" + id, nil }},
		{ResourceName: address, ImportState: true, ImportStateIdFunc: func(_ *terraform.State) (string, error) { return workspaceID + "/" + seedID, nil }, ImportStateCheck: func(states []*terraform.InstanceState) error {
			if len(states) != 1 || states[0].ID != seedID || states[0].Attributes["name"] != "Terraform team acceptance" {
				return fmt.Errorf("default team import failed")
			}
			return nil
		}},
		{Config: renamed, ConfigPlanChecks: action(plancheck.ResourceActionUpdate), Check: check("renamed", false)},
		{Config: renamed, ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}},
		{PreConfig: func() { mutate("update-team", map[string]string{"name": "drift"}) }, Config: renamed, ConfigPlanChecks: action(plancheck.ResourceActionUpdate), Check: check("renamed", false)},
		{Config: denied(), PlanOnly: true, ExpectError: regexp.MustCompile("Unable to Read Team")},
		{Config: renamed, ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}},
		{PreConfig: func() { mutate("remove-team", nil) }, Config: renamed, ConfigPlanChecks: action(plancheck.ResourceActionCreate), Check: check("renamed", true)},
		{Config: config("renamed", "other"), ConfigPlanChecks: action(plancheck.ResourceActionDestroyBeforeCreate), Check: check("renamed", true)},
		{PreConfig: func() {
			if err := api.request(http.MethodPost, "/auth/organization/delete", map[string]string{"organizationId": workspaceID}, nil); err != nil {
				t.Fatal(err)
			}
		}, Config: config("renamed", "other"), ConfigPlanChecks: action(plancheck.ResourceActionCreate), Check: check("renamed", true)},
		{Config: api.providerConfig() + workspaces, Check: func(_ *terraform.State) error {
			var teams []struct{ ID string }
			if err := api.request(http.MethodGet, "/auth/organization/list-teams?organizationId="+url.QueryEscape(workspaceID), nil, &teams); err != nil {
				return err
			}
			if len(teams) != 1 || teams[0].ID != seedID {
				return fmt.Errorf("delete did not preserve default team")
			}
			return nil
		}},
	}})
}

func TestAccTeamMaximum(t *testing.T) {
	api := newAcceptanceAPI(t)
	base := api.providerConfig() + acceptanceWorkspaceConfig("test", "Terraform team limit")
	config := base + `
resource "kaneo_team" "test" {
 workspace_id = kaneo_workspace.test.id
 name = "tenth team"
}
`
	var workspaceID string
	resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: acceptanceProviderFactories, CheckDestroy: api.checkDestroy, Steps: []resource.TestStep{
		{Config: base, Check: func(state *terraform.State) error {
			workspaceID = state.RootModule().Resources["kaneo_workspace.test"].Primary.ID
			// Seed plus eight unmanaged teams leaves exactly one available slot.
			for i := 0; i < 8; i++ {
				if err := api.request(http.MethodPost, "/auth/organization/create-team", map[string]string{"organizationId": workspaceID, "name": fmt.Sprintf("unmanaged %d", i)}, nil); err != nil {
					return err
				}
			}
			return nil
		}},
		{Config: config, Check: func(_ *terraform.State) error {
			var teams []struct{ ID string }
			if err := api.request(http.MethodGet, "/auth/organization/list-teams?organizationId="+url.QueryEscape(workspaceID), nil, &teams); err != nil {
				return err
			}
			if len(teams) != 10 {
				return fmt.Errorf("expected all ten teams, got %d", len(teams))
			}
			return nil
		}},
		{Config: config + `
resource "kaneo_team" "overflow" {
 workspace_id = kaneo_workspace.test.id
 name = "eleventh team"
}
`, ExpectError: regexp.MustCompile("YOU_HAVE_REACHED_THE_MAXIMUM_NUMBER_OF_TEAMS")},
		{Config: config, ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}},
	}})
}

func TestAccTeamLastTeamDeletion(t *testing.T) {
	api := newAcceptanceAPI(t)
	const address = "kaneo_team.test"
	base := api.providerConfig() + acceptanceWorkspaceConfig("test", "Terraform last team")
	config := base + `
resource "kaneo_team" "test" {
 workspace_id = kaneo_workspace.test.id
 name = "last"
}
`
	var id, workspaceID string
	resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: acceptanceProviderFactories, CheckDestroy: api.checkDestroy, Steps: []resource.TestStep{
		{Config: config, Check: func(state *terraform.State) error {
			r := state.RootModule().Resources[address]
			id, workspaceID = r.Primary.ID, r.Primary.Attributes["workspace_id"]
			return nil
		}},
		{PreConfig: func() {
			// The fixture's cookie session is separate from Terraform's session.
			// Clear active team explicitly so this tests the last-team restriction.
			if err := api.request(http.MethodPost, "/auth/organization/set-active-team", map[string]any{"teamId": nil}, nil); err != nil {
				t.Fatal(err)
			}
			var teams []struct{ ID string }
			if err := api.request(http.MethodGet, "/auth/organization/list-teams?organizationId="+url.QueryEscape(workspaceID), nil, &teams); err != nil {
				t.Fatal(err)
			}
			for _, team := range teams {
				if team.ID != id {
					if err := api.request(http.MethodPost, "/auth/organization/remove-team", map[string]string{"organizationId": workspaceID, "teamId": team.ID}, nil); err != nil {
						t.Fatal(err)
					}
				}
			}
		}, Config: base, ExpectError: regexp.MustCompile("Kaneo prohibits deleting the last team")},
		// The failed destroy must leave the same ID in state and in Kaneo.
		{Config: config, ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}, Check: func(state *terraform.State) error {
			if state.RootModule().Resources[address].Primary.ID != id {
				return fmt.Errorf("last team lost from state")
			}
			var teams []struct{ ID string }
			if err := api.request(http.MethodGet, "/auth/organization/list-teams?organizationId="+url.QueryEscape(workspaceID), nil, &teams); err != nil {
				return err
			}
			if len(teams) != 1 || teams[0].ID != id {
				return fmt.Errorf("last team missing from API")
			}
			return nil
		}},
		{PreConfig: func() {
			if err := api.request(http.MethodPost, "/auth/organization/create-team", map[string]string{"organizationId": workspaceID, "name": "spare for deletion"}, nil); err != nil {
				t.Fatal(err)
			}
		}, Config: base},
	}})
}
