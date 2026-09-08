// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

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
	Username types.String `tfsdk:"username"`
	Password types.String `tfsdk:"password"`
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
			"username": schema.StringAttribute{
				MarkdownDescription: "Kaneo account email address for password sign-in. May also be set with the `KANEO_USERNAME` environment variable.",
				Optional:            true,
			},
			"password": schema.StringAttribute{
				MarkdownDescription: "Kaneo account password. May also be set with the `KANEO_PASSWORD` environment variable.",
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
	if config.Username.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("username"),
			"Unknown Kaneo Username",
			"The username must be known while the provider is being configured.",
		)
	}
	if config.Password.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("password"),
			"Unknown Kaneo Password",
			"The password must be known while the provider is being configured.",
		)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	endpoint, username, password := resolveProviderConfig(config, os.Getenv)
	client, err := newAPIClient(ctx, endpoint, username, password, p.version)
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
		newUserResource,
		newWorkspaceRoleResource,
		newWorkspaceMemberResource,
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

func resolveProviderConfig(config KaneoProviderModel, getenv func(string) string) (string, string, string) {
	endpoint := strings.TrimSpace(getenv("KANEO_API_URL"))
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	if !config.Endpoint.IsNull() {
		endpoint = strings.TrimSpace(config.Endpoint.ValueString())
	}

	username := getenv("KANEO_USERNAME")
	if !config.Username.IsNull() {
		username = config.Username.ValueString()
	}
	password := getenv("KANEO_PASSWORD")
	if !config.Password.IsNull() {
		password = config.Password.ValueString()
	}
	return endpoint, strings.TrimSpace(username), password
}

func newAPIClient(ctx context.Context, endpoint, username, password, version string) (*kaneoclient.ClientWithResponses, error) {
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

	if parsedEndpoint.User != nil {
		return nil, fmt.Errorf("endpoint must not include user credentials")
	}
	if strings.TrimSpace(username) == "" {
		return nil, fmt.Errorf("set username or KANEO_USERNAME to the Kaneo account email address")
	}
	if password == "" {
		return nil, fmt.Errorf("set password or KANEO_PASSWORD to the Kaneo account password")
	}

	endpoint = strings.TrimRight(endpoint, "/")
	userAgent := "terraform-provider-kaneo/" + version
	httpClient := &http.Client{
		Timeout: 30 * time.Second,
		// Do not forward credentials or session tokens through redirects.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	// Kaneo's OpenAPI specification does not include email/password sign-in.
	body, err := json.Marshal(map[string]string{"email": username, "password": password})
	if err != nil {
		return nil, fmt.Errorf("encode sign-in request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/auth/sign-in/email", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create sign-in request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", userAgent)
	response, err := httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("kaneo sign-in failed: %w", err)
	}
	// Closing a read-only response cannot affect the decoded session or diagnostics.
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		// Authentication responses can contain secrets; never include their bodies in diagnostics.
		return nil, fmt.Errorf("kaneo email/password sign-in returned HTTP %d", response.StatusCode)
	}
	var session struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&session); err != nil {
		return nil, fmt.Errorf("kaneo sign-in returned an invalid JSON response")
	}
	if strings.TrimSpace(session.Token) == "" {
		return nil, fmt.Errorf("kaneo sign-in returned no session token; interactive authentication is not supported")
	}

	requestEditor := func(_ context.Context, request *http.Request) error {
		request.Header.Set("User-Agent", userAgent)
		request.Header.Set("Authorization", "Bearer "+session.Token)
		return nil
	}
	return kaneoclient.NewClientWithResponses(
		endpoint,
		kaneoclient.WithHTTPClient(httpClient),
		kaneoclient.WithRequestEditorFn(requestEditor),
	)
}
