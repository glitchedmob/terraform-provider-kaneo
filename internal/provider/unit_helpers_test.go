// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	kaneoclient "github.com/glitchedmob/terraform-provider-kaneo/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
)

func testClient(t *testing.T, handler http.HandlerFunc) *kaneoclient.ClientWithResponses {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if handleTestSignIn(t, w, r) {
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-session" {
			t.Error("missing session token")
		}
		w.Header().Set("Content-Type", "application/json")
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	client, err := newAPIClient(t.Context(), server.URL, "test@example.com", " test-password ", "test")
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func testPlan(t *testing.T, r resource.Resource, model any) tfsdk.Plan {
	t.Helper()
	var response resource.SchemaResponse
	r.Schema(t.Context(), resource.SchemaRequest{}, &response)
	if response.Diagnostics.HasError() {
		t.Fatal(response.Diagnostics)
	}
	plan := tfsdk.Plan{Schema: response.Schema}
	if diags := plan.Set(t.Context(), model); diags.HasError() {
		t.Fatal(diags)
	}
	return plan
}

func testConfig(t *testing.T, d datasource.DataSource, model any) tfsdk.Config {
	t.Helper()
	var response datasource.SchemaResponse
	d.Schema(t.Context(), datasource.SchemaRequest{}, &response)
	if response.Diagnostics.HasError() {
		t.Fatal(response.Diagnostics)
	}
	state := tfsdk.State{Schema: response.Schema}
	if diags := state.Set(t.Context(), model); diags.HasError() {
		t.Fatal(diags)
	}
	return tfsdk.Config{Schema: response.Schema, Raw: state.Raw}
}

func testFixture(t *testing.T, body string, value any) {
	t.Helper()
	if err := json.Unmarshal([]byte(body), value); err != nil {
		t.Fatal(err)
	}
}

// HTTP handlers run outside the test goroutine. Report errors without FailNow,
// and let the caller return before using an incomplete request body.
func testDecodeRequest(t *testing.T, w http.ResponseWriter, r *http.Request, value any) bool {
	t.Helper()
	if err := json.NewDecoder(r.Body).Decode(value); err != nil {
		t.Errorf("decode %s %s request: %v", r.Method, r.URL.Path, err)
		w.WriteHeader(http.StatusBadRequest)
		return false
	}
	return true
}

func testEncodeResponse(t *testing.T, w io.Writer, value any) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Errorf("encode response: %v", err)
	}
}
