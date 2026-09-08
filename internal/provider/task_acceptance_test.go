// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func (a *acceptanceAPI) task(id string) (map[string]any, error) {
	var task map[string]any
	err := a.request(http.MethodGet, "/task/"+url.PathEscape(id), nil, &task)
	return task, err
}

func (a *acceptanceAPI) checkTask(address string) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		r, ok := state.RootModule().Resources[address]
		if !ok || r.Primary.ID == "" {
			return fmt.Errorf("%s has no task ID", address)
		}
		task, err := a.task(r.Primary.ID)
		if err != nil {
			return err
		}
		if err := checkAcceptanceStrings(address, r, task, map[string]string{
			"project_id": "projectId", "title": "title", "description": "description", "status": "status", "priority": "priority", "assignee_id": "userId",
		}); err != nil {
			return err
		}
		for attribute, field := range map[string]string{"start_date": "startDate", "due_date": "dueDate", "created_at": "createdAt"} {
			remote, _ := task[field].(string)
			value := r.Primary.Attributes[attribute]
			if remote == "" && value == "" {
				continue
			}
			actual, err := time.Parse(time.RFC3339Nano, remote)
			if err != nil {
				return err
			}
			expected, err := time.Parse(time.RFC3339Nano, value)
			if err != nil {
				return err
			}
			if !actual.Equal(expected) {
				return fmt.Errorf("task %s: API %s=%q, state=%q", r.Primary.ID, field, remote, value)
			}
		}
		for _, field := range []string{"number", "position"} {
			value, ok := task[field].(float64)
			if !ok || strconv.FormatFloat(value, 'f', -1, 64) != r.Primary.Attributes[field] {
				return fmt.Errorf("task %s: %s differs from API", r.Primary.ID, field)
			}
		}
		return nil
	}
}

func (a *acceptanceAPI) updateTask(id string, changes map[string]any) error {
	task, err := a.task(id)
	if err != nil {
		return err
	}
	body := map[string]any{}
	for _, field := range []string{"projectId", "title", "description", "status", "priority", "position", "userId", "startDate", "dueDate"} {
		if task[field] != nil {
			body[field] = task[field]
		}
	}
	for field, value := range changes {
		if value == nil {
			delete(body, field)
		} else {
			body[field] = value
		}
	}
	return a.request(http.MethodPut, "/task/"+url.PathEscape(id), body, nil)
}

func (a *acceptanceAPI) taskAbsent(projectID, id string) error {
	type taskID struct {
		ID string `json:"id"`
	}
	var board struct {
		Data struct {
			ID      string `json:"id"`
			Columns []struct {
				Tasks []taskID `json:"tasks"`
			} `json:"columns"`
			Archived []taskID `json:"archivedTasks"`
			Planned  []taskID `json:"plannedTasks"`
		} `json:"data"`
	}
	if err := a.request(http.MethodGet, "/task/tasks/"+url.PathEscape(projectID), nil, &board); err != nil {
		return err
	}
	if board.Data.ID != projectID {
		return fmt.Errorf("unexpected project board")
	}
	tasks := append(board.Data.Archived, board.Data.Planned...)
	for _, column := range board.Data.Columns {
		tasks = append(tasks, column.Tasks...)
	}
	for _, task := range tasks {
		if task.ID == id {
			return fmt.Errorf("task %s still exists", id)
		}
	}
	return nil
}

