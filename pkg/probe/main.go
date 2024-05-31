package probe

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/monitor/query/azmetrics"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resourcegraph/armresourcegraph"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/jkroepke/azure-monitor-exporter/pkg/cache"
	"github.com/prometheus/client_golang/prometheus"
)

func New(
	logger log.Logger,
	httpClient *http.Client,
	request *http.Request,
	cred azcore.TokenCredential,
	subscriptions []string,
	queryCache *cache.Cache[Resources],
) (*Probe, error) {
	probe := &Probe{
		request:    request,
		logger:     logger,
		cred:       cred,
		httpClient: httpClient,

		subscriptions: subscriptions,
		queryCache:    queryCache,

		scrapeDurationDesc: prometheus.NewDesc(
			prometheus.BuildFQName("azure_monitor", "scrape", "collector_duration_seconds"),
			"azure_monitor_exporter: Duration of a collector scrape.",
			[]string{"phase"},
			nil,
		),
		scrapeSuccessDesc: prometheus.NewDesc(
			prometheus.BuildFQName("azure_monitor", "scrape", "collector_success"),
			"azure_monitor_exporter: Whether a collector succeeded.",
			[]string{},
			nil,
		),
	}

	config, err := GetConfigFromRequest(request)
	if err != nil {
		return nil, fmt.Errorf("error parsing request: %w", err)
	}

	probe.config = config

	return probe, nil
}

func (p *Probe) Describe(_ chan<- *prometheus.Desc) {
	// Return no descriptors to turn the collector into an unchecked collector.
}

func (p *Probe) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithDeadline(p.request.Context(), time.Now().Add(p.getProbeTimeout()))
	defer cancel()

	startTime := time.Now()

	resources, err := p.getResources(ctx)

	ch <- prometheus.MustNewConstMetric(p.scrapeDurationDesc, prometheus.GaugeValue, time.Since(startTime).Seconds(), "query_resources")

	if err != nil {
		ch <- prometheus.NewInvalidMetric(prometheus.NewInvalidDesc(err), err)
		ch <- prometheus.MustNewConstMetric(p.scrapeSuccessDesc, prometheus.GaugeValue, 0)

		_ = level.Error(p.logger).Log("msg", "Error querying resources", "err", err)

		return
	}

	startTime = time.Now()
	err = p.fetchMetrics(ctx, resources, ch)

	ch <- prometheus.MustNewConstMetric(p.scrapeDurationDesc, prometheus.GaugeValue, time.Since(startTime).Seconds(), "fetch_metrics")

	if err != nil {
		ch <- prometheus.NewInvalidMetric(prometheus.NewInvalidDesc(err), err)
		ch <- prometheus.MustNewConstMetric(p.scrapeSuccessDesc, prometheus.GaugeValue, 0)

		_ = level.Error(p.logger).Log("msg", "Error fetching metrics", "err", err)

		return
	}

	ch <- prometheus.MustNewConstMetric(p.scrapeSuccessDesc, prometheus.GaugeValue, 1)
}

// getResources is a method of the Probe structure. It retrieves resource information from a cache or by querying resources if not found in the cache.
// It takes a context as an argument and returns a Resources structure and an error.
// The function first checks the cache using a key generated from the configuration query and the subscriptions of the probe.
// If the resource information is not found in the cache, it calls the queryResources method to retrieve the resource information.
// After retrieving the resource information, it is stored in the cache before being returned.
// The function's behavior depends on the implementation of the queryResources method and the configuration of the cache.
func (p *Probe) getResources(ctx context.Context) (*Resources, error) {
	if p.config.QueryCacheCacheExpiration == 0 {
		return p.queryResources(ctx)
	}

	subscriptions := p.subscriptions
	if p.config.Subscriptions != nil {
		subscriptions = p.config.Subscriptions
	}

	cacheKey := fmt.Sprintf("%s-%s-%s", p.config.Query, p.config.ResourceType, strings.Join(subscriptions, ","))
	hash := sha256.Sum256([]byte(cacheKey))
	cacheKey = hex.EncodeToString(hash[:])

	resources, ok := p.queryCache.Get(cacheKey)
	if ok {
		return resources, nil
	}

	resources, err := p.queryResources(ctx)
	if err != nil {
		return nil, err
	}

	p.queryCache.Set(cacheKey, resources, p.config.QueryCacheCacheExpiration)

	return resources, nil
}

