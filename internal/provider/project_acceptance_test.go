// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"testing"
	"uuid"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// Read raw API responses so these checks do not reuse the generated client or
// the provider's state mapping.
func (a *acceptanceAPI) project(workspaceID, id string) (map[string]any, error) {
	var projects []map[string]any
	if err := a.request(http.MethodGet, "/project?includeArchived=true&workspaceId="+url.QueryEscape(workspaceID), nil, &projects); err != nil {
		return nil, err
	}
	for _, project := range projects {
		if project["id"] == id {
			return project, nil
		}
	}
	return nil, nil
}

func (a *acceptanceAPI) checkProject(address string) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		r, ok := state.RootModule().Resources[address]
		if !ok || r.Primary.ID == "" {
			return fmt.Errorf("%s has no project ID", address)
		}
		project, err := a.project(r.Primary.Attributes["workspace_id"], r.Primary.ID)
		if err != nil {
			return err
		}
		if project == nil {
			return fmt.Errorf("project %s is absent from Kaneo", r.Primary.ID)
		}
		for attribute, field := range map[string]string{"workspace_id": "workspaceId", "name": "name", "slug": "slug", "icon": "icon", "description": "description"} {
			value, _ := project[field].(string)
			if value != r.Primary.Attributes[attribute] {
				return fmt.Errorf("project %s: API %s=%q, state=%q", r.Primary.ID, field, value, r.Primary.Attributes[attribute])
			}
		}
		public, _ := project["isPublic"].(bool)
		if strconv.FormatBool(public) != r.Primary.Attributes["is_public"] {
			return fmt.Errorf("project %s: API visibility does not match state", r.Primary.ID)
		}
		return nil
	}
}

func TestAccProjectLifecycle(t *testing.T) {
	api := newAcceptanceAPI(t)
	const address = "kaneo_project.test"
	workspaceConfig := api.providerConfig() + fmt.Sprintf(`
resource "kaneo_workspace" "test" {
 name = "Terraform Projects"
 slug = %q
}
`, "terraform-"+uuid.NewV4().String())
	config := func(name, slug, optional string) string {
		return workspaceConfig + fmt.Sprintf(`
resource "kaneo_project" "test" {
 workspace_id = kaneo_workspace.test.id
 name = %q
 slug = %q
 %s
}
data "kaneo_project" "by_id" {
 id = kaneo_project.test.id
}
data "kaneo_project" "by_slug" {
 workspace_id = kaneo_project.test.workspace_id
 slug = kaneo_project.test.slug
}
`, name, slug, optional)
	}
	var projectID, workspaceID, duplicateID string
	checks := func(name, slug, icon, description string, public bool) resource.TestCheckFunc {
		checks := []resource.TestCheckFunc{
			api.checkProject(address),
			resource.TestCheckResourceAttr(address, "name", name),
			resource.TestCheckResourceAttr(address, "slug", slug),
			resource.TestCheckResourceAttr(address, "icon", icon),
			resource.TestCheckResourceAttr(address, "description", description),
			resource.TestCheckResourceAttr(address, "is_public", strconv.FormatBool(public)),
			resource.TestCheckResourceAttrSet(address, "created_at"),
			func(state *terraform.State) error {
				projectID = state.RootModule().Resources[address].Primary.ID
				workspaceID = state.RootModule().Resources[address].Primary.Attributes["workspace_id"]
				return nil
			},
		}
		for _, lookup := range []string{"by_id", "by_slug"} {
			for _, field := range []string{"id", "workspace_id", "name", "slug", "icon", "description", "is_public", "created_at"} {
				checks = append(checks, resource.TestCheckResourceAttrPair("data.kaneo_project."+lookup, field, address, field))
			}
		}
		return resource.ComposeAggregateTestCheckFunc(checks...)
	}
	initial := config("Terraform Project", "TST", `icon = "Folder"
 description = "Created with a description"
 is_public = true`)
	updated := config("Renamed Project", "NEW", `icon = "Code"
 description = "Updated description"
 is_public = false`)
	cleared := config("Renamed Project", "NEW", "")
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acceptanceProviderFactories,
		CheckDestroy:             api.checkDestroy,
		Steps: []resource.TestStep{
			{Config: initial, Check: checks("Terraform Project", "TST", "Folder", "Created with a description", true)},
			{
				Config:           updated,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate)}},
				Check:            checks("Renamed Project", "NEW", "Code", "Updated description", false),
			},
			{ResourceName: address, ImportState: true, ImportStateVerify: true},
			{Config: updated, ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}},
			{Config: cleared, Check: checks("Renamed Project", "NEW", "Layout", "", false)},
			{
				PreConfig: func() {
					if err := api.request(http.MethodPut, "/project/"+url.PathEscape(projectID)+"/archive", nil, nil); err != nil {
						t.Fatal(err)
					}
				},
				Config:           cleared,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
				Check: resource.ComposeAggregateTestCheckFunc(
					checks("Renamed Project", "NEW", "Layout", "", false),
					resource.TestCheckResourceAttrSet(address, "archived_at"),
					resource.TestCheckResourceAttrPair("data.kaneo_project.by_id", "archived_at", address, "archived_at"),
					resource.TestCheckResourceAttrPair("data.kaneo_project.by_slug", "archived_at", address, "archived_at"),
				),
			},
			{
				PreConfig: func() {
					var duplicate map[string]any
					if err := api.request(http.MethodPost, "/project", map[string]string{
						"workspaceId": workspaceID, "name": "Duplicate slug", "slug": "NEW", "icon": "Layout",
					}, &duplicate); err != nil {
						t.Fatal(err)
					}
					duplicateID, _ = duplicate["id"].(string)
					if duplicateID == "" {
						t.Fatal("duplicate project has no ID")
					}
				},
				Config:      cleared,
				ExpectError: regexp.MustCompile(`multiple projects have slug`),
			},
			{
				PreConfig: func() {
					if err := api.request(http.MethodDelete, "/project/"+url.PathEscape(duplicateID), nil, nil); err != nil {
						t.Fatal(err)
					}
				},
				Config: workspaceConfig,
				Check: func(_ *terraform.State) error {
					project, err := api.project(workspaceID, projectID)
					if err != nil {
						return err
					}
					if project != nil {
						return fmt.Errorf("project %s still exists after deletion", projectID)
					}
					return nil
				},
			},
		},
	})
}

