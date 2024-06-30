package probe

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/monitor/query/azmetrics"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resourcegraph/armresourcegraph"
	"github.com/go-kit/log"
	"github.com/jkroepke/azure-monitor-exporter/pkg/cache"
	"github.com/jkroepke/azure-monitor-exporter/pkg/testutil"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func BenchmarkFull(b *testing.B) {
	b.StopTimer()

	subscriptions := make([]string, 0)
	requestURL := "/probe?resourceType=Microsoft.Compute/virtualMachines&metricName=VmAvailabilityMetric&query=Resources"
	resourceGraphQueryResponse := func() armresourcegraph.QueryResponse {
		data := make([]map[string]any, 50)

		for i := range 50 {
			data[i] = map[string]any{
				"id":             fmt.Sprintf("/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg-mock/providers/Microsoft.Compute/virtualMachines/vm%d", i),
				"location":       "westeurope",
				"subscriptionId": "00000000-0000-0000-0000-000000000000",
			}
		}

		return armresourcegraph.QueryResponse{
			Count:           to.Ptr(int64(50)),
			TotalRecords:    to.Ptr(int64(50)),
			ResultTruncated: to.Ptr(armresourcegraph.ResultTruncated("false")),
			Data:            data,
		}
	}()

	metricResults := func() azmetrics.MetricResults {
		values := make([]azmetrics.MetricData, 50)

		for i := range 50 {
			values[i] = azmetrics.MetricData{
				EndTime:        to.Ptr("2024-01-01T00:00:00Z"),
				Interval:       to.Ptr("PT5M"),
				Namespace:      to.Ptr("microsoft.compute/virtualmachines"),
				ResourceID:     to.Ptr(fmt.Sprintf("/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg-mock/providers/Microsoft.Compute/virtualMachines/vm%d", i)),
				ResourceRegion: to.Ptr("westeurope"),
				StartTime:      to.Ptr("2024-01-01T01:00:00Z"),
				Values: []azmetrics.Metric{
					{
						ID: to.Ptr(fmt.Sprintf("/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg-mock/providers/Microsoft.Compute/virtualMachines/vm%d/providers/Microsoft.Insights/metrics/VmAvailabilityMetric", i)),
						Name: &azmetrics.LocalizableString{
							Value:          to.Ptr("VmAvailabilityMetric"),
							LocalizedValue: to.Ptr("VM Availability Metric (Preview)"),
						},
						DisplayDescription: to.Ptr("Measure of Availability of Virtual machines over time."),
						Unit:               to.Ptr(azmetrics.MetricUnitCount),
						TimeSeries: []azmetrics.TimeSeriesElement{
							{
								MetadataValues: []azmetrics.MetadataValue{},
								Data: []azmetrics.MetricValue{
									{
										TimeStamp: to.Ptr(time.Date(2024, 1, 1, 0, 30, 0, 0, time.UTC)),
										Average:   to.Ptr(1.0),
									},
								},
							},
						},
					},
				},
			}
		}

		return azmetrics.MetricResults{
			Values: values,
		}
	}()

	httpClient := &http.Client{
		Transport: testutil.MockTransport(http.DefaultTransport, resourceGraphQueryResponse, metricResults),
	}
	cred, err := azidentity.NewClientSecretCredential(
		"mock",
		"00000000-0000-0000-0000-000000000000",
		"invalid",
		&azidentity.ClientSecretCredentialOptions{
			DisableInstanceDiscovery: true,
			ClientOptions: azcore.ClientOptions{
				Transport: httpClient,
			},
		},
	)
	require.NoError(b, err)

	b.ResetTimer()
	b.StartTimer()

	for range b.N {
		probeHandler, err := New(log.NewNopLogger(), httpClient, cred, subscriptions,
			cache.NewCache[Resources](), cache.NewCache[azmetrics.Client]())
		require.NoError(b, err)

		request := httptest.NewRequest(http.MethodGet, requestURL, nil)
		recorder := httptest.NewRecorder()

		probeHandler.ServeHTTP(prometheus.NewRegistry())(recorder, request)

		require.Equal(b, http.StatusOK, recorder.Code)
	}

	b.StopTimer()
	b.ReportAllocs()
}