// queryResources queries the Azure Resource Graph API for resources.
//
//nolint:gocognit,cyclop
func (p *Probe) queryResources(ctx context.Context) (*Resources, error) {
	client, err := armresourcegraph.NewClient(p.cred, &arm.ClientOptions{
		ClientOptions: azcore.ClientOptions{
			Transport: p.httpClient,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("error creating resource graph client: %w", err)
	}

	var (
		skipToken string
		response  armresourcegraph.ClientResourcesResponse
	)

	resources := Resources{}

	subscriptions := p.subscriptions
	if p.config.Subscriptions != nil {
		subscriptions = p.config.Subscriptions
	}

	for {
		query := fmt.Sprintf("%s\n| where type == '%s' \n| project id, subscriptionId, location",
			p.config.Query, strings.ToLower(p.config.ResourceType),
		)

		response, err = client.Resources(ctx, armresourcegraph.QueryRequest{
			Options: &armresourcegraph.QueryRequestOptions{
				ResultFormat: to.Ptr(armresourcegraph.ResultFormatObjectArray),
				SkipToken:    to.Ptr(skipToken),
			},
			Query:         &query,
			Subscriptions: to.SliceOfPtrs(subscriptions...),
		}, nil)
		if err != nil {
			return nil, fmt.Errorf("error querying resource graph '%q': %w", query, err)
		}

		if *response.ResultTruncated != "false" {
			_ = level.Warn(p.logger).Log("msg", "Result truncated", "query", query)
		}

		if *response.Count == 0 {
			break
		}

		rows, ok := response.Data.([]any)
		if !ok {
			return nil, fmt.Errorf("error querying resource graph: unexpected type: %+v", response.Data)
		}

		if len(rows) == 0 {
			return nil, errors.New("error querying resource graph: no rows returned")
		}

		row, ok := rows[0].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("error querying resource graph: unexpected type: %+v", rows[0])
		}

		for _, field := range []string{"subscriptionId", "location", "id"} {
			if _, ok = row[field]; !ok {
				return nil, fmt.Errorf("error querying resource graph: missing field %s", field)
			}
		}

		for _, row := range rows {
			row, ok := row.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("error querying resource graph: unexpected row type: %+v", row)
			}

			subscriptionID, ok := row["subscriptionId"].(string)
			if !ok {
				return nil, fmt.Errorf("error querying resource graph: unexpected subscriptionId type: %+v", rows[0])
			}

			location, ok := row["location"].(string)
			if !ok {
				return nil, fmt.Errorf("error querying resource graph: unexpected location type: %+v", rows[0])
			}

			id, ok := row["id"].(string)
			if !ok {
				return nil, fmt.Errorf("error querying resource graph: unexpected id type: %+v", rows[0])
			}

			if _, ok := resources[location]; !ok {
				resources[location] = make(map[string][]string)
			}

			if _, ok := resources[location][subscriptionID]; !ok {
				resources[location][subscriptionID] = make([]string, 0, len(rows))
			}

			resources[location][subscriptionID] = append(
				resources[location][subscriptionID],
				id,
			)
		}

		if response.SkipToken == nil || *response.SkipToken == "" {
			break
		}

		skipToken = *response.SkipToken
	}

	return &resources, nil
}

// fetchMetrics fetches metrics for the resources.
//
//nolint:gocognit,cyclop
func (p *Probe) fetchMetrics(ctx context.Context, resources *Resources, ch chan<- prometheus.Metric) error {
	var (
		client *azmetrics.Client
		err    error
		resp   azmetrics.QueryResourcesResponse
	)

	if resources == nil {
		return errors.New("resources is nil")
	}

	for locations, subscriptions := range *resources {
		metricsEndpoint := fmt.Sprintf("https://%s.metrics.monitor.azure.com", locations)

		client, err = azmetrics.NewClient(metricsEndpoint, p.cred, &azmetrics.ClientOptions{
			ClientOptions: azcore.ClientOptions{
				Transport: p.httpClient,
			},
		})
		if err != nil {
			return fmt.Errorf("error creating metrics client: %w", err)
		}

		for subscriptionID, resourceIDs := range subscriptions {
			for {
				maxResourceIDs := 50
				if len(resourceIDs) < maxResourceIDs {
					maxResourceIDs = len(resourceIDs)
				}

				requestResourceIDs := resourceIDs[:maxResourceIDs]
				resourceIDs = resourceIDs[maxResourceIDs:]

				metricNamespace := p.config.ResourceType
				if p.config.MetricNamespace != "" {
					metricNamespace = p.config.MetricNamespace
				}

				resp, err = client.QueryResources(
					ctx,
					subscriptionID,
					metricNamespace,
					p.config.MetricNames,
					azmetrics.ResourceIDList{ResourceIDs: requestResourceIDs},
					&p.config.QueryResourcesOptions,
				)
				if err != nil {
					var azErr *azcore.ResponseError
					if errors.As(err, &azErr) {
						return fmt.Errorf("error querying metrics: %w", azErr)
					}

					return fmt.Errorf("error querying metrics: %w", err)
				}

				for _, metric := range resp.Values {
					for _, metricValue := range metric.Values {
						for _, metricTimeSeries := range metricValue.TimeSeries {
							if len(metricTimeSeries.Data) == 0 {
								continue
							}

							prometheusLabels := map[string]string{
								"subscription_id": subscriptionID,
								"region":          *metric.ResourceRegion,
								"instance":        *metric.ResourceID,
							}

							for _, label := range metricTimeSeries.MetadataValues {
								prometheusLabels[*label.Name.Value] = *label.Value
							}

							latestTimestamp := time.Time{}
							latestMetric := map[string]*float64{
								"total":   nil,
								"average": nil,
								"count":   nil,
								"minimum": nil,
								"maximum": nil,
							}

							for _, data := range metricTimeSeries.Data {
								if data.TimeStamp.After(latestTimestamp) {
									latestTimestamp = *data.TimeStamp
									latestMetric["total"] = data.Total
									latestMetric["average"] = data.Average
									latestMetric["count"] = data.Count
									latestMetric["minimum"] = data.Minimum
									latestMetric["maximum"] = data.Maximum
								}
							}

							for metricType, value := range latestMetric {
								if value == nil {
									continue
								}

								ch <- prometheus.MustNewConstMetric(
									prometheus.NewDesc(
										prometheus.BuildFQName(
											"azure_monitor",
											strings.ReplaceAll(strings.ReplaceAll(strings.ToLower(*metric.Namespace), ".", "_"), "/", "_"),
											fmt.Sprintf("%s_%s_%s",
												strings.ToLower(*metricValue.Name.Value),
												metricType,
												strings.ToLower(string(*metricValue.Unit)),
											),
										),
										fmt.Sprintf("%s: %s", *metricValue.Name.LocalizedValue, *metricValue.DisplayDescription),
										nil,
										prometheusLabels,
									),
									prometheus.GaugeValue,
									*value,
								)
							}
						}
					}
				}

				if len(resourceIDs) == 0 {
					break
				}
			}
		}
	}

	return nil
}
