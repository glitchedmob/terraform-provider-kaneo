// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	kaneoclient "github.com/glitchedmob/terraform-provider-kaneo/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/oapi-codegen/nullable"
)

type workspaceAPIValue struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Slug        string  `json:"slug"`
	Description *string `json:"description"`
	Logo        *string `json:"logo"`
	CreatedAt   string  `json:"createdAt"`
}

type workspaceAPIServer struct {
	t         *testing.T
	server    *httptest.Server
	mu        sync.Mutex
	workspace *workspaceAPIValue
}

func newWorkspaceAPIServer(t *testing.T, workspace *workspaceAPIValue) *workspaceAPIServer {
	t.Helper()

	testServer := &workspaceAPIServer{t: t, workspace: workspace}
	testServer.server = httptest.NewServer(http.HandlerFunc(testServer.handle))
	t.Cleanup(testServer.server.Close)
	return testServer
}

func (s *workspaceAPIServer) handle(writer http.ResponseWriter, request *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if authorization := request.Header.Get("Authorization"); authorization != "Bearer test-key" {
		s.t.Errorf("expected bearer authorization, got %q", authorization)
	}
	writer.Header().Set("Content-Type", "application/json")

	switch {
	case request.Method == http.MethodPost && request.URL.Path == "/api/auth/organization/create":
		var body struct {
			Name        string  `json:"name"`
			Slug        string  `json:"slug"`
			Description *string `json:"description"`
			Logo        *string `json:"logo"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			s.t.Errorf("decode create request: %v", err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		s.workspace = &workspaceAPIValue{
			ID:          "workspace-1",
			Name:        body.Name,
			Slug:        body.Slug,
			Description: body.Description,
			Logo:        body.Logo,
			CreatedAt:   "2026-01-02T03:04:05Z",
		}
		json.NewEncoder(writer).Encode(s.workspace)
	case request.Method == http.MethodGet && request.URL.Path == "/api/auth/organization/list":
		if s.workspace == nil {
			json.NewEncoder(writer).Encode([]workspaceAPIValue{})
			return
		}
		json.NewEncoder(writer).Encode([]workspaceAPIValue{*s.workspace})
	case request.Method == http.MethodPost && request.URL.Path == "/api/auth/organization/update":
		var body struct {
			OrganizationID string `json:"organizationId"`
			Data           struct {
				Name        *string `json:"name"`
				Slug        *string `json:"slug"`
				Description *string `json:"description"`
				Logo        *string `json:"logo"`
			} `json:"data"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			s.t.Errorf("decode update request: %v", err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		if s.workspace == nil || body.OrganizationID != s.workspace.ID {
			http.Error(writer, "workspace not found", http.StatusBadRequest)
			return
		}
		if body.Data.Name != nil {
			s.workspace.Name = *body.Data.Name
		}
		if body.Data.Slug != nil {
			s.workspace.Slug = *body.Data.Slug
		}
		s.workspace.Description = body.Data.Description
		s.workspace.Logo = body.Data.Logo
		json.NewEncoder(writer).Encode(s.workspace)
	case request.Method == http.MethodPost && request.URL.Path == "/api/auth/organization/delete":
		var body struct {
			OrganizationID string `json:"organizationId"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			s.t.Errorf("decode delete request: %v", err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		if s.workspace == nil || body.OrganizationID != s.workspace.ID {
			http.Error(writer, "workspace not found", http.StatusBadRequest)
			return
		}
		json.NewEncoder(writer).Encode(s.workspace)
		s.workspace = nil
	default:
		http.NotFound(writer, request)
	}
}

func (s *workspaceAPIServer) client(t *testing.T) *workspaceResource {
	t.Helper()
	client, err := newAPIClient(s.server.URL+"/api", "test-key", "test")
	if err != nil {
		t.Fatalf("create API client: %v", err)
	}
	return &workspaceResource{client: client}
}

func TestWorkspaceResourceLifecycle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	server := newWorkspaceAPIServer(t, nil)
	workspaceResource := server.client(t)
	schemaResponse := &resource.SchemaResponse{}
	workspaceResource.Schema(ctx, resource.SchemaRequest{}, schemaResponse)

	createResponse := &resource.CreateResponse{State: tfsdk.State{Schema: schemaResponse.Schema}}
	workspaceResource.Create(ctx, resource.CreateRequest{
		Plan: tfsdk.Plan{
			Raw: workspaceRawValue(map[string]any{
				"id":          tftypes.UnknownValue,
				"name":        "Engineering",
				"slug":        "engineering",
				"description": "Engineering workspace",
				"logo":        nil,
				"created_at":  tftypes.UnknownValue,
			}),
			Schema: schemaResponse.Schema,
		},
	}, createResponse)
	if createResponse.Diagnostics.HasError() {
		t.Fatalf("unexpected create diagnostics: %v", createResponse.Diagnostics)
	}

	var created workspaceModel
	if diagnostics := createResponse.State.Get(ctx, &created); diagnostics.HasError() {
		t.Fatalf("read created state: %v", diagnostics)
	}
	if created.ID.ValueString() != "workspace-1" || created.Slug.ValueString() != "engineering" {
		t.Fatalf("unexpected created state: %#v", created)
	}

	updateResponse := &resource.UpdateResponse{State: tfsdk.State{Schema: schemaResponse.Schema}}
	workspaceResource.Update(ctx, resource.UpdateRequest{
		Plan: tfsdk.Plan{
			Raw: workspaceRawValue(map[string]any{
				"id":          "workspace-1",
				"name":        "Product Engineering",
				"slug":        "product-engineering",
				"description": "Updated description",
				"logo":        "https://example.com/logo.png",
				"created_at":  "2026-01-02T03:04:05Z",
			}),
			Schema: schemaResponse.Schema,
		},
	}, updateResponse)
	if updateResponse.Diagnostics.HasError() {
		t.Fatalf("unexpected update diagnostics: %v", updateResponse.Diagnostics)
	}

	var updated workspaceModel
	if diagnostics := updateResponse.State.Get(ctx, &updated); diagnostics.HasError() {
		t.Fatalf("read updated state: %v", diagnostics)
	}
	if updated.Name.ValueString() != "Product Engineering" || updated.Logo.ValueString() != "https://example.com/logo.png" {
		t.Fatalf("unexpected updated state: %#v", updated)
	}

	readResponse := &resource.ReadResponse{State: tfsdk.State{Schema: schemaResponse.Schema}}
	workspaceResource.Read(ctx, resource.ReadRequest{State: updateResponse.State}, readResponse)
	if readResponse.Diagnostics.HasError() {
		t.Fatalf("unexpected read diagnostics: %v", readResponse.Diagnostics)
	}

	deleteResponse := &resource.DeleteResponse{State: readResponse.State}
	workspaceResource.Delete(ctx, resource.DeleteRequest{State: readResponse.State}, deleteResponse)
	if deleteResponse.Diagnostics.HasError() {
		t.Fatalf("unexpected delete diagnostics: %v", deleteResponse.Diagnostics)
	}
}

func TestWorkspaceResourceRemovesMissingWorkspace(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	server := newWorkspaceAPIServer(t, nil)
	workspaceResource := server.client(t)
	schemaResponse := &resource.SchemaResponse{}
	workspaceResource.Schema(ctx, resource.SchemaRequest{}, schemaResponse)
	state := tfsdk.State{
		Raw: workspaceRawValue(map[string]any{
			"id":          "missing",
			"name":        "Missing",
			"slug":        "missing",
			"description": nil,
			"logo":        nil,
			"created_at":  "2026-01-02T03:04:05Z",
		}),
		Schema: schemaResponse.Schema,
	}
	response := &resource.ReadResponse{State: tfsdk.State{Schema: schemaResponse.Schema}}
	workspaceResource.Read(ctx, resource.ReadRequest{State: state}, response)

	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected read diagnostics: %v", response.Diagnostics)
	}
	if !response.State.Raw.IsNull() {
		t.Fatal("expected missing workspace to be removed from state")
	}
}

func TestWorkspaceResourceImport(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspaceResource := &workspaceResource{}
	schemaResponse := &resource.SchemaResponse{}
	workspaceResource.Schema(ctx, resource.SchemaRequest{}, schemaResponse)
	response := &resource.ImportStateResponse{State: tfsdk.State{
		Raw:    workspaceRawValue(map[string]any{}),
		Schema: schemaResponse.Schema,
	}}
	workspaceResource.ImportState(ctx, resource.ImportStateRequest{ID: "workspace-1"}, response)

	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected import diagnostics: %v", response.Diagnostics)
	}
	var id types.String
	if diagnostics := response.State.GetAttribute(ctx, path.Root("id"), &id); diagnostics.HasError() {
		t.Fatalf("read imported ID: %v", diagnostics)
	}
	if id.ValueString() != "workspace-1" {
		t.Fatalf("expected imported ID %q, got %q", "workspace-1", id.ValueString())
	}
}

func TestWorkspaceDataSourceLookup(t *testing.T) {
	t.Parallel()

	description := "Existing workspace"
	workspace := &workspaceAPIValue{
		ID:          "workspace-1",
		Name:        "Engineering",
		Slug:        "engineering",
		Description: &description,
		CreatedAt:   "2026-01-02T03:04:05Z",
	}
	server := newWorkspaceAPIServer(t, workspace)
	resourceClient := server.client(t)
	dataSource := &workspaceDataSource{client: resourceClient.client}
	ctx := context.Background()
	schemaResponse := &datasource.SchemaResponse{}
	dataSource.Schema(ctx, datasource.SchemaRequest{}, schemaResponse)

	for name, lookup := range map[string]map[string]any{
		"by ID": {
			"id":   "workspace-1",
			"slug": nil,
		},
		"by slug": {
			"id":   nil,
			"slug": "engineering",
		},
	} {
		t.Run(name, func(t *testing.T) {
			response := &datasource.ReadResponse{State: tfsdk.State{Schema: schemaResponse.Schema}}
			dataSource.Read(ctx, datasource.ReadRequest{
				Config: tfsdk.Config{
					Raw: workspaceRawValue(map[string]any{
						"id":          lookup["id"],
						"name":        nil,
						"slug":        lookup["slug"],
						"description": nil,
						"logo":        nil,
						"created_at":  nil,
					}),
					Schema: schemaResponse.Schema,
				},
			}, response)
			if response.Diagnostics.HasError() {
				t.Fatalf("unexpected data source diagnostics: %v", response.Diagnostics)
			}

			var state workspaceModel
			if diagnostics := response.State.Get(ctx, &state); diagnostics.HasError() {
				t.Fatalf("read data source state: %v", diagnostics)
			}
			if state.ID.ValueString() != "workspace-1" || state.Slug.ValueString() != "engineering" {
				t.Fatalf("unexpected data source state: %#v", state)
			}
		})
	}
}

func TestWorkspaceDataSourceRequiresOneLookupAttribute(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dataSource := &workspaceDataSource{}
	schemaResponse := &datasource.SchemaResponse{}
	dataSource.Schema(ctx, datasource.SchemaRequest{}, schemaResponse)
	validator := dataSource.ConfigValidators(ctx)[0]

	for name, test := range map[string]struct {
		lookup    map[string]any
		wantError bool
	}{
		"neither": {lookup: map[string]any{"id": nil, "slug": nil}, wantError: true},
		"both":    {lookup: map[string]any{"id": "workspace-1", "slug": "engineering"}, wantError: true},
		"ID":      {lookup: map[string]any{"id": "workspace-1", "slug": nil}},
		"slug":    {lookup: map[string]any{"id": nil, "slug": "engineering"}},
	} {
		t.Run(name, func(t *testing.T) {
			request := datasource.ValidateConfigRequest{Config: tfsdk.Config{
				Raw: workspaceRawValue(map[string]any{
					"id":          test.lookup["id"],
					"name":        nil,
					"slug":        test.lookup["slug"],
					"description": nil,
					"logo":        nil,
					"created_at":  nil,
				}),
				Schema: schemaResponse.Schema,
			}}
			response := &datasource.ValidateConfigResponse{}
			validator.ValidateDataSource(ctx, request, response)
			if response.Diagnostics.HasError() != test.wantError {
				t.Fatalf("expected validation error to be %t, got diagnostics: %v", test.wantError, response.Diagnostics)
			}
		})
	}
}

func workspaceRawValue(attributes map[string]any) tftypes.Value {
	attributeTypes := map[string]tftypes.Type{
		"id":          tftypes.String,
		"name":        tftypes.String,
		"slug":        tftypes.String,
		"description": tftypes.String,
		"logo":        tftypes.String,
		"created_at":  tftypes.String,
	}
	values := make(map[string]tftypes.Value, len(attributeTypes))
	for name, valueType := range attributeTypes {
		values[name] = tftypes.NewValue(valueType, attributes[name])
	}
	return tftypes.NewValue(tftypes.Object{AttributeTypes: attributeTypes}, values)
}

func TestWorkspaceModelUsesMetadataDescriptionFallback(t *testing.T) {
	t.Parallel()

	workspace := workspaceModelFromAPI(workspaceFromMetadata("Metadata description"))
	if workspace.Description.ValueString() != "Metadata description" {
		t.Fatalf("expected metadata description fallback, got %q", workspace.Description.ValueString())
	}
}

func workspaceFromMetadata(description string) kaneoclient.Workspace {
	metadata := nullable.NewNullableWithValue(map[string]any{"description": description})
	return kaneoclient.Workspace{
		Id:        "workspace-1",
		Name:      "Engineering",
		Slug:      "engineering",
		Metadata:  metadata,
		CreatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}
}
