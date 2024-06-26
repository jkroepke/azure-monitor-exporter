package probe

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/monitor/query/azmetrics"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resourcegraph/armresourcegraph"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/exp/maps"
)

func (r *Request) Describe(_ chan<- *prometheus.Desc) {
	// Return no descriptors to turn the collector into an unchecked collector.
}

func (r *Request) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithDeadline(r.Context(), time.Now().Add(r.getProbeTimeout()))
	defer cancel()

	startTime := time.Now()

	azureResources, err := r.getResources(ctx)

	ch <- prometheus.MustNewConstMetric(r.probe.scrapeDurationDesc, prometheus.GaugeValue, time.Since(startTime).Seconds(), "query_resources")

	if err != nil {
		ch <- prometheus.NewInvalidMetric(prometheus.NewInvalidDesc(err), err)
		ch <- prometheus.MustNewConstMetric(r.probe.scrapeSuccessDesc, prometheus.GaugeValue, 0)

		_ = level.Error(r).Log("msg", "Error querying resources", "err", err)

		return
	}

	startTime = time.Now()
	err = r.fetchMetrics(ctx, azureResources, ch)

	ch <- prometheus.MustNewConstMetric(r.probe.scrapeDurationDesc, prometheus.GaugeValue, time.Since(startTime).Seconds(), "fetch_metrics")

	if err != nil {
		ch <- prometheus.NewInvalidMetric(prometheus.NewInvalidDesc(err), err)
		ch <- prometheus.MustNewConstMetric(r.probe.scrapeSuccessDesc, prometheus.GaugeValue, 0)

		_ = level.Error(r).Log("msg", "Error fetching metrics", "err", err)

		return
	}

	ch <- prometheus.MustNewConstMetric(r.probe.scrapeSuccessDesc, prometheus.GaugeValue, 1)
}

// getResources is a method of the Probe structure. It retrieves resource information from a cache or by querying resources if not found in the cache.
// It takes a context as an argument and returns a Resources structure and an error.
// The function first checks the cache using a key generated from the configuration query and the subscriptions of the probe.
// If the resource information is not found in the cache, it calls the queryResources method to retrieve the resource information.
// After retrieving the resource information, it is stored in the cache before being returned.
// The function's behavior depends on the implementation of the queryResources method and the configuration of the cache.
func (r *Request) getResources(ctx context.Context) (*Resources, error) {
	if r.config.QueryCacheCacheExpiration == 0 {
		return r.queryResources(ctx)
	}

	subscriptions := r.probe.subscriptions
	if r.config.Subscriptions != nil {
		subscriptions = r.config.Subscriptions
	}

	cacheKey := fmt.Sprintf("%s-%s-%s", r.config.Query, r.config.ResourceType, strings.Join(subscriptions, ","))
	hash := sha256.Sum256([]byte(cacheKey))
	cacheKey = hex.EncodeToString(hash[:])

	resources, ok := r.probe.queryCache.Get(cacheKey)
	if ok {
		return resources, nil
	}

	resources, err := r.queryResources(ctx)
	if err != nil {
		return nil, err
	}

	r.probe.queryCache.Set(cacheKey, resources, r.config.QueryCacheCacheExpiration)

	return resources, nil
}

