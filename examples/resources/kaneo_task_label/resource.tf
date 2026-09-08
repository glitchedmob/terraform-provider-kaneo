# Existing workspace label and task must be in the same workspace.
resource "kaneo_task_label" "bug" {
  label_id = "existing-workspace-label-id"
  task_id  = "existing-task-id"
}
