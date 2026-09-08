---
page_title: "Import existing objects - Kaneo"
description: |-
  Adopt existing Kaneo objects without recreating them.
---

# Import existing objects

Each resource page gives its import ID format. Example IDs are placeholders; replace them with real IDs. Configure the target resource to match the existing object before applying, since omitted arguments can clear values or apply defaults.

Use `terraform import ADDRESS ID`, or a declarative import block with Terraform 1.5 or later. For example, adopt a default column in an existing project:

```terraform
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
```

Create the project first so import IDs are known during planning. Set `is_final = true` for the default Done column. See [column defaults and import formats](/providers/glitchedmob/kaneo/latest/docs/resources/column#import-and-default-columns).

Literal IDs do not establish Terraform dependencies. When Terraform manages related objects, reference their attributes instead so creation and deletion happen in dependency order.
