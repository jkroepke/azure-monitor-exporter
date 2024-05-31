package probe_test

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/monitor/query/azmetrics"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resourcegraph/armresourcegraph"
	"github.com/go-kit/log"
	"github.com/jkroepke/azure-monitor-exporter/pkg/cache"
	"github.com/jkroepke/azure-monitor-exporter/pkg/probe"
	"github.com/jkroepke/azure-monitor-exporter/pkg/testutil"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/expfmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProbe(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name                       string
		subscriptions              []string
		request                    *http.Request
		resourceGraphQueryResponse armresourcegraph.QueryResponse
		metricResults              azmetrics.MetricResults
		expectedMetrics            []string
	}{
		{
			name:          "simple probe",
			subscriptions: make([]string, 0),
			request: &http.Request{
				URL: &url.URL{
					RawQuery: "resourceType=Microsoft.Compute/virtualMachines&metricName=VmAvailabilityMetric&query=Resources",
				},
			},
			resourceGraphQueryResponse: armresourcegraph.QueryResponse{
				Count:           to.Ptr(int64(1)),
				TotalRecords:    to.Ptr(int64(1)),
				ResultTruncated: to.Ptr(armresourcegraph.ResultTruncated("false")),
				Data: []any{
					map[string]any{
						"id":             "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg-mock/providers/Microsoft.Compute/virtualMachines/vm1",
						"location":       "westeurope",
						"subscriptionId": "00000000-0000-0000-0000-000000000000",
					},
				},
			},
			metricResults: azmetrics.MetricResults{
				Values: []azmetrics.MetricData{
					{
						EndTime:        to.Ptr("2024-01-01T00:00:00Z"),
						Interval:       to.Ptr("PT5M"),
						Namespace:      to.Ptr("microsoft.compute/virtualmachines"),
						ResourceID:     to.Ptr("/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg-mock/providers/Microsoft.Compute/virtualMachines/vm1"),
						ResourceRegion: to.Ptr("westeurope"),
						StartTime:      to.Ptr("2024-01-01T01:00:00Z"),
						Values: []azmetrics.Metric{
							{
								ID: to.Ptr("/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg-mock/providers/Microsoft.Compute/virtualMachines/vm1/providers/Microsoft.Insights/metrics/VmAvailabilityMetric"),
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
					},
				},
			},
			expectedMetrics: []string{
				`azure_monitor_microsoft_compute_virtualmachines_vmavailabilitymetric_average_count{region="westeurope",resourceID="/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg-mock/providers/Microsoft.Compute/virtualMachines/vm1",subscription_id="00000000-0000-0000-0000-000000000000"} 1`,
			},
		},
		{
			name:          "lager probe",
			subscriptions: make([]string, 0),
			request: &http.Request{
				URL: &url.URL{
					RawQuery: "resourceType=Microsoft.Compute/virtualMachines&metricName=VmAvailabilityMetric&query=Resources",
				},
			},
			resourceGraphQueryResponse: func() armresourcegraph.QueryResponse {
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
			}(),
			metricResults: func() azmetrics.MetricResults {
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
			}(),
			expectedMetrics: []string{
				`azure_monitor_microsoft_compute_virtualmachines_vmavailabilitymetric_average_count{region="westeurope",resourceID="/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg-mock/providers/Microsoft.Compute/virtualMachines/vm1",subscription_id="00000000-0000-0000-0000-000000000000"} 1`,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			httpClient := &http.Client{
				Transport: testutil.MockTransport(http.DefaultTransport, tc.resourceGraphQueryResponse, tc.metricResults),
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
			require.NoError(t, err)

			probeHandler, err := probe.New(log.NewNopLogger(), httpClient, tc.request, cred, tc.subscriptions, cache.NewCache[probe.Resources]())
			require.NoError(t, err)

			reg := prometheus.NewRegistry()
			reg.MustRegister(probeHandler)

			metrics, err := reg.Gather()
			require.NoError(t, err)

			sb := &strings.Builder{}
			for _, metric := range metrics {
				_, err = expfmt.MetricFamilyToText(sb, metric)
				require.NoError(t, err)
			}

			require.NoError(t, err)

			metricsText := sb.String()

			assert.Contains(t, metricsText, "azure_monitor_scrape_collector_success 1")

			for _, expectedMetric := range tc.expectedMetrics {
				assert.Contains(t, metricsText, expectedMetric)
			}
		})
	}
}