func TestAccTaskLifecycle(t *testing.T) {
	api := newAcceptanceAPI(t)
	const address = "kaneo_task.test"
	base := api.projectConfig("Terraform Tasks", "TASK") + `
resource "kaneo_column" "testing" {
 project_id = kaneo_project.test.id
 name = "Testing"
}
`
	config := func(title, status, optional string) string {
		return base + fmt.Sprintf(`
resource "kaneo_task" "test" {
 project_id = kaneo_project.test.id
 title = %q
 status = %s
 %s
}
data "kaneo_task" "by_id" {
 id = kaneo_task.test.id
}
`, title, status, optional)
	}
	initial := config("Test deployment", "kaneo_column.testing.slug", fmt.Sprintf(`
 description = "Initial description"
 priority = "high"
 assignee_id = %q
 start_date = "2027-09-01T08:00:00.120-04:00"
 due_date = "2027-09-02T12:00:00.001Z"
`, api.userID))
	updated := config("Verify deployment", `"planned"`, fmt.Sprintf(`
 description = "Updated description"
 priority = "urgent"
 assignee_id = %q
 start_date = "2027-09-01T08:00:00.121-04:00"
 due_date = "2027-09-02T12:00:00.002Z"
`, api.userID))
	cleared := config("Verify deployment", `"archived"`, "")
	var taskID, projectID string
	check := func(title, status, priority, position string, extra ...resource.TestCheckFunc) resource.TestCheckFunc {
		checks := []resource.TestCheckFunc{
			api.checkTask(address), api.checkTask("data.kaneo_task.by_id"),
			resource.TestCheckResourceAttr(address, "title", title),
			resource.TestCheckResourceAttr(address, "status", status),
			resource.TestCheckResourceAttr(address, "priority", priority),
			resource.TestCheckResourceAttr(address, "position", position),
			resource.TestCheckResourceAttr(address, "number", "1"),
			resource.TestCheckResourceAttrSet(address, "created_at"),
			func(state *terraform.State) error {
				r := state.RootModule().Resources[address]
				if taskID != "" && taskID != r.Primary.ID {
					return fmt.Errorf("update replaced the task")
				}
				taskID, projectID = r.Primary.ID, r.Primary.Attributes["project_id"]
				return nil
			},
		}
		for _, field := range []string{"id", "project_id", "title", "description", "status", "priority", "number", "position", "created_at"} {
			checks = append(checks, resource.TestCheckResourceAttrPair("data.kaneo_task.by_id", field, address, field))
		}
		return resource.ComposeAggregateTestCheckFunc(append(checks, extra...)...)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acceptanceProviderFactories,
		CheckDestroy:             api.checkDestroy,
		Steps: []resource.TestStep{
			{Config: initial, Check: check("Test deployment", "testing", "high", "1",
				resource.TestCheckResourceAttr(address, "description", "Initial description"),
				resource.TestCheckResourceAttr(address, "assignee_id", api.userID),
				resource.TestCheckResourceAttr(address, "start_date", "2027-09-01T08:00:00.120-04:00"),
				resource.TestCheckResourceAttr(address, "due_date", "2027-09-02T12:00:00.001Z"))},
			{
				Config:           updated,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate)}},
				Check: check("Verify deployment", "planned", "urgent", "1",
					resource.TestCheckResourceAttr(address, "description", "Updated description"),
					resource.TestCheckResourceAttr(address, "start_date", "2027-09-01T08:00:00.121-04:00"),
					resource.TestCheckResourceAttr(address, "due_date", "2027-09-02T12:00:00.002Z")),
			},
			{Config: updated, ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}},
			{
				PreConfig: func() {
					if err := api.updateTask(taskID, map[string]any{"dueDate": "2027-09-02T12:00:00.003Z", "position": 16777217}); err != nil {
						t.Fatal(err)
					}
				},
				Config:           updated,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate)}},
				Check:            check("Verify deployment", "planned", "urgent", "16777217", resource.TestCheckResourceAttr(address, "due_date", "2027-09-02T12:00:00.002Z")),
			},
			{Config: cleared, Check: check("Verify deployment", "archived", "no-priority", "16777217",
				resource.TestCheckResourceAttr(address, "description", ""),
				resource.TestCheckNoResourceAttr(address, "assignee_id"),
				resource.TestCheckNoResourceAttr(address, "start_date"),
				resource.TestCheckNoResourceAttr(address, "due_date"))},
			{ResourceName: address, ImportState: true, ImportStateVerify: true},
			{Config: cleared, ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}},
			{Config: base, Check: func(_ *terraform.State) error { return api.taskAbsent(projectID, taskID) }},
		},
	})
}

func TestAccTaskDeletedOutsideTerraform(t *testing.T) {
	api := newAcceptanceAPI(t)
	const address = "kaneo_task.test"
	base := api.projectConfig("Terraform Tasks", "TASK") + `
resource "kaneo_project" "other" {
 workspace_id = kaneo_workspace.test.id
 name = "Other Project"
 slug = "OTHER"
}
`
	config := func(project string) string {
		return base + fmt.Sprintf(`
resource "kaneo_task" "test" {
 project_id = kaneo_project.%s.id
 title = "Test defaults"
}
`, project)
	}
	var originalID, recreatedID, projectID string
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acceptanceProviderFactories,
		CheckDestroy:             api.checkDestroy,
		Steps: []resource.TestStep{
			{Config: config("test"), Check: resource.ComposeAggregateTestCheckFunc(api.checkTask(address),
				resource.TestCheckResourceAttr(address, "status", "to-do"),
				resource.TestCheckResourceAttr(address, "priority", "no-priority"),
				resource.TestCheckResourceAttr(address, "description", ""),
				resource.TestCheckResourceAttr(address, "number", "1"),
				resource.TestCheckResourceAttr(address, "position", "1"),
				resource.TestCheckNoResourceAttr(address, "assignee_id"),
				resource.TestCheckNoResourceAttr(address, "start_date"),
				resource.TestCheckNoResourceAttr(address, "due_date"),
				func(state *terraform.State) error {
					r := state.RootModule().Resources[address]
					originalID, projectID = r.Primary.ID, r.Primary.Attributes["project_id"]
					return nil
				})},
			{
				PreConfig: func() {
					// Absence must still be confirmed when the board contains virtual-status tasks.
					for _, status := range []string{"planned", "archived"} {
						if err := api.request(http.MethodPost, "/task/"+url.PathEscape(projectID), map[string]string{
							"title": "Other task", "description": "", "status": status, "priority": "no-priority",
						}, nil); err != nil {
							t.Fatal(err)
						}
					}
					if err := api.request(http.MethodDelete, "/task/"+url.PathEscape(originalID), nil, nil); err != nil {
						t.Fatal(err)
					}
				},
				Config:           config("test"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionCreate)}},
				Check: resource.ComposeAggregateTestCheckFunc(api.checkTask(address), resource.TestCheckResourceAttr(address, "number", "4"),
					func(state *terraform.State) error {
						recreatedID = state.RootModule().Resources[address].Primary.ID
						if recreatedID == originalID {
							return fmt.Errorf("task was not recreated")
						}
						return nil
					}),
			},
			{
				Config:           config("other"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionDestroyBeforeCreate)}},
				Check: resource.ComposeAggregateTestCheckFunc(api.checkTask(address), resource.TestCheckResourceAttr(address, "number", "1"),
					func(state *terraform.State) error {
						if state.RootModule().Resources[address].Primary.ID == recreatedID {
							return fmt.Errorf("project change did not replace task")
						}
						return api.taskAbsent(projectID, recreatedID)
					}),
			},
		},
	})
}
