// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func (a *acceptanceAPI) label(id string) (map[string]any, error) {
	var label map[string]any
	err := a.request(http.MethodGet, "/label/"+url.PathEscape(id), nil, &label)
	return label, err
}

func (a *acceptanceAPI) checkLabel(address string) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		r, ok := state.RootModule().Resources[address]
		if !ok || r.Primary.ID == "" {
			return fmt.Errorf("%s has no label ID", address)
		}
		label, err := a.label(r.Primary.ID)
		if err != nil {
			return err
		}
		if label["id"] != r.Primary.ID || label["taskId"] != nil {
			return fmt.Errorf("%s is not the expected workspace label", address)
		}
		for attribute, field := range map[string]string{"name": "name", "color": "color", "workspace_id": "workspaceId"} {
			if label[field] != r.Primary.Attributes[attribute] {
				return fmt.Errorf("%s: API %s=%v, state=%q", address, field, label[field], r.Primary.Attributes[attribute])
			}
		}
		return nil
	}
}

func (a *acceptanceAPI) labelAbsent(workspaceID, id string) error {
	var labels []map[string]any
	if err := a.request(http.MethodGet, "/label/workspace/"+url.PathEscape(workspaceID), nil, &labels); err != nil {
		return err
	}
	for _, label := range labels {
		if label["id"] == id {
			return fmt.Errorf("label %s still exists", id)
		}
	}
	return nil
}

func TestAccLabelAndTaskLabelLifecycle(t *testing.T) {
	api := newAcceptanceAPI(t)
	const labelAddress, attachmentAddress = "kaneo_label.test", "kaneo_task_label.test"
	base := api.projectConfig("Terraform Tasks", "TASK") + `
resource "kaneo_task" "one" {
 project_id = kaneo_project.test.id
 title = "First task"
}
resource "kaneo_task" "two" {
 project_id = kaneo_project.test.id
 title = "Second task"
}
resource "kaneo_label" "other" {
 workspace_id = kaneo_workspace.test.id
 name = "Feature"
 color = "#abcdef"
}
`
	config := func(name, color, source, task string) string {
		result := base + fmt.Sprintf(`
resource "kaneo_label" "test" {
 workspace_id = kaneo_workspace.test.id
 name = %q
 color = %q
}
data "kaneo_label" "by_id" {
 id = kaneo_label.test.id
}
`, name, color)
		if source != "" {
			result += fmt.Sprintf(`
resource "kaneo_task_label" "test" {
 label_id = kaneo_label.%s.id
 task_id = kaneo_task.%s.id
}
`, source, task)
		}
		return result
	}
	initial := config("Bug", "#ef4444", "test", "one")
	renamed := config("Issue", "#123ABC", "test", "one")
	moved := config("Issue", "#123ABC", "test", "two")
	changedSource := config("Issue", "#123ABC", "other", "two")
	var copyID, manualID, workspaceID, taskOneID, cascadeCopyID string
	check := func(name, color string, replaced bool) resource.TestCheckFunc {
		return resource.ComposeAggregateTestCheckFunc(
			api.checkLabel(labelAddress), api.checkLabel("data.kaneo_label.by_id"),
			resource.TestCheckResourceAttrSet(labelAddress, "created_at"), resource.TestCheckResourceAttrSet(labelAddress, "updated_at"),
			resource.TestCheckResourceAttrPair("data.kaneo_label.by_id", "name", labelAddress, "name"),
			resource.TestCheckResourceAttrPair("data.kaneo_label.by_id", "color", labelAddress, "color"),
			func(state *terraform.State) error {
				r := state.RootModule().Resources[attachmentAddress]
				if copyID != "" && (r.Primary.ID != copyID) != replaced {
					return fmt.Errorf("unexpected attachment replacement: old=%s new=%s", copyID, r.Primary.ID)
				}
				copyID, workspaceID = r.Primary.ID, r.Primary.Attributes["workspace_id"]
				taskOneID = state.RootModule().Resources["kaneo_task.one"].Primary.ID
				if copyID == r.Primary.Attributes["label_id"] {
					return fmt.Errorf("attachment tracked source instead of copy ID")
				}
				label, err := api.label(copyID)
				if err != nil {
					return err
				}
				if label["taskId"] != r.Primary.Attributes["task_id"] || label["workspaceId"] != workspaceID || label["name"] != name || label["color"] != color {
					return fmt.Errorf("unexpected task-label copy: %v", label)
				}
				// This copy is created through the API, never owned by Terraform.
				if manualID == "" {
					var manual map[string]any
					if err := api.request(http.MethodPost, "/label", map[string]string{"workspaceId": workspaceID, "taskId": taskOneID, "name": "Manual", "color": "#999999"}, &manual); err != nil {
						return err
					}
					manualID, _ = manual["id"].(string)
					if manualID == "" {
						return fmt.Errorf("manual label has no ID")
					}
				}
				manual, err := api.label(manualID)
				if err != nil {
					return err
				}
				if manual["taskId"] != taskOneID || manual["name"] != "Manual" {
					return fmt.Errorf("unmanaged label changed")
				}
				return nil
			})
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acceptanceProviderFactories, CheckDestroy: api.checkDestroy,
		Steps: []resource.TestStep{
			{Config: initial, Check: check("Bug", "#ef4444", false)},
			{ResourceName: labelAddress, ImportState: true, ImportStateVerify: true},
			{ResourceName: attachmentAddress, ImportState: true, ImportStateVerify: true, ImportStateIdFunc: func(state *terraform.State) (string, error) {
				r := state.RootModule().Resources[attachmentAddress]
				return r.Primary.Attributes["label_id"] + "/" + r.Primary.ID, nil
			}},
			{Config: initial + `
resource "kaneo_task_label" "invalid_source" {
 label_id = kaneo_task_label.test.id
 task_id = kaneo_task.two.id
}
`, ExpectError: regexp.MustCompile("is task-specific")},
			{Config: initial + `
resource "kaneo_task_label" "duplicate" {
 label_id = kaneo_label.test.id
 task_id = kaneo_task.one.id
}
`, ExpectError: regexp.MustCompile("Task Label Already Exists")},
			{Config: initial, ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}, Check: check("Bug", "#ef4444", false)},
			{Config: renamed, ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(labelAddress, plancheck.ResourceActionUpdate)}}, Check: check("Issue", "#123ABC", false)},
			{
				PreConfig: func() {
					if err := api.request(http.MethodDelete, "/label/"+url.PathEscape(copyID)+"/task", nil, nil); err != nil {
						t.Fatal(err)
					}
				},
				Config: renamed, ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(attachmentAddress, plancheck.ResourceActionCreate)}}, Check: check("Issue", "#123ABC", true),
			},
			{Config: moved, ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(attachmentAddress, plancheck.ResourceActionDestroyBeforeCreate)}}, Check: check("Issue", "#123ABC", true)},
			{Config: changedSource, ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(attachmentAddress, plancheck.ResourceActionDestroyBeforeCreate)}}, Check: check("Feature", "#abcdef", true)},
			{Config: config("Issue", "#123ABC", "", ""), Check: resource.ComposeAggregateTestCheckFunc(api.checkLabel(labelAddress), api.checkLabel("kaneo_label.other"), func(state *terraform.State) error {
				if err := api.labelAbsent(workspaceID, copyID); err != nil {
					return err
				}
				manual, err := api.label(manualID)
				if err != nil {
					return err
				}
				if manual["taskId"] != taskOneID {
					return fmt.Errorf("detaching changed the unmanaged label")
				}
				for _, address := range []string{"kaneo_task.one", "kaneo_task.two"} {
					if _, err := api.task(state.RootModule().Resources[address].Primary.ID); err != nil {
						return err
					}
				}
				return nil
			})},
			{
				PreConfig: func() {
					var copy map[string]any
					if err := api.request(http.MethodPost, "/label", map[string]string{"workspaceId": workspaceID, "taskId": taskOneID, "name": "Issue", "color": "#123ABC"}, &copy); err != nil {
						t.Fatal(err)
					}
					cascadeCopyID, _ = copy["id"].(string)
					if cascadeCopyID == "" {
						t.Fatal("cascade fixture has no ID")
					}
				},
				Config: base,
				Check: resource.ComposeAggregateTestCheckFunc(api.checkLabel("kaneo_label.other"), func(_ *terraform.State) error {
					if err := api.labelAbsent(workspaceID, cascadeCopyID); err != nil {
						return err
					}
					_, err := api.label(manualID)
					return err
				}),
			},
		},
	})
}

