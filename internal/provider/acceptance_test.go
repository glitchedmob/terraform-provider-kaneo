// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

var acceptanceProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"kaneo": providerserver.NewProtocol6WithError(New("acceptance")()),
}

type acceptanceAPI struct {
	endpoint string
	username string
	password string
	userID   string
	client   *http.Client
}

func newAcceptanceAPI(t *testing.T) *acceptanceAPI {
	t.Helper()
	if os.Getenv("TF_ACC") == "" {
		t.Skip("set TF_ACC=1 to run acceptance tests, or use make testacc")
	}
	endpoint := startAcceptanceStack(t)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	api := &acceptanceAPI{
		endpoint: endpoint,
		username: "terraform-" + uuid.NewV4().String() + "@example.com",
		password: uuid.NewV4().String(),
		client:   &http.Client{Jar: jar, Timeout: 30 * time.Second},
	}
	var signup struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	if err := api.request(http.MethodPost, "/auth/sign-up/email", map[string]string{
		"name": "Terraform Acceptance", "email": api.username, "password": api.password,
	}, &signup); err != nil {
		t.Fatalf("create test user: %s", err)
	}
	api.userID = signup.User.ID
	if api.userID == "" {
		t.Fatal("signup returned no user ID")
	}
	return api
}

func (a *acceptanceAPI) request(method, path string, body, result any) (err error) {
	var data []byte
	if body != nil {
		var err error
		data, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequest(method, a.endpoint+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", strings.TrimSuffix(a.endpoint, "/api"))
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := resp.Body.Close(); err == nil {
			err = closeErr
		}
	}()
	if resp.StatusCode != http.StatusOK {
		// Do not print response bodies: authentication responses can contain secrets.
		return fmt.Errorf("%s %s returned HTTP %d", method, path, resp.StatusCode)
	}
	if result == nil {
		_, err = io.Copy(io.Discard, resp.Body)
		return err
	}
	return json.NewDecoder(resp.Body).Decode(result)
}

func (a *acceptanceAPI) providerConfig() string {
	return fmt.Sprintf("provider \"kaneo\" {\n endpoint = %q\n username = %q\n password = %q\n}\n", a.endpoint, a.username, a.password)
}

// Use HTTP directly, independently of the provider's generated client and state mapping.
func (a *acceptanceAPI) workspace(id string) (map[string]any, error) {
	var workspaces []map[string]any
	if err := a.request(http.MethodGet, "/auth/organization/list", nil, &workspaces); err != nil {
		return nil, err
	}
	for _, workspace := range workspaces {
		if workspace["id"] == id {
			return workspace, nil
		}
	}
	return nil, nil
}

func (a *acceptanceAPI) checkWorkspace(address string) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		r, ok := state.RootModule().Resources[address]
		if !ok || r.Primary.ID == "" {
			return fmt.Errorf("%s has no workspace ID", address)
		}
		workspace, err := a.workspace(r.Primary.ID)
		if err != nil {
			return err
		}
		if workspace == nil {
			return fmt.Errorf("workspace %s is absent from Kaneo", r.Primary.ID)
		}
		for _, field := range []string{"name", "slug", "description", "logo"} {
			value, _ := workspace[field].(string)
			if value != r.Primary.Attributes[field] {
				return fmt.Errorf("workspace %s: API %s=%q, state=%q", r.Primary.ID, field, value, r.Primary.Attributes[field])
			}
		}
		return nil
	}
}

func (a *acceptanceAPI) checkDestroy(state *terraform.State) error {
	for address, r := range state.RootModule().Resources {
		if r.Type != "kaneo_workspace" || strings.HasPrefix(address, "data.") {
			continue
		}
		workspace, err := a.workspace(r.Primary.ID)
		if err != nil {
			return err
		}
		if workspace != nil {
			return fmt.Errorf("workspace %s still exists after destroy", r.Primary.ID)
		}
	}
	return nil
}
