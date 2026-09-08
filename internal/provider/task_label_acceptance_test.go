// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccTaskLabelDeletedParents(t *testing.T) {
	api := newAcceptanceAPI(t)
	const labelAddress, taskAddress, attachmentAddress = "kaneo_label.test", "kaneo_task.test", "kaneo_task_label.test"
	config := api.projectConfig("Terraform Tasks", "TASK") + `
resource "kaneo_task" "test" {
 project_id = kaneo_project.test.id
 title = "Label recovery"
}
resource "kaneo_label" "test" {
 workspace_id = kaneo_workspace.test.id
 name = "Bug"
 color = "#ef4444"
}
resource "kaneo_task_label" "test" {
 label_id = kaneo_label.test.id
 task_id = kaneo_task.test.id
}
`
	var labelID, taskID, copyID, workspaceID string
	check := func(newLabel, newTask bool) resource.TestCheckFunc {
		return resource.ComposeAggregateTestCheckFunc(api.checkLabel(labelAddress), api.checkTask(taskAddress), func(state *terraform.State) error {
			label := state.RootModule().Resources[labelAddress]
			task := state.RootModule().Resources[taskAddress]
			attachment := state.RootModule().Resources[attachmentAddress]
			if labelID != "" {
				if (label.Primary.ID != labelID) != newLabel || (task.Primary.ID != taskID) != newTask || attachment.Primary.ID == copyID {
					return fmt.Errorf("unexpected parent or copy replacement")
				}
				if err := api.labelAbsent(workspaceID, copyID); err != nil {
					return err
				}
				if newLabel {
					if err := api.labelAbsent(workspaceID, labelID); err != nil {
						return err
					}
				}
			}
			labelID, taskID, copyID, workspaceID = label.Primary.ID, task.Primary.ID, attachment.Primary.ID, attachment.Primary.Attributes["workspace_id"]
			copy, err := api.label(copyID)
			if err != nil {
				return err
			}
			if copyID == labelID || copy["taskId"] != taskID || copy["workspaceId"] != workspaceID || attachment.Primary.Attributes["label_id"] != labelID || attachment.Primary.Attributes["task_id"] != taskID {
				return fmt.Errorf("attachment does not match its parents")
			}
			return nil
		})
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acceptanceProviderFactories, CheckDestroy: api.checkDestroy,
		Steps: []resource.TestStep{
			{Config: config, Check: check(false, false)},
			{
				PreConfig: func() {
					if err := api.request(http.MethodDelete, "/label/"+url.PathEscape(labelID), nil, nil); err != nil {
						t.Fatal(err)
					}
				},
				Config: config, ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(labelAddress, plancheck.ResourceActionCreate),
					plancheck.ExpectResourceAction(attachmentAddress, plancheck.ResourceActionCreate),
					plancheck.ExpectResourceAction(taskAddress, plancheck.ResourceActionNoop),
				}}, Check: check(true, false),
			},
			{
				PreConfig: func() {
					if err := api.request(http.MethodDelete, "/task/"+url.PathEscape(taskID), nil, nil); err != nil {
						t.Fatal(err)
					}
				},
				Config: config, ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(taskAddress, plancheck.ResourceActionCreate),
					plancheck.ExpectResourceAction(attachmentAddress, plancheck.ResourceActionCreate),
					plancheck.ExpectResourceAction(labelAddress, plancheck.ResourceActionNoop),
				}}, Check: check(false, true),
			},
			{Config: config, ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}},
		},
	})
}
