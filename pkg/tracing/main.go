package tracing

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type AzureSDKStatistics struct {
	AzureAPIDuration  *prometheus.HistogramVec
	AzureAPIRateLimit *prometheus.GaugeVec
	Transport         http.RoundTripper
}

var subscriptionRegexp = regexp.MustCompile(`^(?i)/subscriptions/([^/]+)/?.*$`)

func New(registry prometheus.Registerer, transport http.RoundTripper) *AzureSDKStatistics {
	stats := &AzureSDKStatistics{}
	stats.AzureAPIDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "azurerm_api_http_request_duration_seconds",
			Help:    "A histogram of request latencies.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "code"},
	)

	registry.MustRegister(stats.AzureAPIDuration)

	stats.AzureAPIRateLimit = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "azurerm_api_ratelimit",
			Help: "AzureRM API ratelimit",
		},
		[]string{
			"endpoint",
			"subscription_id",
			"scope",
			"type",
		},
	)

	registry.MustRegister(stats.AzureAPIRateLimit)

	stats.Transport = stats.scrapeRateLimits(promhttp.InstrumentRoundTripperDuration(stats.AzureAPIDuration, transport))

	return stats
}

func (s *AzureSDKStatistics) scrapeRateLimits(next http.RoundTripper) promhttp.RoundTripperFunc {
	return func(req *http.Request) (*http.Response, error) {
		resp, err := next.RoundTrip(req)
		if err != nil {
			return resp, err //nolint:wrapcheck
		}

		// get hostname (shorten it to 3 parts)
		hostname := strings.ToLower(req.Host)
		if hostnameParts := strings.Split(hostname, "."); len(hostnameParts) > 3 {
			hostname = strings.Join(hostnameParts[len(hostnameParts)-3:], ".")
		}

		subscriptionID := ""
		if matches := subscriptionRegexp.FindStringSubmatch(req.URL.RawPath); len(matches) >= 2 {
			subscriptionID = strings.ToLower(matches[1])
		}

		if strings.HasPrefix(req.URL.RawPath, "/providers/microsoft.resourcegraph/") {
			s.collectAzureAPIRateLimitMetric(resp, hostname, subscriptionID,
				"x-ms-user-quota-remaining", "resourcegraph", "quota")
		}

		s.collectAzureAPIRateLimitMetric(resp, hostname, subscriptionID,
			"x-ms-ratelimit-remaining-microsoft.consumption-tenant-requests", "consumption", "tenant-requests")

		// subscription rate limits
		s.collectAzureAPIRateLimitMetric(resp, hostname, subscriptionID,
			"x-ms-ratelimit-remaining-subscription-reads", "subscription", "reads")
		s.collectAzureAPIRateLimitMetric(resp, hostname, subscriptionID,
			"x-ms-ratelimit-remaining-subscription-writes", "subscription", "writes")
		s.collectAzureAPIRateLimitMetric(resp, hostname, subscriptionID,
			"x-ms-ratelimit-remaining-subscription-resource-requests", "subscription", "resourceRequests")
		s.collectAzureAPIRateLimitMetric(resp, hostname, subscriptionID,
			"x-ms-ratelimit-remaining-subscription-resource-entities-read", "subscription", "resource-entities-read")

		// tenant rate limits
		s.collectAzureAPIRateLimitMetric(resp, hostname, subscriptionID,
			"x-ms-ratelimit-remaining-tenant-reads", "tenant", "reads")
		s.collectAzureAPIRateLimitMetric(resp, hostname, subscriptionID,
			"x-ms-ratelimit-remaining-tenant-writes", "tenant", "writes")
		s.collectAzureAPIRateLimitMetric(resp, hostname, subscriptionID,
			"x-ms-ratelimit-remaining-tenant-resource-requests", "tenant", "resource-requests")
		s.collectAzureAPIRateLimitMetric(resp, hostname, subscriptionID,
			"x-ms-ratelimit-remaining-tenant-resource-entities-read", "tenant", "resource-entities-read")

		return resp, nil
	}
}

func (s *AzureSDKStatistics) collectAzureAPIRateLimitMetric(r *http.Response, hostname, subscription_id, headerName, scopeLabel, typeLabel string) {
	headerValue := r.Header.Get(headerName)

	if value, err := strconv.ParseInt(headerValue, 10, 64); err == nil {
		// single value
		s.AzureAPIRateLimit.With(prometheus.Labels{
			"endpoint":        hostname,
			"subscription_id": subscription_id,
			"scope":           scopeLabel,
			"type":            typeLabel,
		}).Set(float64(value))
	} else if strings.Contains(headerValue, ":") {
		// multi value (comma separated eg "QueriesPerHour:496,QueriesPerMin:37,QueriesPer10Sec:11")
		for _, val := range strings.Split(headerValue, ",") {
			if parts := strings.SplitN(val, ":", 2); len(parts) == 2 {
				quotaName := parts[0]
				quotaValue := parts[1]

				if value, err = strconv.ParseInt(quotaValue, 10, 64); err == nil {
					s.AzureAPIRateLimit.With(prometheus.Labels{
						"endpoint":        hostname,
						"subscription_id": subscription_id,
						"scope":           scopeLabel,
						"type":            fmt.Sprintf("%s.%s", typeLabel, quotaName),
					}).Set(float64(value))
				}
			}
		}
	}
}
