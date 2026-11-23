package connection

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"github.com/stretchr/testify/require"
)

func TestStatusValidation(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		status  string
		wantErr bool
	}{
		{status: "ACTIVE"},
		{status: "INACTIVE"},
		{status: "INACTIVE ", wantErr: true},
		{status: "active", wantErr: true},
		{status: "DELETED", wantErr: true},
	} {
		t.Run(tt.status, func(t *testing.T) {
			t.Parallel()
			config := terraform.NewResourceConfigRaw(map[string]any{
				"cluster_id": 3,
				"datacenter": "AWS_US_EAST_1",
				"type":       "AWS_TGW_ATTACHMENT",
				"cidrlist":   []any{"10.0.0.0/16"},
				"data":       map[string]any{"tgwid": "tgw-1", "ramarn": "arn:aws:ram::1:resource-share/1"},
				"status":     tt.status,
			})

			diags := ResourceClusterConnection().Validate(config)

			require.Equal(t, tt.wantErr, diags.HasError(), "%v", diags)
		})
	}
}
