// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"net/http"
	"testing"
	"uuid"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccWorkspaceLifecycle(t *testing.T) {
	api := newAcceptanceAPI(t)
	slug := "terraform-" + uuid.NewV4().String()
	const address = "kaneo_workspace.test"
	config := func(name, slug, optional string) string {
		return api.providerConfig() + fmt.Sprintf(`
resource "kaneo_workspace" "test" {
  name = %q
  slug = %q
  %s
}
data "kaneo_workspace" "by_id" {
  id = kaneo_workspace.test.id
}
data "kaneo_workspace" "by_slug" {
  slug = kaneo_workspace.test.slug
}
`, name, slug, optional)
	}
	checks := func(name, slug string, extra ...resource.TestCheckFunc) resource.TestCheckFunc {
		checks := []resource.TestCheckFunc{
			resource.TestCheckResourceAttr(address, "name", name),
			resource.TestCheckResourceAttr(address, "slug", slug),
			resource.TestCheckResourceAttrSet(address, "created_at"),
			api.checkWorkspace(address),
		}
		for _, lookup := range []string{"by_id", "by_slug"} {
			for _, field := range []string{"id", "name", "slug", "created_at"} {
				checks = append(checks, resource.TestCheckResourceAttrPair("data.kaneo_workspace."+lookup, field, address, field))
			}
		}
		return resource.ComposeAggregateTestCheckFunc(append(checks, extra...)...)
	}
	initial := config("Terraform Acceptance", slug, "")
	updated := config("Terraform Updated", slug+"-updated", `description = "Managed by Terraform"
  logo = "https://example.com/logo.png"`)
	cleared := config("Terraform Updated", slug+"-updated", "")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acceptanceProviderFactories,
		CheckDestroy:             api.checkDestroy,
		Steps: []resource.TestStep{
			{
				Config: initial,
				Check: checks("Terraform Acceptance", slug,
					resource.TestCheckNoResourceAttr(address, "description"),
					resource.TestCheckNoResourceAttr(address, "logo")),
			},
			{
				Config: updated,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
				}},
				Check: checks("Terraform Updated", slug+"-updated",
					resource.TestCheckResourceAttr(address, "description", "Managed by Terraform"),
					resource.TestCheckResourceAttr(address, "logo", "https://example.com/logo.png"),
					resource.TestCheckResourceAttrPair("data.kaneo_workspace.by_id", "description", address, "description"),
					resource.TestCheckResourceAttrPair("data.kaneo_workspace.by_slug", "logo", address, "logo")),
			},
			{
				ResourceName:      address,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: updated,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectEmptyPlan(),
				}},
			},
			{
				Config: cleared,
				Check: checks("Terraform Updated", slug+"-updated",
					resource.TestCheckNoResourceAttr(address, "description"),
					resource.TestCheckNoResourceAttr(address, "logo")),
			},
		},
	})
}

func TestAccWorkspaceDeletedOutsideTerraform(t *testing.T) {
	api := newAcceptanceAPI(t)
	const address = "kaneo_workspace.test"
	config := api.providerConfig() + acceptanceWorkspaceConfig("test", "Terraform Drift")
	var originalID string

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acceptanceProviderFactories,
		CheckDestroy:             api.checkDestroy,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(api.checkWorkspace(address), func(state *terraform.State) error {
					originalID = state.RootModule().Resources[address].Primary.ID
					return nil
				}),
			},
			{
				PreConfig: func() {
					if err := api.request(http.MethodPost, "/auth/organization/delete", map[string]string{"organizationId": originalID}, nil); err != nil {
						t.Fatalf("delete workspace outside Terraform: %s", err)
					}
				},
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(address, plancheck.ResourceActionCreate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(api.checkWorkspace(address), func(state *terraform.State) error {
					if state.RootModule().Resources[address].Primary.ID == originalID {
						return fmt.Errorf("expected a new workspace ID after recreation")
					}
					return nil
				}),
			},
		},
	})
}
