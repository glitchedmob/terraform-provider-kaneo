// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccUserLifecycle(t *testing.T) {
	api := newAcceptanceAPI(t)
	const address = "kaneo_user.test"
	email := "Terraform-" + uuid.NewV4().String() + "@Example.com"
	password, rotated := uuid.NewV4().String(), uuid.NewV4().String()
	var id string
	config := func(name, email, role, password string, verified bool) string {
		optional := ""
		if password != "" {
			optional = fmt.Sprintf("password = %q", password)
		}
		return api.providerConfig() + fmt.Sprintf(`
resource "kaneo_user" "test" {
 name = %q
 email = %q
 role = %q
 email_verified = %t
 %s
}
`, name, email, role, verified, optional)
	}
	check := func(name, email, role, password string, verified bool) resource.TestCheckFunc {
		return resource.ComposeAggregateTestCheckFunc(
			resource.TestCheckResourceAttr(address, "name", name),
			resource.TestCheckResourceAttr(address, "email", email),
			resource.TestCheckResourceAttr(address, "role", role),
			resource.TestCheckResourceAttr(address, "email_verified", fmt.Sprint(verified)),
			func(state *terraform.State) error {
				id = state.RootModule().Resources[address].Primary.ID
				var user struct {
					ID, Email, Name, Role string
					EmailVerified         bool
				}
				if err := api.request(http.MethodGet, "/auth/admin/get-user?id="+url.QueryEscape(id), nil, &user); err != nil {
					return err
				}
				if user.ID != id || user.Email != strings.ToLower(email) || user.Name != name || user.Role != role || user.EmailVerified != verified {
					return fmt.Errorf("API user differs from state")
				}
				if password != "" {
					target := userAcceptanceSession(api, email, password)
					if err := target.request(http.MethodPost, "/auth/sign-in/email", map[string]string{"email": email, "password": password}, nil); err != nil {
						return fmt.Errorf("managed password login: %w", err)
					}
					var organizations []map[string]any
					if err := target.request(http.MethodGet, "/auth/organization/list", nil, &organizations); err != nil {
						return err
					}
					if len(organizations) != 0 {
						return fmt.Errorf("user unexpectedly belongs to %d workspaces", len(organizations))
					}
					if role == "user" {
						// Permission errors must not masquerade as a missing user, even for a nonexistent ID.
						req, err := http.NewRequest(http.MethodGet, api.endpoint+"/auth/admin/get-user?id=missing-user", nil)
						if err != nil {
							return err
						}
						response, err := target.client.Do(req)
						if err != nil {
							return err
						}
						response.Body.Close()
						if response.StatusCode != 403 {
							return fmt.Errorf("non-admin get-user: want 403, got %d", response.StatusCode)
						}
					}
				}
				return nil
			})
	}
	absent := func() error {
		req, err := http.NewRequest(http.MethodGet, api.endpoint+"/auth/admin/get-user?id="+url.QueryEscape(id), nil)
		if err != nil {
			return err
		}
		response, err := api.client.Do(req)
		if err != nil {
			return err
		}
		response.Body.Close()
		if response.StatusCode != 404 {
			return fmt.Errorf("deleted user: want 404, got %d", response.StatusCode)
		}
		return nil
	}
	initial := config("Terraform User", email, "user", "", false)
	credential := config("Terraform User", email, "user", password, false)
	updatedEmail := "Updated-" + email
	updated := config("Updated User", updatedEmail, "admin", rotated, true)
	demoted := config("Updated User", updatedEmail, "user", rotated, true)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acceptanceProviderFactories,
		CheckDestroy:             func(_ *terraform.State) error { return absent() },
		Steps: []resource.TestStep{
			{Config: initial, Check: check("Terraform User", email, "user", "", false)},
			{ResourceName: address, ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"email"}},
			{Config: credential, Check: check("Terraform User", email, "user", password, false)},
			{Config: updated, Check: resource.ComposeAggregateTestCheckFunc(check("Updated User", updatedEmail, "admin", rotated, true), func(_ *terraform.State) error {
				old := userAcceptanceSession(api, updatedEmail, password)
				if err := old.request(http.MethodPost, "/auth/sign-in/email", map[string]string{"email": updatedEmail, "password": password}, nil); err == nil {
					return fmt.Errorf("old password still works")
				}
				return nil
			})},
			{ResourceName: address, ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"email", "password"}},
			{Config: updated, ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}},
			{Config: demoted, Check: check("Updated User", updatedEmail, "user", rotated, true)},
			{PreConfig: func() {
				if err := api.request(http.MethodPost, "/auth/admin/update-user", map[string]any{"userId": id, "data": map[string]any{"name": "Outside", "role": "admin", "emailVerified": false}}, nil); err != nil {
					t.Fatal(err)
				}
			}, Config: demoted, ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate)}}, Check: check("Updated User", updatedEmail, "user", rotated, true)},
			{Config: config("Updated User", updatedEmail, "user", "", true), Check: resource.ComposeAggregateTestCheckFunc(resource.TestCheckNoResourceAttr(address, "password"), check("Updated User", updatedEmail, "user", rotated, true))},
			{PreConfig: func() {
				if err := api.request(http.MethodPost, "/auth/admin/remove-user", map[string]string{"userId": id}, nil); err != nil {
					t.Fatal(err)
				}
				if err := absent(); err != nil {
					t.Fatal(err)
				}
			}, Config: demoted, ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionCreate)}}, Check: check("Updated User", updatedEmail, "user", rotated, true)},
		},
	})
}

func userAcceptanceSession(admin *acceptanceAPI, email, password string) *acceptanceAPI {
	jar, _ := cookiejar.New(nil)
	return &acceptanceAPI{endpoint: admin.endpoint, username: email, password: password, client: &http.Client{Jar: jar, Timeout: 30 * time.Second}}
}
