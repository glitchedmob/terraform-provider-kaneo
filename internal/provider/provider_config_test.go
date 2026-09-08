// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestResolveProviderConfig(t *testing.T) {
	t.Parallel()

	environment := map[string]string{
		"KANEO_API_URL":  "https://environment.example/api",
		"KANEO_USERNAME": "environment@example.com",
		"KANEO_PASSWORD": " environment-password ",
	}
	tests := map[string]struct {
		config       KaneoProviderModel
		environment  map[string]string
		wantEndpoint string
		wantUsername string
		wantPassword string
	}{
		"defaults": {
			wantEndpoint: defaultEndpoint,
		},
		"environment": {
			environment:  environment,
			wantEndpoint: "https://environment.example/api",
			wantUsername: "environment@example.com",
			wantPassword: " environment-password ",
		},
		"configuration overrides environment": {
			config: KaneoProviderModel{
				Endpoint: types.StringValue(" https://configuration.example/api "),
				Username: types.StringValue(" configuration@example.com "),
				Password: types.StringValue(" configuration-password "),
			},
			environment:  environment,
			wantEndpoint: "https://configuration.example/api",
			wantUsername: "configuration@example.com",
			wantPassword: " configuration-password ",
		},
		"mixed configuration and environment": {
			config: KaneoProviderModel{
				Username: types.StringValue("configuration@example.com"),
			},
			environment:  environment,
			wantEndpoint: "https://environment.example/api",
			wantUsername: "configuration@example.com",
			wantPassword: " environment-password ",
		},
		"empty configuration does not fall back to environment": {
			config: KaneoProviderModel{
				Endpoint: types.StringValue(""),
				Username: types.StringValue(""),
				Password: types.StringValue(""),
			},
			environment: environment,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			endpoint, username, password := resolveProviderConfig(test.config, func(key string) string {
				return test.environment[key]
			})
			if endpoint != test.wantEndpoint || username != test.wantUsername || password != test.wantPassword {
				t.Fatalf("unexpected resolved config: %q, %q, %q", endpoint, username, password)
			}
		})
	}
}

// Shared by the existing API fixtures so resource tests also use a signed-in client.
func handleTestSignIn(t *testing.T, writer http.ResponseWriter, request *http.Request) bool {
	t.Helper()
	if strings.TrimPrefix(request.URL.Path, "/api") != "/auth/sign-in/email" {
		return false
	}
	if request.Method != http.MethodPost || request.Header.Get("Content-Type") != "application/json" {
		t.Error("expected JSON sign-in POST")
	}
	if request.Header.Get("Authorization") != "" || request.Header.Get("x-api-key") != "" {
		t.Error("sign-in must not send an API key or session token")
	}
	var body map[string]string
	if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
		t.Errorf("decode sign-in: %v", err)
	}
	if body["email"] != "test@example.com" || body["password"] != " test-password " {
		t.Error("incorrect sign-in credentials")
	}
	writer.Header().Set("Content-Type", "application/json")
	if _, err := fmt.Fprint(writer, `{"token":"test-session"}`); err != nil {
		t.Error(err)
	}
	return true
}

func TestAPIClientRequestHeaders(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if userAgent := request.Header.Get("User-Agent"); userAgent != "terraform-provider-kaneo/test" {
			t.Errorf("expected provider user agent, got %q", userAgent)
		}
		if handleTestSignIn(t, writer, request) {
			return
		}
		if request.URL.Path != "/api/instance/status" {
			t.Errorf("expected request path %q, got %q", "/api/instance/status", request.URL.Path)
		}
		if authorization := request.Header.Get("Authorization"); authorization != "Bearer test-session" {
			t.Errorf("expected session bearer authorization, got %q", authorization)
		}
		if request.Header.Get("x-api-key") != "" {
			t.Error("API requests must not send an API key")
		}
		writer.Header().Set("Content-Type", "application/json")
		if _, err := fmt.Fprint(writer, `{"hasUsers":true,"hasAdmin":true}`); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()

	client, err := newAPIClient(t.Context(), server.URL+"/api/", "test@example.com", " test-password ", "test")
	if err != nil {
		t.Fatalf("create API client: %v", err)
	}
	for range 2 {
		response, err := client.GetInstanceStatusWithResponse(t.Context())
		if err != nil {
			t.Fatalf("get instance status: %v", err)
		}
		if response.StatusCode() != http.StatusOK {
			t.Fatalf("expected HTTP %d, got %d", http.StatusOK, response.StatusCode())
		}
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
		"https://user:password@kaneo.example/api",
	} {
		t.Run(endpoint, func(t *testing.T) {
			t.Parallel()
			if _, err := newAPIClient(t.Context(), endpoint, "test@example.com", " test-password ", "test"); err == nil {
				t.Fatalf("expected endpoint %q to be rejected", endpoint)
			}
		})
	}
}

func TestAPIClientSignInFailures(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		status int
		body   string
	}{
		"invalid credentials": {http.StatusUnauthorized, `{"message":"secret-password"}`},
		"disabled login":      {http.StatusForbidden, `{"message":"secret-password"}`},
		"redirect":            {http.StatusTemporaryRedirect, `secret-password`},
		"invalid JSON":        {http.StatusOK, `secret-password`},
		"invalid token type":  {http.StatusOK, `{"token":{"secret-password":true}}`},
		"missing token":       {http.StatusOK, `{}`},
		"empty token":         {http.StatusOK, `{"token":" "}`},
		"MFA challenge":       {http.StatusOK, `{"twoFactorRedirect":true}`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/auth/sign-in/email" {
					t.Error("sign-in redirect must not be followed")
				}
				w.Header().Set("Location", "/redirect-target")
				w.WriteHeader(test.status)
				if _, err := fmt.Fprint(w, test.body); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			client, err := newAPIClient(t.Context(), server.URL, "test@example.com", "secret-password", "test")
			if err == nil || client != nil {
				t.Fatal("expected failed sign-in without a configured client")
			}
			if strings.Contains(err.Error(), "secret-password") {
				t.Fatal("sign-in error leaked response contents")
			}
		})
	}

	t.Run("canceled context", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			t.Error("canceled sign-in must not reach the server")
		}))
		defer server.Close()
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if _, err := newAPIClient(ctx, server.URL, "test@example.com", " test-password ", "test"); err == nil {
			t.Fatal("expected canceled sign-in to fail")
		}
	})
}
