// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"testing"
	"uuid"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccUserEphemeralRandomPassword(t *testing.T) {
	api := newAcceptanceAPI(t)
	email := "random-" + uuid.NewV4().String() + "@example.com"
	// Observe the real API request in memory so we can independently test login
	// and search Terraform JSON for the generated secret. Terraform uses the real
	// Random provider's ephemeral resource, not a test provider or managed secret.
	var mu sync.Mutex
	var passwords []string
	secrets := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), passwords...)
	}
	target, err := url.Parse(api.endpoint)
	if err != nil {
		t.Fatal(err)
	}
	target.Path = ""
	proxy := httputil.NewSingleHostReverseProxy(target)
	observer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if strings.HasSuffix(req.URL.Path, "/auth/admin/set-user-password") {
			body, err := io.ReadAll(req.Body)
			req.Body.Close()
			if err != nil {
				http.Error(w, "read request failed", http.StatusBadRequest)
				return
			}
			var payload struct {
				NewPassword string `json:"newPassword"`
			}
			if err := json.Unmarshal(body, &payload); err != nil {
				http.Error(w, "invalid request", http.StatusBadRequest)
				return
			}
			mu.Lock()
			passwords = append(passwords, payload.NewPassword)
			mu.Unlock()
			req.Body = io.NopCloser(bytes.NewReader(body))
		}
		proxy.ServeHTTP(w, req)
	}))
	defer observer.Close()
	config := func(name string, version int) string {
		return strings.Replace(api.providerConfig(), api.endpoint, observer.URL+"/api", 1) + fmt.Sprintf(`
terraform {
 required_version = ">= 1.11.0"
 required_providers {
  random = { source = "hashicorp/random", version = "3.7.2" }
 }
}
ephemeral "random_password" "user" {
 length = 32
}
resource "kaneo_user" "test" {
 name = %q
 email = %q
 password_wo = ephemeral.random_password.user.result
 password_wo_version = %d
}
`, name, email, version)
	}
	check := func(count int) resource.TestCheckFunc {
		return func(_ *terraform.State) error {
			values := secrets()
			if len(values) != count {
				return fmt.Errorf("password API calls: want %d, got %d", count, len(values))
			}
			password := values[len(values)-1]
			if len(password) != 32 {
				return fmt.Errorf("unexpected generated password length")
			}
			session := userAcceptanceSession(api, email, password)
			if err := session.request(http.MethodPost, "/auth/sign-in/email", map[string]string{"email": email, "password": password}, nil); err != nil {
				return err
			}
			if count > 1 {
				old := userAcceptanceSession(api, email, values[0])
				if err := old.request(http.MethodPost, "/auth/sign-in/email", map[string]string{"email": email, "password": values[0]}, nil); err == nil {
					return fmt.Errorf("old generated password still works")
				}
			}
			return nil
		}
	}
	absent := userPasswordAbsent{secretSource: secrets}
	steps := []resource.TestStep{
		{Config: config("Random User", 1), Check: check(1)},
		{Config: config("Random User", 1), ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}, Check: check(1)},
		{Config: config("Renamed Random User", 1), Check: check(1)},
		{Config: config("Renamed Random User", 2), Check: check(2)},
	}
	for i := range steps {
		steps[i].ConfigPlanChecks.PreApply = append(steps[i].ConfigPlanChecks.PreApply, absent)
		steps[i].ConfigPlanChecks.PostApplyPreRefresh = []plancheck.PlanCheck{absent}
		steps[i].ConfigPlanChecks.PostApplyPostRefresh = []plancheck.PlanCheck{absent}
		steps[i].ConfigStateChecks = []statecheck.StateCheck{absent}
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: acceptanceProviderFactories,
		ExternalProviders:        map[string]resource.ExternalProvider{"random": {Source: "hashicorp/random", VersionConstraint: "3.7.2"}},
		Steps:                    steps,
	})
}
