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

func (a *acceptanceAPI) columns(projectID string) ([]map[string]any, error) {
	var columns []map[string]any
	err := a.request(http.MethodGet, "/column/"+url.PathEscape(projectID), nil, &columns)
	return columns, err
}

func (a *acceptanceAPI) checkColumn(address string) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		r, ok := state.RootModule().Resources[address]
		if !ok || r.Primary.ID == "" {
			return fmt.Errorf("%s has no column ID", address)
		}
		columns, err := a.columns(r.Primary.Attributes["project_id"])
		if err != nil {
			return err
		}
		for _, column := range columns {
			if column["id"] != r.Primary.ID {
				continue
			}
			for attribute, field := range map[string]string{"project_id": "projectId", "name": "name", "slug": "slug", "icon": "icon", "color": "color"} {
				value, _ := column[field].(string)
				if value != r.Primary.Attributes[attribute] {
					return fmt.Errorf("column %s: API %s=%q, state=%q", r.Primary.ID, field, value, r.Primary.Attributes[attribute])
				}
			}
			final, _ := column["isFinal"].(bool)
			position, _ := column["position"].(float64)
			if strconv.FormatBool(final) != r.Primary.Attributes["is_final"] || strconv.FormatFloat(position, 'f', -1, 64) != r.Primary.Attributes["position"] {
				return fmt.Errorf("column %s: final flag or position differs from state", r.Primary.ID)
			}
			return nil
		}
		return fmt.Errorf("column %s is absent from Kaneo", r.Primary.ID)
	}
}

func columnAcceptanceBase(api *acceptanceAPI) string {
	return api.providerConfig() + fmt.Sprintf(`
resource "kaneo_workspace" "test" {
 name = "Terraform Columns"
 slug = %q
}
resource "kaneo_project" "test" {
 workspace_id = kaneo_workspace.test.id
 name = "Terraform Columns"
 slug = "COL"
}
`, "terraform-"+uuid.NewV4().String())
}

