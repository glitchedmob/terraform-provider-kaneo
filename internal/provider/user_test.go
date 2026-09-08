// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	kaneoclient "github.com/glitchedmob/terraform-provider-kaneo/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func userTestResource(t *testing.T, handler http.HandlerFunc) (*userResource, tfsdk.State, tfsdk.Plan) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := kaneoclient.NewClientWithResponses(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	r := &userResource{client: client}
	schema := &resource.SchemaResponse{}
	r.Schema(t.Context(), resource.SchemaRequest{}, schema)
	state := tfsdk.State{Schema: schema.Schema}
	model := userModel{ID: types.StringValue("user-1"), Name: types.StringValue("Test"), Email: types.StringValue("Test@Example.com"), Role: types.StringValue("user"), EmailVerified: types.BoolValue(false), PasswordWO: types.StringNull(), PasswordWOVersion: types.Int64Value(1)}
	if d := state.Set(t.Context(), &model); d.HasError() {
		t.Fatal(d)
	}
	return r, state, tfsdk.Plan{Schema: schema.Schema, Raw: state.Raw}
}

func userTestConfig(t *testing.T, plan tfsdk.Plan, password types.String) tfsdk.Config {
	t.Helper()
	configured := tfsdk.State{Schema: plan.Schema, Raw: plan.Raw}
	if d := configured.SetAttribute(t.Context(), path.Root("password_wo"), password); d.HasError() {
		t.Fatal(d)
	}
	return tfsdk.Config{Schema: plan.Schema, Raw: configured.Raw}
}

func TestUserReadAndDeleteErrors(t *testing.T) {
	for _, status := range []int{400, 401, 403, 404, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			r, state, _ := userTestResource(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				fmt.Fprint(w, `{"message":"secret-password"}`)
			})
			read := &resource.ReadResponse{State: state}
			r.Read(t.Context(), resource.ReadRequest{State: state}, read)
			if read.Diagnostics.HasError() != (status != 404) {
				t.Fatalf("read diagnostics: %v", read.Diagnostics)
			}
			if read.State.Raw.IsNull() != (status == 404) {
				t.Fatal("only 404 may remove state")
			}
			del := &resource.DeleteResponse{State: state}
			r.Delete(t.Context(), resource.DeleteRequest{State: state}, del)
			if del.Diagnostics.HasError() != (status != 404) {
				t.Fatalf("delete diagnostics: %v", del.Diagnostics)
			}
			if strings.Contains(fmt.Sprint(read.Diagnostics, del.Diagnostics), "secret-password") {
				t.Fatal("credentials leaked")
			}
		})
	}
}

