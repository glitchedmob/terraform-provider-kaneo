data "kaneo_column" "todo" {
  project_id = "existing-project-id"
  slug       = "to-do"
}

resource "kaneo_column" "todo" {
  project_id = data.kaneo_column.todo.project_id
  name       = "To Do"
  is_final   = false
}

import {
  to = kaneo_column.todo
  id = "${data.kaneo_column.todo.project_id}/${data.kaneo_column.todo.id}"
}
