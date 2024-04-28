package tracing

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type AzureSDKStatistics struct {
	AzureApiDuration  *prometheus.HistogramVec
	AzureApiRateLimit *prometheus.GaugeVec
	Transport         http.RoundTripper
}

var subscriptionRegexp = regexp.MustCompile(`^(?i)/subscriptions/([^/]+)/?.*$`)

func New(registry prometheus.Registerer, transport http.RoundTripper) *AzureSDKStatistics {
	stats := &AzureSDKStatistics{}
	stats.AzureApiDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "azurerm_api_http_request_duration_seconds",
			Help:    "A histogram of request latencies.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "code"},
	)

	registry.MustRegister(stats.AzureApiDuration)

	stats.AzureApiRateLimit = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "azurerm_api_ratelimit",
			Help: "AzureRM API ratelimit",
		},
		[]string{
			"endpoint",
			"subscriptionID",
			"scope",
			"type",
		},
	)

	registry.MustRegister(stats.AzureApiRateLimit)

	stats.Transport = stats.scrapeRateLimits(promhttp.InstrumentRoundTripperDuration(stats.AzureApiDuration, transport))
	return stats
}

func (s *AzureSDKStatistics) scrapeRateLimits(next http.RoundTripper) promhttp.RoundTripperFunc {
	return func(req *http.Request) (*http.Response, error) {
		resp, err := next.RoundTrip(req)

		if strings.HasSuffix(req.Host, "metrics.monitor.azure.com") {
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)

			_ = body

			return nil, err
		}

		if err != nil {
			return resp, err
		}

		// get hostname (shorten it to 3 parts)
		hostname := strings.ToLower(req.Host)
		if hostnameParts := strings.Split(hostname, "."); len(hostnameParts) > 3 {
			hostname = strings.Join(hostnameParts[len(hostnameParts)-3:], ".")
		}
		subscriptionId := ""
		if matches := subscriptionRegexp.FindStringSubmatch(req.URL.RawPath); len(matches) >= 2 {
			subscriptionId = strings.ToLower(matches[1])
		}

		if strings.HasPrefix(req.URL.RawPath, "/providers/microsoft.resourcegraph/") {
			s.collectAzureApiRateLimitMetric(resp, hostname, subscriptionId, "x-ms-user-quota-remaining", "resourcegraph", "quota")
		}

		s.collectAzureApiRateLimitMetric(resp, hostname, subscriptionId, "x-ms-ratelimit-remaining-microsoft.consumption-tenant-requests", "consumption", "tenant-requests")

		// subscription rate limits
		s.collectAzureApiRateLimitMetric(resp, hostname, subscriptionId, "x-ms-ratelimit-remaining-subscription-reads", "subscription", "reads")
		s.collectAzureApiRateLimitMetric(resp, hostname, subscriptionId, "x-ms-ratelimit-remaining-subscription-writes", "subscription", "writes")
		s.collectAzureApiRateLimitMetric(resp, hostname, subscriptionId, "x-ms-ratelimit-remaining-subscription-resource-requests", "subscription", "resourceRequests")
		s.collectAzureApiRateLimitMetric(resp, hostname, subscriptionId, "x-ms-ratelimit-remaining-subscription-resource-entities-read", "subscription", "resource-entities-read")

		// tenant rate limits
		s.collectAzureApiRateLimitMetric(resp, hostname, subscriptionId, "x-ms-ratelimit-remaining-tenant-reads", "tenant", "reads")
		s.collectAzureApiRateLimitMetric(resp, hostname, subscriptionId, "x-ms-ratelimit-remaining-tenant-writes", "tenant", "writes")
		s.collectAzureApiRateLimitMetric(resp, hostname, subscriptionId, "x-ms-ratelimit-remaining-tenant-resource-requests", "tenant", "resource-requests")
		s.collectAzureApiRateLimitMetric(resp, hostname, subscriptionId, "x-ms-ratelimit-remaining-tenant-resource-entities-read", "tenant", "resource-entities-read")

		return resp, nil
	}
}

func (s *AzureSDKStatistics) collectAzureApiRateLimitMetric(r *http.Response, hostname, subscriptionId, headerName, scopeLabel, typeLabel string) {
	headerValue := r.Header.Get(headerName)

	if v, err := strconv.ParseInt(headerValue, 10, 64); err == nil {
		// single value
		s.AzureApiRateLimit.With(prometheus.Labels{
			"endpoint":       hostname,
			"subscriptionID": subscriptionId,
			"scope":          scopeLabel,
			"type":           typeLabel,
		}).Set(float64(v))
	} else if strings.Contains(headerValue, ":") {
		// multi value (comma separated eg "QueriesPerHour:496,QueriesPerMin:37,QueriesPer10Sec:11")
		for _, val := range strings.Split(headerValue, ",") {
			if parts := strings.SplitN(val, ":", 2); len(parts) == 2 {
				quotaName := parts[0]
				quotaValue := parts[1]
				if v, err := strconv.ParseInt(quotaValue, 10, 64); err == nil {
					s.AzureApiRateLimit.With(prometheus.Labels{
						"endpoint":       hostname,
						"subscriptionID": subscriptionId,
						"scope":          scopeLabel,
						"type":           fmt.Sprintf("%s.%s", typeLabel, quotaName),
					}).Set(float64(v))
				}
			}
		}
	}
}
