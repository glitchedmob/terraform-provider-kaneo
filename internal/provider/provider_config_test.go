// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestResolveProviderConfig(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		config       KaneoProviderModel
		environment  map[string]string
		wantEndpoint string
		wantAPIKey   string
	}{
		"defaults": {
			config: KaneoProviderModel{
				Endpoint: types.StringNull(),
				APIKey:   types.StringNull(),
			},
			wantEndpoint: defaultEndpoint,
		},
		"environment": {
			config: KaneoProviderModel{
				Endpoint: types.StringNull(),
				APIKey:   types.StringNull(),
			},
			environment: map[string]string{
				"KANEO_API_URL": "https://environment.example/api",
				"KANEO_API_KEY": "environment-key",
			},
			wantEndpoint: "https://environment.example/api",
			wantAPIKey:   "environment-key",
		},
		"configuration overrides environment": {
			config: KaneoProviderModel{
				Endpoint: types.StringValue("https://configuration.example/api"),
				APIKey:   types.StringValue("configuration-key"),
			},
			environment: map[string]string{
				"KANEO_API_URL": "https://environment.example/api",
				"KANEO_API_KEY": "environment-key",
			},
			wantEndpoint: "https://configuration.example/api",
			wantAPIKey:   "configuration-key",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			getenv := func(key string) string {
				return test.environment[key]
			}
			endpoint, apiKey := resolveProviderConfig(test.config, getenv)
			if endpoint != test.wantEndpoint {
				t.Fatalf("expected endpoint %q, got %q", test.wantEndpoint, endpoint)
			}
			if apiKey != test.wantAPIKey {
				t.Fatalf("expected API key %q, got %q", test.wantAPIKey, apiKey)
			}
		})
	}
}

func TestAPIClientRequestHeaders(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/instance/status" {
			t.Errorf("expected request path %q, got %q", "/api/instance/status", request.URL.Path)
		}
		if authorization := request.Header.Get("Authorization"); authorization != "Bearer test-key" {
			t.Errorf("expected bearer authorization, got %q", authorization)
		}
		if userAgent := request.Header.Get("User-Agent"); userAgent != "terraform-provider-kaneo/test" {
			t.Errorf("expected provider user agent, got %q", userAgent)
		}
		writer.Header().Set("Content-Type", "application/json")
		fmt.Fprint(writer, `{"hasUsers":true,"hasAdmin":true}`)
	}))
	defer server.Close()

	client, err := newAPIClient(server.URL+"/api", "test-key", "test")
	if err != nil {
		t.Fatalf("create API client: %v", err)
	}
	response, err := client.GetInstanceStatusWithResponse(context.Background())
	if err != nil {
		t.Fatalf("get instance status: %v", err)
	}
	if response.StatusCode() != http.StatusOK {
		t.Fatalf("expected HTTP %d, got %d", http.StatusOK, response.StatusCode())
	}
}

func TestAPIClientRejectsInvalidEndpoints(t *testing.T) {
	t.Parallel()

	for _, endpoint := range []string{
		"",
		"kaneo.example/api",
		"ftp://kaneo.example/api",
		"https://kaneo.example/api?query=value",
		"https://kaneo.example/api#fragment",
	} {
		t.Run(endpoint, func(t *testing.T) {
			t.Parallel()
			if _, err := newAPIClient(endpoint, "", "test"); err == nil {
				t.Fatalf("expected endpoint %q to be rejected", endpoint)
			}
		})
	}
}
