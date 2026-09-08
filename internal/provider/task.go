// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"time"

	kaneoclient "github.com/glitchedmob/terraform-provider-kaneo/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/oapi-codegen/nullable"
)

type taskModel struct {
	ID          types.String `tfsdk:"id"`
	ProjectID   types.String `tfsdk:"project_id"`
	Title       types.String `tfsdk:"title"`
	Description types.String `tfsdk:"description"`
	Status      types.String `tfsdk:"status"`
	Priority    types.String `tfsdk:"priority"`
	AssigneeID  types.String `tfsdk:"assignee_id"`
	StartDate   types.String `tfsdk:"start_date"`
	DueDate     types.String `tfsdk:"due_date"`
	Number      types.Int64  `tfsdk:"number"`
	Position    types.Int64  `tfsdk:"position"`
	CreatedAt   types.String `tfsdk:"created_at"`
}

func getTask(ctx context.Context, client *kaneoclient.ClientWithResponses, id, projectID string) (*kaneoclient.Task, error) {
	response, err := client.GetTaskWithResponse(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get task: %w", err)
	}
	if (response.StatusCode() == 400 || response.StatusCode() == 404) && projectID != "" {
		absent, err := taskAbsent(ctx, client, id, projectID)
		if err != nil {
			return nil, err
		}
		if absent {
			return nil, nil
		}
	}
	if response.StatusCode() != 200 {
		return nil, apiResponseError("get task", response.StatusCode(), response.Body)
	}
	task := response.JSON200
	if task == nil || task.Id != id || task.ProjectId == "" {
		return nil, fmt.Errorf("get task: response did not contain the requested task and its project ID")
	}
	// The detail endpoint adds assignee display fields to the ordinary task.
	return &kaneoclient.Task{
		Id: task.Id, ProjectId: task.ProjectId, Title: task.Title, Description: task.Description,
		Status: task.Status, Priority: task.Priority, UserId: task.UserId,
		StartDate: task.StartDate, DueDate: task.DueDate, Number: task.Number,
		Position: task.Position, CreatedAt: task.CreatedAt,
	}, nil
}

// Missing tasks fail in workspace middleware with HTTP 400. As with projects,
// only a successful, complete list can distinguish deletion from lookup errors.
func taskAbsent(ctx context.Context, client *kaneoclient.ClientWithResponses, id, projectID string) (bool, error) {
	// Omitting all query parameters disables pagination and includes every status.
	response, err := client.ListTasksWithResponse(ctx, projectID, nil)
	if err != nil {
		return false, fmt.Errorf("list tasks: %w", err)
	}
	if response.StatusCode() != 200 {
		return false, apiResponseError("list tasks", response.StatusCode(), response.Body)
	}
	board := response.JSON200
	if board == nil || board.Data.Id != projectID || board.Data.Columns == nil || board.Data.ArchivedTasks == nil || board.Data.PlannedTasks == nil {
		return false, fmt.Errorf("list tasks: response did not contain the complete project board")
	}
	groups := [][]kaneoclient.BoardTask{board.Data.ArchivedTasks, board.Data.PlannedTasks}
	for _, column := range board.Data.Columns {
		if column.Tasks == nil {
			return false, fmt.Errorf("list tasks: response contained an incomplete column")
		}
		groups = append(groups, column.Tasks)
	}
	found := false
	seen := make(map[string]bool)
	for _, tasks := range groups {
		for _, task := range tasks {
			if task.Id == "" || task.ProjectId != projectID || seen[task.Id] {
				return false, fmt.Errorf("list tasks: response contained an invalid or duplicate task")
			}
			seen[task.Id] = true
			found = found || task.Id == id
		}
	}
	if board.Pagination.Page != 1 || board.Pagination.TotalPages != 1 || float64(board.Pagination.Total) != float64(len(seen)) {
		return false, fmt.Errorf("list tasks: response was paginated or incomplete")
	}
	return !found, nil
}

func taskModelFromAPI(task kaneoclient.Task, prior taskModel) taskModel {
	assignee := types.StringNull()
	if task.UserId.IsSpecified() && !task.UserId.IsNull() {
		assignee = types.StringValue(task.UserId.GetOrEmpty())
	}
	number, position := types.Int64Null(), types.Int64Null()
	if task.Number.IsSpecified() && !task.Number.IsNull() {
		number = types.Int64Value(int64(task.Number.GetOrEmpty()))
	}
	if task.Position.IsSpecified() && !task.Position.IsNull() {
		position = types.Int64Value(int64(task.Position.GetOrEmpty()))
	}
	return taskModel{
		ID: types.StringValue(task.Id), ProjectID: types.StringValue(task.ProjectId),
		Title: types.StringValue(task.Title), Description: types.StringValue(task.Description.GetOrEmpty()),
		Status: types.StringValue(task.Status), Priority: types.StringValue(task.Priority),
		AssigneeID: assignee, StartDate: taskDateFromAPI(task.StartDate, prior.StartDate), DueDate: taskDateFromAPI(task.DueDate, prior.DueDate),
		Number: number, Position: position, CreatedAt: types.StringValue(task.CreatedAt.Format(time.RFC3339Nano)),
	}
}

func taskDateFromAPI(value nullable.Nullable[time.Time], prior types.String) types.String {
	if !value.IsSpecified() || value.IsNull() {
		return types.StringNull()
	}
	date := value.GetOrEmpty()
	// Kaneo serializes dates in UTC with millisecond precision. Keep the user's
	// spelling only when it represents exactly the same instant, including fractions.
	if previous, err := time.Parse(time.RFC3339Nano, prior.ValueString()); err == nil && previous.Equal(date) {
		return prior
	}
	return types.StringValue(date.Format(time.RFC3339Nano))
}