func TestAccColumnLifecycle(t *testing.T) {
	api := newAcceptanceAPI(t)
	const address = "kaneo_column.test"
	base := columnAcceptanceBase(api)
	config := func(name, optional string) string {
		return base + fmt.Sprintf(`
resource "kaneo_column" "test" {
 project_id = kaneo_project.test.id
 name = %q
 %s
}
data "kaneo_column" "by_id" {
 project_id = kaneo_column.test.project_id
 id = kaneo_column.test.id
}
data "kaneo_column" "by_slug" {
 project_id = kaneo_column.test.project_id
 slug = kaneo_column.test.slug
}
`, name, optional)
	}
	var columnID, projectID, taskID string
	checks := func(name string, position int, final bool, optional ...resource.TestCheckFunc) resource.TestCheckFunc {
		checks := []resource.TestCheckFunc{
			api.checkColumn(address),
			resource.TestCheckResourceAttr(address, "name", name),
			resource.TestCheckResourceAttr(address, "slug", "testing"),
			resource.TestCheckResourceAttr(address, "position", strconv.Itoa(position)),
			resource.TestCheckResourceAttr(address, "is_final", strconv.FormatBool(final)),
			resource.TestCheckResourceAttrSet(address, "created_at"),
			resource.TestCheckResourceAttrSet(address, "updated_at"),
			func(state *terraform.State) error {
				columnID = state.RootModule().Resources[address].Primary.ID
				projectID = state.RootModule().Resources[address].Primary.Attributes["project_id"]
				columns, err := api.columns(projectID)
				if err != nil {
					return err
				}
				if len(columns) != 5 {
					return fmt.Errorf("expected four default columns and one managed column, got %d", len(columns))
				}
				for position, slug := range []string{"to-do", "in-progress", "in-review", "done"} {
					found := false
					for _, column := range columns {
						if column["slug"] == slug {
							found = true
							if column["position"] != float64(position) {
								return fmt.Errorf("default column %s was reordered", slug)
							}
						}
					}
					if !found {
						return fmt.Errorf("default column %s is missing", slug)
					}
				}
				return nil
			},
		}
		for _, lookup := range []string{"by_id", "by_slug"} {
			for _, field := range []string{"id", "project_id", "name", "slug", "position", "is_final", "created_at", "updated_at"} {
				checks = append(checks, resource.TestCheckResourceAttrPair("data.kaneo_column."+lookup, field, address, field))
			}
		}
		return resource.ComposeAggregateTestCheckFunc(append(checks, optional...)...)
	}
	initial := config("Testing", `icon = "FlaskConical"
 color = "#abcdef"
 is_final = true
 position = 8`)
	updated := config("Verified", `icon = "Check"
 color = "#123456"
 is_final = false
 position = 9`)
	cleared := config("Verified", `position = 9`)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acceptanceProviderFactories,
		CheckDestroy:             api.checkDestroy,
		Steps: []resource.TestStep{
			{Config: initial, Check: checks("Testing", 8, true, resource.TestCheckResourceAttr(address, "icon", "FlaskConical"), resource.TestCheckResourceAttr(address, "color", "#abcdef"))},
			{
				Config:           updated,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate)}},
				Check:            checks("Verified", 9, false, resource.TestCheckResourceAttr(address, "icon", "Check"), resource.TestCheckResourceAttr(address, "color", "#123456")),
			},
			{
				ResourceName: address, ImportState: true, ImportStateVerify: true,
				ImportStateIdFunc: func(state *terraform.State) (string, error) {
					r := state.RootModule().Resources[address]
					return r.Primary.Attributes["project_id"] + "/" + r.Primary.ID, nil
				},
			},
			{Config: updated, ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}},
			{Config: cleared, Check: checks("Verified", 9, false, resource.TestCheckNoResourceAttr(address, "icon"), resource.TestCheckNoResourceAttr(address, "color"))},
			{
				PreConfig: func() {
					if err := api.request(http.MethodPut, "/column/reorder/"+url.PathEscape(projectID), map[string]any{"columns": []map[string]any{{"id": columnID, "position": 6}}}, nil); err != nil {
						t.Fatal(err)
					}
				},
				Config:           cleared,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate)}},
				Check:            checks("Verified", 9, false),
			},
			{
				PreConfig: func() {
					var task map[string]any
					if err := api.request(http.MethodPost, "/task/"+url.PathEscape(projectID), map[string]string{
						"title": "Keep this column", "description": "Acceptance test", "status": "testing", "priority": "no-priority",
					}, &task); err != nil {
						t.Fatal(err)
					}
					taskID, _ = task["id"].(string)
					if taskID == "" {
						t.Fatal("created task has no ID")
					}
				},
				Config:      base,
				ExpectError: regexp.MustCompile(`Cannot delete column that contains tasks`),
			},
			{Config: cleared, Check: checks("Verified", 9, false)},
			{
				PreConfig: func() {
					if err := api.request(http.MethodDelete, "/task/"+url.PathEscape(taskID), nil, nil); err != nil {
						t.Fatal(err)
					}
				},
				Config: base,
				Check: func(_ *terraform.State) error {
					columns, err := api.columns(projectID)
					if err != nil {
						return err
					}
					for _, column := range columns {
						if column["id"] == columnID {
							return fmt.Errorf("column still exists after deletion")
						}
					}
					if len(columns) != 4 {
						return fmt.Errorf("expected default columns to remain, got %d", len(columns))
					}
					return nil
				},
			},
		},
	})
}

