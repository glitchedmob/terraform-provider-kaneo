// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	kaneoclient "github.com/glitchedmob/terraform-provider-kaneo/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const defaultEndpoint = "https://cloud.kaneo.app/api"

var _ provider.Provider = &KaneoProvider{}

// KaneoProvider defines the provider implementation.
type KaneoProvider struct {
	version string
}

// KaneoProviderModel describes the provider configuration.
type KaneoProviderModel struct {
	Endpoint types.String `tfsdk:"endpoint"`
	APIKey   types.String `tfsdk:"api_key"`
}

func (p *KaneoProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "kaneo"
	resp.Version = p.version
}

func (p *KaneoProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The Kaneo provider configures access to a Kaneo API.",
		Attributes: map[string]schema.Attribute{
			"endpoint": schema.StringAttribute{
				MarkdownDescription: "Kaneo API base URL. Defaults to `https://cloud.kaneo.app/api`. May also be set with the `KANEO_API_URL` environment variable.",
				Optional:            true,
			},
			"api_key": schema.StringAttribute{
				MarkdownDescription: "Kaneo API key. May also be set with the `KANEO_API_KEY` environment variable.",
				Optional:            true,
				Sensitive:           true,
			},
		},
	}
}

func (p *KaneoProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config KaneoProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if config.Endpoint.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("endpoint"),
			"Unknown Kaneo API Endpoint",
			"The endpoint must be known while the provider is being configured.",
		)
	}
	if config.APIKey.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("api_key"),
			"Unknown Kaneo API Key",
			"The API key must be known while the provider is being configured.",
		)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	endpoint, apiKey := resolveProviderConfig(config, os.Getenv)
	client, err := newAPIClient(endpoint, apiKey, p.version)
	if err != nil {
		resp.Diagnostics.AddError("Unable to Configure Kaneo API Client", err.Error())
		return
	}

	resp.DataSourceData = client
	resp.ResourceData = client
}

func (p *KaneoProvider) Resources(context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		newWorkspaceResource,
		newProjectResource,
		newColumnResource,
		newTaskResource,
		newLabelResource,
		newTaskLabelResource,
	}
}

func (p *KaneoProvider) DataSources(context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		newWorkspaceDataSource,
		newProjectDataSource,
		newColumnDataSource,
		newTaskDataSource,
		newLabelDataSource,
	}
}

// New returns a provider factory for protocol server registration and tests.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &KaneoProvider{version: version}
	}
}

func resolveProviderConfig(config KaneoProviderModel, getenv func(string) string) (string, string) {
	endpoint := strings.TrimSpace(getenv("KANEO_API_URL"))
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	if !config.Endpoint.IsNull() {
		endpoint = strings.TrimSpace(config.Endpoint.ValueString())
	}

	apiKey := getenv("KANEO_API_KEY")
	if !config.APIKey.IsNull() {
		apiKey = config.APIKey.ValueString()
	}
	return endpoint, apiKey
}

func newAPIClient(endpoint, apiKey, version string) (*kaneoclient.ClientWithResponses, error) {
	parsedEndpoint, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse endpoint: %w", err)
	}
	if parsedEndpoint.Scheme != "http" && parsedEndpoint.Scheme != "https" {
		return nil, fmt.Errorf("endpoint must use HTTP or HTTPS")
	}
	if parsedEndpoint.Host == "" {
		return nil, fmt.Errorf("endpoint must include a host")
	}
	if parsedEndpoint.RawQuery != "" || parsedEndpoint.Fragment != "" {
		return nil, fmt.Errorf("endpoint must not include a query string or fragment")
	}

	requestEditor := func(_ context.Context, request *http.Request) error {
		request.Header.Set("User-Agent", "terraform-provider-kaneo/"+version)
		if apiKey != "" {
			request.Header.Set("Authorization", "Bearer "+apiKey)
		}
		return nil
	}

	return kaneoclient.NewClientWithResponses(
		strings.TrimRight(endpoint, "/"),
		kaneoclient.WithRequestEditorFn(requestEditor),
	)
}
