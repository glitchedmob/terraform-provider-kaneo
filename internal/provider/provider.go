// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

var _ provider.Provider = &KaneoProvider{}

// KaneoProvider defines the provider implementation.
type KaneoProvider struct {
	version string
}

func (p *KaneoProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "kaneo"
	resp.Version = p.version
}

func (p *KaneoProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{}
}

func (p *KaneoProvider) Configure(context.Context, provider.ConfigureRequest, *provider.ConfigureResponse) {
}

func (p *KaneoProvider) Resources(context.Context) []func() resource.Resource {
	return nil
}

func (p *KaneoProvider) DataSources(context.Context) []func() datasource.DataSource {
	return nil
}

// New returns a provider factory for protocol server registration and tests.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &KaneoProvider{version: version}
	}
}