func TestAccColumnImportDefaults(t *testing.T) {
	api := newAcceptanceAPI(t)
	base := columnAcceptanceBase(api)
	var resources, imports string
	for _, column := range []struct {
		slug, name string
		final      bool
	}{
		{"to-do", "To Do", false}, {"in-progress", "In Progress", false},
		{"in-review", "In Review", false}, {"done", "Done", true},
	} {
		base += fmt.Sprintf(`
data "kaneo_column" %q {
 project_id = kaneo_project.test.id
 slug = %q
}
`, column.slug, column.slug)
		resources += fmt.Sprintf(`
resource "kaneo_column" %q {
 project_id = kaneo_project.test.id
 name = %q
 is_final = %t
}
`, column.slug, column.name, column.final)
		imports += fmt.Sprintf(`
import {
 to = kaneo_column.%s
 id = "${kaneo_project.test.id}/${data.kaneo_column.%s.id}"
}
`, column.slug, column.slug)
	}
	managed := base + resources
	imported := managed + imports
	originalIDs := map[string]string{}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acceptanceProviderFactories,
		CheckDestroy:             api.checkDestroy,
		Steps: []resource.TestStep{
			{Config: base, Check: func(state *terraform.State) error {
				for _, slug := range []string{"to-do", "in-progress", "in-review", "done"} {
					originalIDs[slug] = state.RootModule().Resources["data.kaneo_column."+slug].Primary.ID
				}
				return nil
			}},
			{Config: managed, ExpectError: regexp.MustCompile(`already\s+exists\s+in\s+this\s+project`)},
			{Config: imported, Check: func(state *terraform.State) error {
				projectID := state.RootModule().Resources["kaneo_project.test"].Primary.ID
				columns, err := api.columns(projectID)
				if err != nil {
					return err
				}
				if len(columns) != 4 {
					return fmt.Errorf("import created duplicate columns: got %d", len(columns))
				}
				for slug, id := range originalIDs {
					address := "kaneo_column." + slug
					if state.RootModule().Resources[address].Primary.ID != id {
						return fmt.Errorf("import replaced default column %s", slug)
					}
					if err := api.checkColumn(address)(state); err != nil {
						return err
					}
				}
				return nil
			}},
			{Config: managed, ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}},
		},
	})
}

func TestAccColumnDeletedOutsideTerraform(t *testing.T) {
	api := newAcceptanceAPI(t)
	const address = "kaneo_column.test"
	base := columnAcceptanceBase(api) + `
resource "kaneo_project" "other" {
 workspace_id = kaneo_workspace.test.id
 name = "Other Project"
 slug = "OTHER"
}
`
	config := func(project string) string {
		return base + fmt.Sprintf(`
resource "kaneo_column" "test" {
 project_id = kaneo_project.%s.id
 name = "Testing"
}
`, project)
	}
	var originalID, recreatedID, originalProjectID string
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acceptanceProviderFactories,
		CheckDestroy:             api.checkDestroy,
		Steps: []resource.TestStep{
			{Config: config("test"), Check: resource.ComposeAggregateTestCheckFunc(api.checkColumn(address),
				resource.TestCheckResourceAttr(address, "position", "4"), resource.TestCheckResourceAttr(address, "is_final", "false"),
				func(state *terraform.State) error {
					r := state.RootModule().Resources[address]
					originalID = r.Primary.ID
					originalProjectID = r.Primary.Attributes["project_id"]
					return nil
				})},
			{
				PreConfig: func() {
					if err := api.request(http.MethodDelete, "/column/"+url.PathEscape(originalID), nil, nil); err != nil {
						t.Fatal(err)
					}
				},
				Config:           config("test"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionCreate)}},
				Check: resource.ComposeAggregateTestCheckFunc(api.checkColumn(address), func(state *terraform.State) error {
					recreatedID = state.RootModule().Resources[address].Primary.ID
					if recreatedID == originalID {
						return fmt.Errorf("column was not recreated")
					}
					return nil
				}),
			},
			{
				Config:           config("other"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionDestroyBeforeCreate)}},
				Check: resource.ComposeAggregateTestCheckFunc(api.checkColumn(address), func(state *terraform.State) error {
					if state.RootModule().Resources[address].Primary.ID == recreatedID {
						return fmt.Errorf("project change did not replace column")
					}
					columns, err := api.columns(originalProjectID)
					if err != nil {
						return err
					}
					for _, column := range columns {
						if column["id"] == recreatedID {
							return fmt.Errorf("old column still exists")
						}
					}
					return nil
				}),
			},
		},
	})
}
