// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
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
	key      string
	client   *http.Client
}

func newAcceptanceAPI(t *testing.T) *acceptanceAPI {
	t.Helper()
	if os.Getenv("TF_ACC") == "" {
		t.Skip("set TF_ACC=1 to run acceptance tests, or use make testacc")
	}
	endpoint := strings.TrimRight(os.Getenv("KANEO_TEST_ENDPOINT"), "/")
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "http" || (parsed.Hostname() != "localhost" && parsed.Hostname() != "127.0.0.1") {
		t.Fatal("KANEO_TEST_ENDPOINT must point to a disposable local instance; use make testacc")
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	api := &acceptanceAPI{endpoint: endpoint, client: &http.Client{Jar: jar, Timeout: 30 * time.Second}}
	if err := api.request(http.MethodPost, "/auth/sign-up/email", map[string]string{
		"name": "Terraform Acceptance", "email": "terraform-" + uuid.NewString() + "@example.com",
		"password": uuid.NewString(),
	}, nil); err != nil {
		t.Fatalf("create test user: %s", err)
	}
	var response struct {
		Key string `json:"key"`
	}
	if err := api.request(http.MethodPost, "/auth/api-key/create", map[string]string{"name": "terraform-acceptance"}, &response); err != nil {
		t.Fatalf("create API key: %s", err)
	}
	if response.Key == "" {
		t.Fatal("API key creation returned no key")
	}
	api.key = response.Key
	// Discard the signup session so subsequent checks prove API-key authentication works.
	api.client = &http.Client{Timeout: 30 * time.Second}
	return api
}

func (a *acceptanceAPI) request(method, path string, body, result any) error {
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
	if a.key != "" {
		req.Header.Set("x-api-key", a.key)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
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
	return fmt.Sprintf("provider \"kaneo\" {\n endpoint = %q\n api_key = %q\n}\n", a.endpoint, a.key)
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
