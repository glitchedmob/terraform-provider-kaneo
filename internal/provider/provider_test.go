// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	kaneoclient "github.com/glitchedmob/terraform-provider-kaneo/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	providerschema "github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestProviderMetadata(t *testing.T) {
	t.Parallel()

	p := New("test")()
	request := provider.MetadataRequest{}
	response := &provider.MetadataResponse{}

	p.Metadata(context.Background(), request, response)

	if response.TypeName != "kaneo" {
		t.Fatalf("expected provider type name %q, got %q", "kaneo", response.TypeName)
	}
	if response.Version != "test" {
		t.Fatalf("expected provider version %q, got %q", "test", response.Version)
	}
}

func TestProviderConfigure(t *testing.T) {
	t.Parallel()

	p := New("test")()
	schemaResponse := &provider.SchemaResponse{}
	p.Schema(context.Background(), provider.SchemaRequest{}, schemaResponse)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !handleTestSignIn(t, w, r) {
			t.Errorf("unexpected configure request: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	for name, test := range map[string]struct {
		attribute string
		value     any
		wantError string
	}{
		"password sign-in": {},
		"unknown endpoint": {"endpoint", tftypes.UnknownValue, "Unknown Kaneo API Endpoint"},
		"unknown username": {"username", tftypes.UnknownValue, "Unknown Kaneo Username"},
		"unknown password": {"password", tftypes.UnknownValue, "Unknown Kaneo Password"},
		"missing username": {"username", "", "set username or KANEO_USERNAME"},
		"blank username":   {"username", " ", "set username or KANEO_USERNAME"},
		"missing password": {"password", "", "set password or KANEO_PASSWORD"},
	} {
		t.Run(name, func(t *testing.T) {
			values := map[string]tftypes.Value{
				"endpoint": tftypes.NewValue(tftypes.String, server.URL+"/api"),
				"username": tftypes.NewValue(tftypes.String, "test@example.com"),
				"password": tftypes.NewValue(tftypes.String, " test-password "),
			}
			if test.attribute != "" {
				values[test.attribute] = tftypes.NewValue(tftypes.String, test.value)
			}
			request := provider.ConfigureRequest{
				Config: tfsdk.Config{
					Raw: tftypes.NewValue(tftypes.Object{AttributeTypes: map[string]tftypes.Type{
						"endpoint": tftypes.String, "username": tftypes.String, "password": tftypes.String,
					}}, values),
					Schema: schemaResponse.Schema,
				},
			}
			response := &provider.ConfigureResponse{}
			p.Configure(t.Context(), request, response)
			if test.wantError != "" {
				if !response.Diagnostics.HasError() || !strings.Contains(response.Diagnostics[0].Summary()+" "+response.Diagnostics[0].Detail(), test.wantError) {
					t.Fatalf("expected %q, got %v", test.wantError, response.Diagnostics)
				}
				if response.DataSourceData != nil || response.ResourceData != nil {
					t.Fatal("invalid configuration must not provide a client")
				}
				return
			}
			if response.Diagnostics.HasError() {
				t.Fatalf("unexpected configure diagnostics: %v", response.Diagnostics)
			}
			if _, ok := response.DataSourceData.(*kaneoclient.ClientWithResponses); !ok {
				t.Fatalf("expected configured data source client, got %T", response.DataSourceData)
			}
			if _, ok := response.ResourceData.(*kaneoclient.ClientWithResponses); !ok {
				t.Fatalf("expected configured resource client, got %T", response.ResourceData)
			}
		})
	}
}

func TestProviderRegistersTypes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	p := New("test")()
	resources := p.Resources(ctx)
	want := []string{"kaneo_workspace", "kaneo_project", "kaneo_column", "kaneo_task", "kaneo_label", "kaneo_task_label", "kaneo_user"}
	if len(resources) != len(want) {
		t.Fatalf("expected %d resource registrations, got %d", len(want), len(resources))
	}
	for i, name := range want {
		metadata := &resource.MetadataResponse{}
		resources[i]().Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "kaneo"}, metadata)
		if metadata.TypeName != name {
			t.Fatalf("expected resource %q, got %q", name, metadata.TypeName)
		}
	}

	want = []string{"kaneo_workspace", "kaneo_project", "kaneo_column", "kaneo_task", "kaneo_label"}
	dataSources := p.DataSources(ctx)
	if len(dataSources) != len(want) {
		t.Fatalf("expected %d data source registrations, got %d", len(want), len(dataSources))
	}
	for i, name := range want {
		metadata := &datasource.MetadataResponse{}
		dataSources[i]().Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: "kaneo"}, metadata)
		if metadata.TypeName != name {
			t.Fatalf("expected data source %q, got %q", name, metadata.TypeName)
		}
	}
}

func TestProviderSchema(t *testing.T) {
	t.Parallel()

	p := New("test")()
	response := &provider.SchemaResponse{}
	p.Schema(context.Background(), provider.SchemaRequest{}, response)

	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected schema diagnostics: %v", response.Diagnostics)
	}
	if len(response.Schema.Attributes) != 3 || len(response.Schema.Blocks) != 0 {
		t.Fatalf("expected three provider attributes and no blocks, got %d attributes and %d blocks", len(response.Schema.Attributes), len(response.Schema.Blocks))
	}

	endpoint, ok := response.Schema.Attributes["endpoint"].(providerschema.StringAttribute)
	if !ok || !endpoint.Optional || endpoint.Sensitive {
		t.Fatal("expected endpoint to be an optional, non-sensitive string attribute")
	}
	username, ok := response.Schema.Attributes["username"].(providerschema.StringAttribute)
	if !ok || !username.Optional || username.Sensitive {
		t.Fatal("expected username to be an optional, non-sensitive string attribute")
	}
	password, ok := response.Schema.Attributes["password"].(providerschema.StringAttribute)
	if !ok || !password.Optional || !password.Sensitive {
		t.Fatal("expected password to be an optional, sensitive string attribute")
	}
}