func TestAccLabelDeletedOutsideTerraform(t *testing.T) {
	api := newAcceptanceAPI(t)
	const address = "kaneo_label.test"
	base := api.projectConfig("Terraform Tasks", "TASK") + acceptanceWorkspaceConfig("other", "Other workspace")
	config := func(workspace string) string {
		return base + fmt.Sprintf(`
resource "kaneo_label" "test" {
 workspace_id = kaneo_workspace.%s.id
 name = "Bug"
 color = "#ef4444"
}
`, workspace)
	}
	var id, workspaceID string
	check := func(replaced bool) resource.TestCheckFunc {
		return resource.ComposeAggregateTestCheckFunc(api.checkLabel(address),
			resource.TestCheckResourceAttr(address, "name", "Bug"), resource.TestCheckResourceAttr(address, "color", "#ef4444"),
			func(state *terraform.State) error {
				r := state.RootModule().Resources[address]
				if id != "" && (r.Primary.ID != id) != replaced {
					return fmt.Errorf("unexpected label replacement")
				}
				if replaced {
					if err := api.labelAbsent(workspaceID, id); err != nil {
						return err
					}
				}
				id, workspaceID = r.Primary.ID, r.Primary.Attributes["workspace_id"]
				return nil
			})
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acceptanceProviderFactories, CheckDestroy: api.checkDestroy,
		Steps: []resource.TestStep{
			{Config: config("test"), Check: check(false)},
			{PreConfig: func() {
				if err := api.request(http.MethodPut, "/label/"+url.PathEscape(id), map[string]string{"name": "External change", "color": "#000000"}, nil); err != nil {
					t.Fatal(err)
				}
			},
				Config: config("test"), ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate)}}, Check: check(false)},
			{PreConfig: func() {
				if err := api.request(http.MethodDelete, "/label/"+url.PathEscape(id), nil, nil); err != nil {
					t.Fatal(err)
				}
			},
				Config: config("test"), ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionCreate)}}, Check: check(true)},
			{Config: config("other"), ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionDestroyBeforeCreate)}}, Check: check(true)},
			{Config: base, Check: func(_ *terraform.State) error { return api.labelAbsent(workspaceID, id) }},
		},
	})
}
