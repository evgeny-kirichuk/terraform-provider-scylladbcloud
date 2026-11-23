package cluster

import (
	"context"
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/stretchr/testify/require"

	"github.com/scylladb/terraform-provider-scylladbcloud/internal/scylla"
	"github.com/scylladb/terraform-provider-scylladbcloud/internal/scylla/model"
	"github.com/scylladb/terraform-provider-scylladbcloud/internal/scylla/scyllatest"
)

const (
	testClusterID = 3
	testRequestID = 9
)

func testCreateRequest(status string) model.ClusterRequest {
	return model.ClusterRequest{
		ID:                  testRequestID,
		ClusterID:           testClusterID,
		RequestType:         "CREATE_CLUSTER",
		Status:              status,
		ProgressPercent:     30,
		ProgressDescription: "Not enough capacity",
	}
}

// testCreateServer answers the calls that Create makes before the wait. The
// first GET of the request returns QUEUED, the next ones return final.
func testCreateServer(t *testing.T, final string) (*scyllatest.Server, *scylla.Client) {
	t.Helper()
	srv := scyllatest.New(t)
	srv.Reply(http.MethodGet, "/deployment/cloud-provider/1/region/2", model.CloudProviderInstances{
		Instances: []model.CloudProviderInstance{{ID: 4, ExternalID: "i4i.large"}},
	})
	srv.Reply(http.MethodPost, "/account/1/cluster", map[string]any{"requestId": testRequestID})
	srv.Sequence(http.MethodGet, "/account/1/cluster/request/9",
		scyllatest.Response{Data: testCreateRequest("QUEUED")},
		scyllatest.Response{Data: testCreateRequest(final)},
	)

	c := srv.Client(t)
	c.Meta = &scylla.Cloudmeta{
		CloudProviders: []scylla.CloudProvider{{
			CloudProvider: &model.CloudProvider{ID: 1, Name: "AWS"},
			CloudProviderRegions: &model.CloudProviderRegions{
				Regions: []model.CloudProviderRegion{{ID: 2, ExternalID: "us-east-1"}},
			},
		}},
		ScyllaVersions: &model.ScyllaVersions{DefaultScyllaVersionID: 5},
	}
	return srv, c
}

func testClusterData(t *testing.T) *schema.ResourceData {
	t.Helper()
	return schema.TestResourceDataRaw(t, ResourceCluster().Schema, map[string]any{
		"name":      "test",
		"cloud":     "AWS",
		"region":    "us-east-1",
		"node_type": "i4i.large",
		"min_nodes": 3,
	})
}

// A cluster whose create fails still runs and costs money, so Create must
// keep its ID in state.
func TestCreateKeepsIDWhenWaitFails(t *testing.T) {
	t.Parallel()
	_, c := testCreateServer(t, "FAILED")
	d := testClusterData(t)

	diags := resourceClusterCreate(context.Background(), d, c)

	require.True(t, diags.HasError())
	require.Contains(t, diagSummaries(diags), "cluster request ID 9 failed (CREATE_CLUSTER at 30%): Not enough capacity")
	require.Equal(t, "3", d.Id())
	require.Equal(t, testRequestID, d.Get("request_id"))
}

func TestReadFailedCreate(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		listed  string // status of the create request in the list
		waitErr bool   // the wait fails with an API error, not with FAILED
	}{
		{name: "failed before read", listed: "FAILED"},
		{name: "fails during the wait", listed: "IN_PROGRESS"},
		{name: "wait fails for another reason", listed: "IN_PROGRESS", waitErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := scyllatest.New(t)
			srv.Reply(http.MethodGet, "/account/1/cluster/3/request", []model.ClusterRequest{testCreateRequest(tt.listed)})
			if tt.waitErr {
				srv.Fail(http.MethodGet, "/account/1/cluster/request/9", http.StatusInternalServerError, "000000")
			} else {
				srv.Reply(http.MethodGet, "/account/1/cluster/request/9", testCreateRequest("FAILED"))
			}
			d := testClusterData(t)
			d.SetId("3")

			diags := resourceClusterRead(context.Background(), d, srv.Client(t))

			if tt.waitErr {
				require.True(t, diags.HasError(), "an unclear wait error must stay an error")
				require.NotEqual(t, failedCreateStatus, d.Get("status"))
				return
			}
			require.False(t, diags.HasError(), "%v", diags)
			require.Len(t, diags, 1)
			require.Equal(t, diag.Warning, diags[0].Severity)
			require.Equal(t, "3", d.Id())
			require.Equal(t, failedCreateStatus, d.Get("status"))
			require.Equal(t, testRequestID, d.Get("request_id"))
			require.Empty(t, srv.CallsTo(http.MethodGet, "/account/1/cluster/3"), "Read must not read a cluster whose create failed")
		})
	}
}

func diagSummaries(diags diag.Diagnostics) string {
	var s string
	for _, d := range diags {
		s += d.Summary + "\n"
	}
	return s
}
