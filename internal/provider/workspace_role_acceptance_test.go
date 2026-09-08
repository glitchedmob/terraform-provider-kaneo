// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"testing"
	"uuid"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccWorkspaceRoleLifecycle(t *testing.T) {
	api := newAcceptanceAPI(t)
	const address = "kaneo_workspace_role.test"
	// A separate instance admin has no membership in the owner's workspace.
	outsider := userAcceptanceSession(api, "role-outsider-"+uuid.NewV4().String()+"@example.com", uuid.NewV4().String())
	var signup struct{ User struct{ ID string } }
	if err := outsider.request(http.MethodPost, "/auth/sign-up/email", map[string]string{"email": outsider.username, "password": outsider.password, "name": "Role outsider"}, &signup); err != nil {
		t.Fatal(err)
	}
	if err := api.request(http.MethodPost, "/auth/admin/set-role", map[string]string{"userId": signup.User.ID, "role": "admin"}, nil); err != nil {
		t.Fatal(err)
	}
	workspaces := acceptanceWorkspaceConfig("test", "Terraform role acceptance") +
		acceptanceWorkspaceConfig("other", "Terraform role replacement")
	config := func(name, permissions, workspace string, forbidden bool) string {
		operator := api
		if forbidden {
			operator = outsider
		}
		return operator.providerConfig() + workspaces + fmt.Sprintf(`
resource "kaneo_workspace_role" "test" {
 workspace_id = kaneo_workspace.%s.id
 name = %q
 permissions = %s
}
`, workspace, name, permissions)
	}
	var id, workspaceID string
	check := func(name string, actions []string, replaced bool) resource.TestCheckFunc {
		return resource.ComposeAggregateTestCheckFunc(resource.TestCheckResourceAttr(address, "name", name), func(state *terraform.State) error {
			r := state.RootModule().Resources[address]
			if id != "" && (r.Primary.ID != id) != replaced {
				return fmt.Errorf("unexpected role replacement")
			}
			id, workspaceID = r.Primary.ID, r.Primary.Attributes["workspace_id"]
			var role struct {
				ID, OrganizationID, Role string
				Permission               map[string][]string
			}
			if err := api.request(http.MethodGet, "/auth/organization/get-role?organizationId="+url.QueryEscape(workspaceID)+"&roleId="+url.QueryEscape(id), nil, &role); err != nil {
				return err
			}
			sort.Strings(role.Permission["task"])
			want := append([]string{}, actions...)
			sort.Strings(want)
			if role.ID != id || role.OrganizationID != workspaceID || role.Role != name || !reflect.DeepEqual(role.Permission["task"], want) {
				return fmt.Errorf("role API differs from state: %+v", role)
			}
			return nil
		})
	}
	mutate := func(path string, data map[string]any) {
		body := map[string]any{"organizationId": workspaceID, "roleId": id}
		if data != nil {
			body["data"] = data
		}
		if err := api.request(http.MethodPost, "/auth/organization/"+path, body, nil); err != nil {
			t.Fatal(err)
		}
	}
	absent := func(_ *terraform.State) error {
		var roles []struct{ ID string }
		if err := api.request(http.MethodGet, "/auth/organization/list-roles?organizationId="+url.QueryEscape(workspaceID), nil, &roles); err != nil {
			return err
		}
		for _, role := range roles {
			if role.ID == id {
				return fmt.Errorf("deleted role still exists")
			}
		}
		return nil
	}
	initial := config("custom", `{task = ["update", "read"], project = []}`, "test", false)
	renamed := config("renamed", `{task = ["read", "update"], project = []}`, "test", false)
	changed := config("renamed", `{task = ["read"]}`, "test", false)
	action := func(a plancheck.ResourceActionType) resource.ConfigPlanChecks {
		return resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, a)}}
	}
	importSeed := func(name string) resource.TestStep {
		return resource.TestStep{ResourceName: address, ImportState: true, ImportStateIdFunc: func(_ *terraform.State) (string, error) {
			var roles []struct{ ID, Role string }
			if err := api.request(http.MethodGet, "/auth/organization/list-roles?organizationId="+url.QueryEscape(workspaceID), nil, &roles); err != nil {
				return "", err
			}
			for _, role := range roles {
				if role.Role == name {
					return workspaceID + "/" + role.ID, nil
				}
			}
			return "", fmt.Errorf("seeded %s not found", name)
		}, ImportStateCheck: func(states []*terraform.InstanceState) error {
			if len(states) != 1 || states[0].Attributes["name"] != name {
				return fmt.Errorf("seed import failed for %s", name)
			}
			return nil
		}}
	}
	resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: acceptanceProviderFactories, CheckDestroy: api.checkDestroy, Steps: []resource.TestStep{
		{Config: initial, Check: check("custom", []string{"read", "update"}, false)},
		{ResourceName: address, ImportState: true, ImportStateVerify: true, ImportStateIdFunc: func(_ *terraform.State) (string, error) { return workspaceID + "/" + id, nil }},
		{Config: renamed, ConfigPlanChecks: action(plancheck.ResourceActionUpdate), Check: check("renamed", []string{"read", "update"}, false)},
		{Config: changed, ConfigPlanChecks: action(plancheck.ResourceActionUpdate), Check: check("renamed", []string{"read"}, false)},
		{Config: config("renamed", `{}`, "test", false), ConfigPlanChecks: action(plancheck.ResourceActionUpdate), Check: resource.TestCheckResourceAttr(address, "permissions.%", "0")},
		{Config: changed, ConfigPlanChecks: action(plancheck.ResourceActionUpdate), Check: check("renamed", []string{"read"}, false)},
		{Config: changed, ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}},
		{PreConfig: func() {
			mutate("update-role", map[string]any{"permission": map[string][]string{"task": {"read", "update"}}})
		}, Config: changed, ConfigPlanChecks: action(plancheck.ResourceActionUpdate), Check: check("renamed", []string{"read"}, false)},
		{Config: config("renamed", `{task = ["read"]}`, "test", true), PlanOnly: true, ExpectError: regexp.MustCompile("Unable to Read Workspace Role")},
		{Config: changed, ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}},
		{PreConfig: func() {
			var roles []struct{ ID, Role string }
			if err := api.request(http.MethodGet, "/auth/organization/list-roles?organizationId="+url.QueryEscape(workspaceID), nil, &roles); err != nil {
				t.Fatal(err)
			}
			for _, role := range roles {
				if role.Role == "viewer" {
					if err := api.request(http.MethodPost, "/auth/organization/update-role", map[string]any{"organizationId": workspaceID, "roleId": role.ID, "data": map[string]any{"permission": map[string][]string{"task": {"read"}}}}, nil); err != nil {
						t.Fatal(err)
					}
				}
			}
			var invitation struct{ ID string }
			if err := api.request(http.MethodPost, "/auth/organization/invite-member", map[string]string{"organizationId": workspaceID, "email": outsider.username, "role": "viewer"}, &invitation); err != nil {
				t.Fatal(err)
			}
			if err := outsider.request(http.MethodPost, "/auth/organization/accept-invitation", map[string]string{"invitationId": invitation.ID}, nil); err != nil {
				t.Fatal(err)
			}
		}, Config: config("renamed", `{task = ["read"]}`, "test", true), PlanOnly: true, ExpectError: regexp.MustCompile("Unable to Read Workspace Role")},
		{Config: changed, ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}},
		importSeed("viewer"),
		importSeed("member"),
		importSeed("admin"),
		{PreConfig: func() { mutate("delete-role", nil) }, Config: changed, ConfigPlanChecks: action(plancheck.ResourceActionCreate), Check: check("renamed", []string{"read"}, true)},
		{Config: config("renamed", `{task = ["read"]}`, "other", false), ConfigPlanChecks: action(plancheck.ResourceActionDestroyBeforeCreate), Check: check("renamed", []string{"read"}, true)},
		{PreConfig: func() {
			if err := api.request(http.MethodPost, "/auth/organization/delete", map[string]string{"organizationId": workspaceID}, nil); err != nil {
				t.Fatal(err)
			}
		}, Config: config("renamed", `{task = ["read"]}`, "other", false), ConfigPlanChecks: action(plancheck.ResourceActionCreate), Check: check("renamed", []string{"read"}, true)},
		{Config: api.providerConfig() + workspaces, Check: absent},
	}})
}