// queryResources queries the Azure Resource Graph API for resources.
//
//nolint:gocognit,cyclop
func (r *Request) queryResources(ctx context.Context) (*Resources, error) {
	var (
		err       error
		skipToken string
		response  armresourcegraph.ClientResourcesResponse
	)

	resources := Resources{
		Resources:        make(map[string]map[string][]string),
		AdditionalLabels: make(map[string]map[string]string),
	}

	subscriptions := r.probe.subscriptions
	if r.config.Subscriptions != nil {
		subscriptions = r.config.Subscriptions
	}

	for {
		query := fmt.Sprintf("%s\n| where type == '%s' \n| project-keep id, subscriptionId, location, label_*",
			r.config.Query, strings.ToLower(r.config.ResourceType),
		)

		response, err = r.probe.resourceGraphClient.Resources(ctx, armresourcegraph.QueryRequest{
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

		if response.ResultTruncated == nil || response.Data == nil || response.Count == nil {
			return nil, errors.New("error querying resource graph: unexpected response")
		}

		if *response.ResultTruncated == armresourcegraph.ResultTruncatedTrue {
			_ = level.Warn(r).Log("msg", "Result truncated", "query", query)
		}

		if *response.Count == 0 {
			return nil, errors.New("error querying resource graph: no rows returned")
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
				return nil, fmt.Errorf("error querying resource graph: missing field %s. Available fields: %v", field, maps.Keys(row))
			}
		}

		var (
			resultRow      map[string]any
			subscriptionID string
			location       string
			labelValue     string
			resourceID     string
		)

		for _, row := range rows {
			resultRow, ok = row.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("error querying resource graph: unexpected row type: %+v", row)
			}

			subscriptionID, ok = resultRow["subscriptionId"].(string)
			if !ok {
				return nil, fmt.Errorf("error querying resource graph: unexpected subscriptionId type: %+v", rows[0])
			}

			location, ok = resultRow["location"].(string)
			if !ok {
				return nil, fmt.Errorf("error querying resource graph: unexpected location type: %+v", rows[0])
			}

			resourceID, ok = resultRow["id"].(string)
			if !ok {
				return nil, fmt.Errorf("error querying resource graph: unexpected id type: %+v", rows[0])
			}

			if _, ok = resources.Resources[location]; !ok {
				resources.Resources[location] = make(map[string][]string, len(subscriptions))
			}

			if _, ok = resources.Resources[location][subscriptionID]; !ok {
				resources.Resources[location][subscriptionID] = make([]string, 0, len(rows))
			}

			if len(resultRow)-3 > 0 {
				resources.AdditionalLabels[resourceID] = make(map[string]string, len(resultRow)-3)

				for key, value := range resultRow {
					if strings.HasPrefix(key, "label_") {
						labelValue, ok = value.(string)
						if !ok {
							return nil, fmt.Errorf("error querying resource graph: unexpected id type: %+v", rows[0])
						}

						resources.AdditionalLabels[resourceID][key[6:]] = labelValue
					}
				}
			}

			resources.Resources[location][subscriptionID] = append(
				resources.Resources[location][subscriptionID],
				resourceID,
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
func (r *Request) fetchMetrics(ctx context.Context, resources *Resources, ch chan<- prometheus.Metric) error {
	var (
		client *azmetrics.Client
		err    error
		resp   azmetrics.QueryResourcesResponse
	)

	if resources == nil {
		return errors.New("resources is nil")
	}

	for location, subscriptions := range resources.Resources {
		client, err = r.probe.getMetricsClient(location)
		if err != nil {
			return fmt.Errorf("error get metrics client: %w", err)
		}

		for subscriptionID, resourceIDs := range subscriptions {
			for {
				maxResourceIDs := 50
				if len(resourceIDs) < maxResourceIDs {
					maxResourceIDs = len(resourceIDs)
				}

				requestResourceIDs := resourceIDs[:maxResourceIDs]
				resourceIDs = resourceIDs[maxResourceIDs:]

				metricNamespace := r.config.ResourceType
				if r.config.MetricNamespace != "" {
					metricNamespace = r.config.MetricNamespace
				}

				resp, err = client.QueryResources(
					ctx,
					subscriptionID,
					metricNamespace,
					r.config.MetricNames,
					azmetrics.ResourceIDList{ResourceIDs: requestResourceIDs},
					&r.config.QueryResourcesOptions,
				)
				if err != nil {
					var azErr *azcore.ResponseError
					if errors.As(err, &azErr) {
						return fmt.Errorf("error querying metrics: %w", azErr)
					}

					return fmt.Errorf("error querying metrics: %w", err)
				}

				var (
					latestTimestamp time.Time
					latestMetric    map[string]*float64
				)

				for _, metric := range resp.Values {
					prometheusMetricNamespace := "azure_monitor_" + strings.ReplaceAll(strings.ReplaceAll(strings.ToLower(*metric.Namespace), ".", "_"), "/", "_")

					prometheusLabels := map[string]string{
						"subscription_id": subscriptionID,
						"region":          *metric.ResourceRegion,
						"instance":        *metric.ResourceID,
					}

					for labelKey, labelValue := range resources.AdditionalLabels[*metric.ResourceID] {
						prometheusLabels[labelKey] = labelValue
					}

					latestTimestamp = time.Time{}
					latestMetric = map[string]*float64{
						"total":   nil,
						"average": nil,
						"count":   nil,
						"minimum": nil,
						"maximum": nil,
					}

					for _, metricValue := range metric.Values {
						for _, metricTimeSeries := range metricValue.TimeSeries {
							if len(metricTimeSeries.Data) == 0 {
								continue
							}

							for _, label := range metricTimeSeries.MetadataValues {
								prometheusLabels[*label.Name.Value] = *label.Value
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
						}

						for metricType, value := range latestMetric {
							if value == nil {
								continue
							}

							ch <- prometheus.MustNewConstMetric(
								prometheus.NewDesc(
									prometheus.BuildFQName(
										prometheusMetricNamespace,
										strings.ReplaceAll(strings.ToLower(*metricValue.Name.Value), " ", ""),
										fmt.Sprintf("%s_%s",
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

				if len(resourceIDs) == 0 {
					break
				}
			}
		}
	}

	return nil
}