func TestAccProjectDeletedOutsideTerraform(t *testing.T) {
	api := newAcceptanceAPI(t)
	const address = "kaneo_project.test"
	workspaceConfig := api.providerConfig() + fmt.Sprintf(`
resource "kaneo_workspace" "test" {
 name = "Project Drift"
 slug = %q
}
resource "kaneo_workspace" "other" {
 name = "Replacement Workspace"
 slug = %q
}
`, "terraform-"+uuid.NewV4().String(), "terraform-"+uuid.NewV4().String())
	config := func(workspace string) string {
		return workspaceConfig + fmt.Sprintf(`
resource "kaneo_project" "test" {
 workspace_id = kaneo_workspace.%s.id
 name = "Project Drift"
 slug = "DRIFT"
}
`, workspace)
	}
	var originalID, replacementID, originalWorkspaceID string
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acceptanceProviderFactories,
		CheckDestroy:             api.checkDestroy,
		Steps: []resource.TestStep{
			{
				Config: config("test"),
				Check: resource.ComposeAggregateTestCheckFunc(api.checkProject(address),
					resource.TestCheckResourceAttr(address, "icon", "Layout"),
					resource.TestCheckResourceAttr(address, "description", ""),
					resource.TestCheckResourceAttr(address, "is_public", "false"),
					func(state *terraform.State) error {
						originalID = state.RootModule().Resources[address].Primary.ID
						originalWorkspaceID = state.RootModule().Resources[address].Primary.Attributes["workspace_id"]
						return nil
					}),
			},
			{
				PreConfig: func() {
					if err := api.request(http.MethodDelete, "/project/"+url.PathEscape(originalID), nil, nil); err != nil {
						t.Fatal(err)
					}
				},
				Config:           config("test"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionCreate)}},
				Check: resource.ComposeAggregateTestCheckFunc(api.checkProject(address), func(state *terraform.State) error {
					replacementID = state.RootModule().Resources[address].Primary.ID
					if replacementID == originalID {
						return fmt.Errorf("expected a new project ID after recreation")
					}
					return nil
				}),
			},
			{
				Config:           config("other"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionDestroyBeforeCreate)}},
				Check: resource.ComposeAggregateTestCheckFunc(api.checkProject(address), func(state *terraform.State) error {
					if state.RootModule().Resources[address].Primary.ID == replacementID {
						return fmt.Errorf("workspace change did not replace project")
					}
					project, err := api.project(originalWorkspaceID, replacementID)
					if err != nil {
						return err
					}
					if project != nil {
						return fmt.Errorf("replaced project still exists in original workspace")
					}
					return nil
				}),
			},
		},
	})
}
