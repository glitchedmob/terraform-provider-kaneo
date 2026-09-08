// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
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

	request := provider.ConfigureRequest{
		Config: tfsdk.Config{
			Raw: tftypes.NewValue(
				tftypes.Object{AttributeTypes: map[string]tftypes.Type{
					"endpoint": tftypes.String,
					"api_key":  tftypes.String,
				}},
				map[string]tftypes.Value{
					"endpoint": tftypes.NewValue(tftypes.String, "https://kaneo.example/api"),
					"api_key":  tftypes.NewValue(tftypes.String, "test-key"),
				},
			),
			Schema: schemaResponse.Schema,
		},
	}
	response := &provider.ConfigureResponse{}
	p.Configure(context.Background(), request, response)

	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected configure diagnostics: %v", response.Diagnostics)
	}
	if _, ok := response.DataSourceData.(*kaneoclient.ClientWithResponses); !ok {
		t.Fatalf("expected configured data source client, got %T", response.DataSourceData)
	}
	if _, ok := response.ResourceData.(*kaneoclient.ClientWithResponses); !ok {
		t.Fatalf("expected configured resource client, got %T", response.ResourceData)
	}
}

func TestProviderRegistersWorkspaceTypes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	p := New("test")()
	resources := p.Resources(ctx)
	if len(resources) != 1 {
		t.Fatalf("expected one resource registration, got %d", len(resources))
	}
	resourceMetadata := &resource.MetadataResponse{}
	resources[0]().Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "kaneo"}, resourceMetadata)
	if resourceMetadata.TypeName != "kaneo_workspace" {
		t.Fatalf("expected workspace resource, got %q", resourceMetadata.TypeName)
	}

	dataSources := p.DataSources(ctx)
	if len(dataSources) != 1 {
		t.Fatalf("expected one data source registration, got %d", len(dataSources))
	}
	dataSourceMetadata := &datasource.MetadataResponse{}
	dataSources[0]().Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: "kaneo"}, dataSourceMetadata)
	if dataSourceMetadata.TypeName != "kaneo_workspace" {
		t.Fatalf("expected workspace data source, got %q", dataSourceMetadata.TypeName)
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
	if len(response.Schema.Attributes) != 2 || len(response.Schema.Blocks) != 0 {
		t.Fatalf("expected two provider attributes and no blocks, got %d attributes and %d blocks", len(response.Schema.Attributes), len(response.Schema.Blocks))
	}

	endpoint, ok := response.Schema.Attributes["endpoint"].(providerschema.StringAttribute)
	if !ok || !endpoint.Optional || endpoint.Sensitive {
		t.Fatal("expected endpoint to be an optional, non-sensitive string attribute")
	}
	apiKey, ok := response.Schema.Attributes["api_key"].(providerschema.StringAttribute)
	if !ok || !apiKey.Optional || !apiKey.Sensitive {
		t.Fatal("expected api_key to be an optional, sensitive string attribute")
	}
}
