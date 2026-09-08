// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"testing"

	kaneoclient "github.com/glitchedmob/terraform-provider-kaneo/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/diag"
)

func TestConfigureClient(t *testing.T) {
	oldClient, newClient := &kaneoclient.ClientWithResponses{}, &kaneoclient.ClientWithResponses{}
	for _, tc := range []struct {
		name      string
		data      any
		want      *kaneoclient.ClientWithResponses
		wantError bool
	}{
		{"success", newClient, newClient, false},
		{"nil preserves client", nil, oldClient, false},
		{"wrong type preserves client", "invalid", oldClient, true},
		{"typed nil", (*kaneoclient.ClientWithResponses)(nil), nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := oldClient
			var diags diag.Diagnostics
			configureClient(tc.data, &client, &diags)
			if client != tc.want {
				t.Fatalf("client = %p, want %p", client, tc.want)
			}
			if tc.wantError {
				if len(diags) != 1 || diags[0].Severity() != diag.SeverityError ||
					diags[0].Summary() != "Unexpected Provider Data Type" ||
					diags[0].Detail() != "Expected *client.ClientWithResponses, got string." {
					t.Fatalf("unexpected diagnostics: %v", diags)
				}
			} else if len(diags) != 0 {
				t.Fatalf("unexpected diagnostics: %v", diags)
			}
		})
	}
}
