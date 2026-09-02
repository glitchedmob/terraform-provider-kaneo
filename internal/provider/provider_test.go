// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/provider"
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

func TestProviderSchema(t *testing.T) {
	t.Parallel()

	p := New("test")()
	response := &provider.SchemaResponse{}
	p.Schema(context.Background(), provider.SchemaRequest{}, response)

	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected schema diagnostics: %v", response.Diagnostics)
	}
	if len(response.Schema.Attributes) != 0 || len(response.Schema.Blocks) != 0 {
		t.Fatal("expected the scaffolded provider schema to be empty")
	}
}