func TestUserReadPreservesPasswordVersionAndEmailCase(t *testing.T) {
	for _, imported := range []bool{false, true} {
		t.Run(fmt.Sprint(imported), func(t *testing.T) {
			r, state, _ := userTestResource(t, func(w http.ResponseWriter, req *http.Request) {
				if req.URL.Path != "/auth/admin/get-user" || req.URL.Query().Get("id") != "user-1" {
					t.Errorf("unexpected request %s", req.URL)
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"id":"user-1","name":"Drift","email":"test@example.com","role":"admin","emailVerified":true}`)
			})
			if imported {
				empty := userModel{ID: types.StringNull(), Email: types.StringNull(), Name: types.StringNull(), Role: types.StringNull(), PasswordWO: types.StringNull(), PasswordWOVersion: types.Int64Null(), EmailVerified: types.BoolNull()}
				state.Set(t.Context(), &empty)
				result := &resource.ImportStateResponse{State: state}
				r.ImportState(t.Context(), resource.ImportStateRequest{ID: "user-1"}, result)
				if result.Diagnostics.HasError() {
					t.Fatal(result.Diagnostics)
				}
				state = result.State
			}
			result := &resource.ReadResponse{State: state}
			r.Read(t.Context(), resource.ReadRequest{State: state}, result)
			if result.Diagnostics.HasError() {
				t.Fatal(result.Diagnostics)
			}
			var model userModel
			if d := result.State.Get(t.Context(), &model); d.HasError() {
				t.Fatal(d)
			}
			if model.Name.ValueString() != "Drift" || model.Role.ValueString() != "admin" || !model.EmailVerified.ValueBool() {
				t.Fatal("drift not refreshed")
			}
			if imported {
				if !model.PasswordWO.IsNull() || !model.PasswordWOVersion.IsNull() || model.Email.ValueString() != "test@example.com" {
					t.Fatal("import must not invent a password or email casing")
				}
			} else if !model.PasswordWO.IsNull() || model.PasswordWOVersion.ValueInt64() != 1 || model.Email.ValueString() != "Test@Example.com" {
				t.Fatal("read lost password or equivalent email casing")
			}
		})
	}
}

func TestUserPartialCreateAndUpdate(t *testing.T) {
	for _, update := range []bool{false, true} {
		t.Run(fmt.Sprint(update), func(t *testing.T) {
			var calls []string
			r, state, plan := userTestResource(t, func(w http.ResponseWriter, req *http.Request) {
				calls = append(calls, req.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				var body map[string]any
				if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				user := `{"id":"user-1","name":"Test","email":"test@example.com","role":"user","emailVerified":false}`
				switch req.URL.Path {
				case "/auth/admin/create-user":
					if _, ok := body["password"]; ok {
						t.Error("create must not send password")
					}
					if body["email"] != "test@example.com" {
						t.Error("email not normalized")
					}
					fmt.Fprint(w, `{"user":`+user+`}`)
				case "/auth/admin/update-user":
					fmt.Fprint(w, user)
				case "/auth/admin/set-user-password":
					if body["newPassword"] != "secret-password" || body["userId"] != "user-1" {
						t.Error("wrong password request")
					}
					w.WriteHeader(403)
					fmt.Fprint(w, `{"message":"secret-password"}`)
				default:
					t.Errorf("unexpected endpoint %s", req.URL.Path)
				}
			})
			var result tfsdk.State
			if update {
				var old userModel
				state.Get(t.Context(), &old)
				old.PasswordWOVersion = types.Int64Value(2)
				state.Set(t.Context(), &old)
				resp := &resource.UpdateResponse{State: state}
				r.Update(t.Context(), resource.UpdateRequest{Plan: plan, State: state, Config: userTestConfig(t, plan, types.StringValue("secret-password"))}, resp)
				if !resp.Diagnostics.HasError() || strings.Contains(fmt.Sprint(resp.Diagnostics), "secret-password") {
					t.Fatalf("diagnostics: %v", resp.Diagnostics)
				}
				result = resp.State
			} else {
				resp := &resource.CreateResponse{State: tfsdk.State{Schema: state.Schema}}
				r.Create(t.Context(), resource.CreateRequest{Plan: plan, Config: userTestConfig(t, plan, types.StringValue("secret-password"))}, resp)
				if !resp.Diagnostics.HasError() || strings.Contains(fmt.Sprint(resp.Diagnostics), "secret-password") {
					t.Fatalf("diagnostics: %v", resp.Diagnostics)
				}
				result = resp.State
			}
			var model userModel
			if d := result.Get(t.Context(), &model); d.HasError() {
				t.Fatal(d)
			}
			if model.ID.ValueString() != "user-1" {
				t.Fatal("partial failure lost ID")
			}
			if !model.PasswordWO.IsNull() || update && model.PasswordWOVersion.ValueInt64() != 2 || !update && !model.PasswordWOVersion.IsNull() {
				t.Fatal("failed password was saved")
			}
			if len(calls) != 2 {
				t.Fatalf("unexpected calls %v", calls)
			}
		})
	}
}

func TestUserRejectsMalformedSuccess(t *testing.T) {
	for _, body := range []string{`{}`, `null`, `not-json`} {
		t.Run(body, func(t *testing.T) {
			r, state, plan := userTestResource(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, body)
			})
			read := &resource.ReadResponse{State: state}
			r.Read(t.Context(), resource.ReadRequest{State: state}, read)
			if !read.Diagnostics.HasError() || read.State.Raw.IsNull() {
				t.Fatal("malformed read must fail without removing state")
			}
			create := &resource.CreateResponse{State: tfsdk.State{Schema: state.Schema}}
			r.Create(t.Context(), resource.CreateRequest{Plan: plan, Config: userTestConfig(t, plan, types.StringValue("secret-password"))}, create)
			if !create.Diagnostics.HasError() {
				t.Fatal("malformed create succeeded")
			}
			del := &resource.DeleteResponse{State: state}
			r.Delete(t.Context(), resource.DeleteRequest{State: state}, del)
			if !del.Diagnostics.HasError() {
				t.Fatal("malformed delete succeeded")
			}
		})
	}
}

func TestUserOptionalPassword(t *testing.T) {
	for _, operation := range []string{"create without password", "update unchanged password", "stop tracking password"} {
		t.Run(operation, func(t *testing.T) {
			calls := 0
			r, state, plan := userTestResource(t, func(w http.ResponseWriter, req *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/json")
				user := `{"id":"user-1","name":"Test","email":"test@example.com","role":"user","emailVerified":false}`
				switch req.URL.Path {
				case "/auth/admin/create-user":
					fmt.Fprint(w, `{"user":`+user+`}`)
				case "/auth/admin/update-user":
					fmt.Fprint(w, user)
				default:
					t.Errorf("unexpected password operation: %s", req.URL.Path)
				}
			})
			if operation != "update unchanged password" {
				var model userModel
				state.Get(t.Context(), &model)
				model.PasswordWOVersion = types.Int64Null()
				planned := tfsdk.State{Schema: state.Schema}
				planned.Set(t.Context(), &model)
				plan.Raw = planned.Raw
			}
			password := types.StringNull()
			if operation == "update unchanged password" {
				password = types.StringValue("different-secret")
			}
			config := userTestConfig(t, plan, password)
			var result tfsdk.State
			if operation == "create without password" {
				resp := &resource.CreateResponse{State: tfsdk.State{Schema: state.Schema}}
				r.Create(t.Context(), resource.CreateRequest{Plan: plan, Config: config}, resp)
				if resp.Diagnostics.HasError() {
					t.Fatal(resp.Diagnostics)
				}
				result = resp.State
			} else {
				resp := &resource.UpdateResponse{State: state}
				r.Update(t.Context(), resource.UpdateRequest{Plan: plan, State: state, Config: config}, resp)
				if resp.Diagnostics.HasError() {
					t.Fatal(resp.Diagnostics)
				}
				result = resp.State
			}
			if calls != 1 {
				t.Fatalf("want one API call, got %d", calls)
			}
			var model userModel
			if d := result.Get(t.Context(), &model); d.HasError() {
				t.Fatal(d)
			}
			if !model.PasswordWO.IsNull() || model.PasswordWOVersion.IsNull() != (operation != "update unchanged password") {
				t.Fatal("unexpected password state")
			}
		})
	}
}
