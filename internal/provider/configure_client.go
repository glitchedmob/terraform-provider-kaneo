// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"

	kaneoclient "github.com/glitchedmob/terraform-provider-kaneo/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/diag"
)

func configureClient(data any, target **kaneoclient.ClientWithResponses, diags *diag.Diagnostics) {
	// Terraform may call Configure without provider data; keep any existing client.
	if data == nil {
		return
	}
	client, ok := data.(*kaneoclient.ClientWithResponses)
	if !ok {
		diags.AddError("Unexpected Provider Data Type", fmt.Sprintf("Expected *client.ClientWithResponses, got %T.", data))
		return
	}
	*target = client
}
