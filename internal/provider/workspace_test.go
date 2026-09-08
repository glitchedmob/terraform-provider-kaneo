// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"net/http"
	"reflect"
	"testing"
	"time"

	kaneoclient "github.com/glitchedmob/terraform-provider-kaneo/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/oapi-codegen/nullable"
)

const workspaceFixture = `{"id":"workspace-1","name":"Engineering","slug":"engineering","description":null,"logo":null,"createdAt":"2026-01-02T03:04:05Z"}`

func TestWorkspaceMutationWireShapes(t *testing.T) {
	for _, operation := range []string{"create", "update", "delete"} {
		t.Run(operation, func(t *testing.T) {
			calls := 0
			r := &workspaceResource{client: testClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodPost || r.URL.Path != "/auth/organization/"+operation {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				var body map[string]any
				if !testDecodeRequest(t, w, r, &body) {
					return
				}
				want := map[string]any{"name": "Engineering", "slug": "engineering", "description": nil, "logo": nil}
				if operation == "update" {
					want = map[string]any{"organizationId": "workspace-1", "data": want}
				}
				if operation == "delete" {
					want = map[string]any{"organizationId": "workspace-1"}
				}
				if !reflect.DeepEqual(body, want) {
					t.Errorf("body = %v, want %v", body, want)
				}
				if _, err := fmt.Fprint(w, workspaceFixture); err != nil {
					t.Error(err)
				}
			})}
			model := workspaceModel{ID: types.StringValue("workspace-1"), Name: types.StringValue("Engineering"), Slug: types.StringValue("engineering"), Description: types.StringNull(), Logo: types.StringNull(), CreatedAt: types.StringUnknown()}
			if operation == "create" {
				model.ID = types.StringUnknown()
			}
			plan := testPlan(t, r, model)
			state := tfsdk.State{Schema: plan.Schema}
			switch operation {
			case "create":
				response := resource.CreateResponse{State: state}
				r.Create(t.Context(), resource.CreateRequest{Plan: plan}, &response)
				if response.Diagnostics.HasError() {
					t.Fatal(response.Diagnostics)
				}
				state = response.State
			case "update":
				response := resource.UpdateResponse{State: state}
				r.Update(t.Context(), resource.UpdateRequest{Plan: plan}, &response)
				if response.Diagnostics.HasError() {
					t.Fatal(response.Diagnostics)
				}
				state = response.State
			case "delete":
				response := resource.DeleteResponse{State: tfsdk.State(plan)}
				r.Delete(t.Context(), resource.DeleteRequest{State: tfsdk.State(plan)}, &response)
				if response.Diagnostics.HasError() {
					t.Fatal(response.Diagnostics)
				}
			}
			if calls != 1 {
				t.Fatalf("calls = %d, want 1", calls)
			}
			if operation != "delete" {
				var got workspaceModel
				if diags := state.Get(t.Context(), &got); diags.HasError() {
					t.Fatal(diags)
				}
				if got.ID.ValueString() != "workspace-1" || got.CreatedAt.ValueString() != "2026-01-02T03:04:05Z" || !got.Description.IsNull() || !got.Logo.IsNull() {
					t.Fatalf("unexpected state: %+v", got)
				}
			}
		})
	}
}

func TestWorkspaceResourceRemovesMissingWorkspace(t *testing.T) {
	t.Parallel()
	r := &workspaceResource{client: testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/auth/organization/list" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if _, err := fmt.Fprint(w, `[]`); err != nil {
			t.Error(err)
		}
	})}
	state := tfsdk.State(testPlan(t, r, workspaceModel{ID: types.StringValue("missing"), Name: types.StringValue("Missing"), Slug: types.StringValue("missing"), Description: types.StringNull(), Logo: types.StringNull(), CreatedAt: types.StringValue("2026-01-02T03:04:05Z")}))
	response := resource.ReadResponse{State: state}
	r.Read(t.Context(), resource.ReadRequest{State: state}, &response)
	if response.Diagnostics.HasError() {
		t.Fatal(response.Diagnostics)
	}
	if !response.State.Raw.IsNull() {
		t.Fatal("expected missing workspace to be removed from state")
	}
}

func TestWorkspaceDataSourceLookup(t *testing.T) {
	t.Parallel()
	d := &workspaceDataSource{client: testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/auth/organization/list" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if _, err := fmt.Fprint(w, `[`+workspaceFixture+`]`); err != nil {
			t.Error(err)
		}
	})}
	for name, lookup := range map[string]workspaceModel{
		"by ID":   {ID: types.StringValue("workspace-1")},
		"by slug": {Slug: types.StringValue("engineering")},
	} {
		t.Run(name, func(t *testing.T) {
			config := testConfig(t, d, lookup)
			response := datasource.ReadResponse{State: tfsdk.State{Schema: config.Schema}}
			d.Read(t.Context(), datasource.ReadRequest{Config: config}, &response)
			if response.Diagnostics.HasError() {
				t.Fatal(response.Diagnostics)
			}
			var state workspaceModel
			if diags := response.State.Get(t.Context(), &state); diags.HasError() {
				t.Fatal(diags)
			}
			if state.ID.ValueString() != "workspace-1" || state.Slug.ValueString() != "engineering" {
				t.Fatalf("unexpected state: %+v", state)
			}
		})
	}
}

func TestWorkspaceDataSourceRequiresOneLookupAttribute(t *testing.T) {
	t.Parallel()
	d := &workspaceDataSource{}
	validator := d.ConfigValidators(t.Context())[0]
	for name, test := range map[string]struct {
		lookup    workspaceModel
		wantError bool
	}{
		"neither": {lookup: workspaceModel{}, wantError: true},
		"both":    {lookup: workspaceModel{ID: types.StringValue("workspace-1"), Slug: types.StringValue("engineering")}, wantError: true},
		"ID":      {lookup: workspaceModel{ID: types.StringValue("workspace-1")}},
		"slug":    {lookup: workspaceModel{Slug: types.StringValue("engineering")}},
	} {
		t.Run(name, func(t *testing.T) {
			request := datasource.ValidateConfigRequest{Config: testConfig(t, d, test.lookup)}
			response := datasource.ValidateConfigResponse{}
			validator.ValidateDataSource(t.Context(), request, &response)
			if response.Diagnostics.HasError() != test.wantError {
				t.Fatalf("want error %t: %v", test.wantError, response.Diagnostics)
			}
		})
	}
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
	return kaneoclient.Workspace{Id: "workspace-1", Name: "Engineering", Slug: "engineering", Metadata: metadata, CreatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
}
