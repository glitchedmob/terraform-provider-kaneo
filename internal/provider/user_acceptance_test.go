// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/hashicorp/terraform-plugin-testing/config"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccUserLifecycle(t *testing.T) {
	api := newAcceptanceAPI(t)
	const address = "kaneo_user.test"
	email := "Terraform-" + uuid.NewV4().String() + "@Example.com"
	password, rotated := uuid.NewV4().String(), uuid.NewV4().String()
	var id string
	userConfig := func(name, email, role string, version int, verified bool) string {
		optional := ""
		if version != 0 {
			optional = fmt.Sprintf("password_wo = var.user_password\n password_wo_version = %d", version)
		}
		return api.providerConfig() + `
variable "user_password" {
 type = string
 sensitive = true
 ephemeral = true
 default = null
}
` + fmt.Sprintf(`
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
						if err := response.Body.Close(); err != nil {
							return err
						}
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
		if err := response.Body.Close(); err != nil {
			return err
		}
		if response.StatusCode != 404 {
			return fmt.Errorf("deleted user: want 404, got %d", response.StatusCode)
		}
		return nil
	}
	initial := userConfig("Terraform User", email, "user", 0, false)
	credential := userConfig("Terraform User", email, "user", 1, false)
	updatedEmail := "Updated-" + email
	updated := userConfig("Updated User", updatedEmail, "admin", 2, true)
	demoted := userConfig("Updated User", updatedEmail, "user", 2, true)
	steps := []resource.TestStep{
		{Config: initial, Check: check("Terraform User", email, "user", "", false)},
		{ResourceName: address, ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"email"}},
		{Config: credential, ConfigVariables: config.Variables{"user_password": config.StringVariable(password)}, Check: check("Terraform User", email, "user", password, false)},
		{Config: updated, Check: resource.ComposeAggregateTestCheckFunc(check("Updated User", updatedEmail, "admin", rotated, true), func(_ *terraform.State) error {
			old := userAcceptanceSession(api, updatedEmail, password)
			if err := old.request(http.MethodPost, "/auth/sign-in/email", map[string]string{"email": updatedEmail, "password": password}, nil); err == nil {
				return fmt.Errorf("old password still works")
			}
			return nil
		})},
		{ResourceName: address, ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"email", "password_wo_version"}},
		{Config: updated, ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}},
		// A different ephemeral value with the same version must not cause a diff.
		{Config: updated, ConfigVariables: config.Variables{"user_password": config.StringVariable(password)}, ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}, Check: check("Updated User", updatedEmail, "admin", rotated, true)},
		// Nor may an unrelated update apply that changed ephemeral value.
		{Config: demoted, ConfigVariables: config.Variables{"user_password": config.StringVariable(password)}, Check: check("Updated User", updatedEmail, "user", rotated, true)},
		{PreConfig: func() {
			if err := api.request(http.MethodPost, "/auth/admin/update-user", map[string]any{"userId": id, "data": map[string]any{"name": "Outside", "role": "admin", "emailVerified": false}}, nil); err != nil {
				t.Fatal(err)
			}
		}, Config: demoted, ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate)}}, Check: check("Updated User", updatedEmail, "user", rotated, true)},
		{Config: userConfig("Updated User", updatedEmail, "user", 0, true), Check: resource.ComposeAggregateTestCheckFunc(resource.TestCheckNoResourceAttr(address, "password_wo_version"), check("Updated User", updatedEmail, "user", rotated, true))},
		{PreConfig: func() {
			if err := api.request(http.MethodPost, "/auth/admin/remove-user", map[string]string{"userId": id}, nil); err != nil {
				t.Fatal(err)
			}
			if err := absent(); err != nil {
				t.Fatal(err)
			}
		}, Config: demoted, ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionCreate)}}, Check: check("Updated User", updatedEmail, "user", rotated, true)},
	}
	for i := range steps {
		step := &steps[i]
		if step.ConfigVariables == nil {
			step.ConfigVariables = config.Variables{"user_password": config.StringVariable(rotated)}
		}
		if !step.ImportState {
			absent := userPasswordAbsent{secrets: []string{password, rotated}}
			step.ConfigPlanChecks.PreApply = append(step.ConfigPlanChecks.PreApply, absent)
			step.ConfigPlanChecks.PostApplyPreRefresh = []plancheck.PlanCheck{absent}
			step.ConfigPlanChecks.PostApplyPostRefresh = []plancheck.PlanCheck{absent}
			step.ConfigStateChecks = append(step.ConfigStateChecks, absent)
		}
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acceptanceProviderFactories,
		CheckDestroy:             func(_ *terraform.State) error { return absent() },
		Steps:                    steps,
	})
}

// These checks inspect Terraform's saved plan/state JSON, not just SDK model values.
// The known markers enter through an ephemeral variable, never HCL literals.
type userPasswordAbsent struct {
	secrets      []string
	secretSource func() []string
}

func (c userPasswordAbsent) check(value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	secrets := append([]string(nil), c.secrets...)
	if c.secretSource != nil {
		secrets = append(secrets, c.secretSource()...)
	}
	for _, secret := range secrets {
		encoded, err := json.Marshal(secret)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), string(encoded[1:len(encoded)-1])) {
			return fmt.Errorf("managed password found in Terraform JSON")
		}
	}
	return nil
}
func (c userPasswordAbsent) CheckPlan(_ context.Context, req plancheck.CheckPlanRequest, resp *plancheck.CheckPlanResponse) {
	resp.Error = c.check(req.Plan)
	if resp.Error != nil {
		return
	}
	for _, change := range req.Plan.ResourceChanges {
		if change.Type != "kaneo_user" {
			continue
		}
		for _, value := range []any{change.Change.Before, change.Change.After} {
			if attrs, ok := value.(map[string]any); ok && attrs["password_wo"] != nil {
				resp.Error = fmt.Errorf("write-only password present in plan")
			}
		}
	}
}
func (c userPasswordAbsent) CheckState(_ context.Context, req statecheck.CheckStateRequest, resp *statecheck.CheckStateResponse) {
	resp.Error = c.check(req.State)
	if resp.Error != nil || req.State.Values == nil {
		return
	}
	for _, r := range req.State.Values.RootModule.Resources {
		if r.Type == "kaneo_user" && r.AttributeValues["password_wo"] != nil {
			resp.Error = fmt.Errorf("write-only password present in state")
		}
	}
}

func userAcceptanceSession(admin *acceptanceAPI, email, password string) *acceptanceAPI {
	jar, _ := cookiejar.New(nil)
	return &acceptanceAPI{endpoint: admin.endpoint, username: email, password: password, client: &http.Client{Jar: jar, Timeout: 30 * time.Second}}
}
