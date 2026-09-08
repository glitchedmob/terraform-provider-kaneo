// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestUserWriteOnlySchemaAndValidation(t *testing.T) {
	r, _, plan := userTestResource(t, func(http.ResponseWriter, *http.Request) { t.Error("validation called API") })
	attrs := plan.Schema.(schema.Schema).Attributes
	if _, exists := attrs["password"]; exists {
		t.Fatal("old password attribute exists")
	}
	password := attrs["password_wo"].(schema.StringAttribute)
	if !password.Optional || !password.Sensitive || !password.WriteOnly || password.Computed {
		t.Fatal("incorrect write-only schema")
	}
	version := attrs["password_wo_version"].(schema.Int64Attribute)
	if !version.Optional || version.Computed || version.WriteOnly {
		t.Fatal("incorrect version schema")
	}
	for _, tc := range []struct {
		name     string
		password types.String
		version  types.Int64
		invalid  bool
	}{
		{"omitted", types.StringNull(), types.Int64Null(), false},
		{"paired", types.StringValue("test-password"), types.Int64Value(1), false},
		{"missing version", types.StringValue("test-password"), types.Int64Null(), true},
		{"missing password", types.StringNull(), types.Int64Value(1), true},
		{"unknown password", types.StringUnknown(), types.Int64Value(1), false},
		{"unknown version", types.StringValue("test-password"), types.Int64Unknown(), false},
		{"unknown may become null", types.StringUnknown(), types.Int64Null(), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Use framework values to preserve null and unknown exactly.
			state := tfsdk.State{Schema: plan.Schema, Raw: plan.Raw}
			if d := state.SetAttribute(t.Context(), path.Root("password_wo_version"), tc.version); d.HasError() {
				t.Fatal(d)
			}
			configuredPlan := tfsdk.Plan{Schema: plan.Schema, Raw: state.Raw}
			config := userTestConfig(t, configuredPlan, tc.password)
			resp := &resource.ValidateConfigResponse{}
			for _, v := range r.ConfigValidators(t.Context()) {
				v.ValidateResource(t.Context(), resource.ValidateConfigRequest{Config: config}, resp)
			}
			if resp.Diagnostics.HasError() != tc.invalid {
				t.Fatalf("pair validation: %v", resp.Diagnostics)
			}
			if strings.Contains(fmt.Sprint(resp.Diagnostics), "test-password") {
				t.Fatal("password leaked")
			}
		})
	}
	for _, n := range []int{7, 8, 128, 129} {
		value := strings.Repeat("x", n)
		resp := &validator.StringResponse{}
		for _, v := range password.Validators {
			v.ValidateString(t.Context(), validator.StringRequest{Path: path.Root("password_wo"), ConfigValue: types.StringValue(value)}, resp)
		}
		if resp.Diagnostics.HasError() != (n < 8 || n > 128) {
			t.Fatalf("length %d: %v", n, resp.Diagnostics)
		}
		if strings.Contains(fmt.Sprint(resp.Diagnostics), value) {
			t.Fatal("length validator leaked password")
		}
	}
	for _, value := range []types.Int64{types.Int64Null(), types.Int64Unknown(), types.Int64Value(-1), types.Int64Value(0), types.Int64Value(1)} {
		resp := &validator.Int64Response{}
		for _, v := range version.Validators {
			v.ValidateInt64(t.Context(), validator.Int64Request{Path: path.Root("password_wo_version"), ConfigValue: value}, resp)
		}
		invalid := !value.IsNull() && !value.IsUnknown() && value.ValueInt64() < 1
		if resp.Diagnostics.HasError() != invalid {
			t.Fatalf("version validation: %v", resp.Diagnostics)
		}
	}
}

func TestUserPasswordApplyRejectsInvalidConfig(t *testing.T) {
	for _, tc := range []struct {
		name     string
		password types.String
		version  types.Int64
	}{
		{"unknown", types.StringUnknown(), types.Int64Value(1)},
		{"unknown version", types.StringValue("test-password"), types.Int64Unknown()},
		{"null", types.StringNull(), types.Int64Value(1)},
		{"missing version", types.StringValue("test-password"), types.Int64Null()},
		{"short", types.StringValue("short"), types.Int64Value(1)},
		{"long", types.StringValue(strings.Repeat("x", 129)), types.Int64Value(1)},
		{"zero version", types.StringValue("test-password"), types.Int64Value(0)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, state, plan := userTestResource(t, func(http.ResponseWriter, *http.Request) { t.Error("invalid config reached API") })
			configured := tfsdk.State{Schema: plan.Schema, Raw: plan.Raw}
			if d := configured.SetAttribute(t.Context(), path.Root("password_wo_version"), tc.version); d.HasError() {
				t.Fatal(d)
			}
			plan.Raw = configured.Raw
			config := userTestConfig(t, plan, tc.password)
			create := &resource.CreateResponse{State: state}
			r.Create(t.Context(), resource.CreateRequest{Plan: plan, Config: config}, create)
			update := &resource.UpdateResponse{State: state}
			r.Update(t.Context(), resource.UpdateRequest{Plan: plan, State: state, Config: config}, update)
			if !create.Diagnostics.HasError() || !update.Diagnostics.HasError() {
				t.Fatal("invalid apply config accepted")
			}
			if secret := tc.password.ValueString(); secret != "" && strings.Contains(fmt.Sprint(create.Diagnostics, update.Diagnostics), secret) {
				t.Fatal("apply diagnostics leaked password")
			}
		})
	}
}
