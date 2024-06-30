package probe

import (
	"fmt"
	stdlog "log"
	"math"
	"net/http"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/monitor/query/azmetrics"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resourcegraph/armresourcegraph"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/jkroepke/azure-monitor-exporter/pkg/cache"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func New(
	logger log.Logger,
	httpClient *http.Client,
	cred azcore.TokenCredential,
	subscriptions []string,
	queryCache *cache.Cache[Resources],
	metricsClientCache *cache.Cache[azmetrics.Client],
) (*Probe, error) {
	clientOptions := azcore.ClientOptions{
		Transport: httpClient,
	}

	resourceGraphClient, err := armresourcegraph.NewClient(cred, &arm.ClientOptions{
		ClientOptions: clientOptions,
	})
	if err != nil {
		return nil, fmt.Errorf("error creating resource graph client: %w", err)
	}

	probe := &Probe{
		logger: logger,

		resourceGraphClient: resourceGraphClient,
		azClientOptions:     clientOptions,

		subscriptions:      subscriptions,
		queryCache:         queryCache,
		metricsClientCache: metricsClientCache,

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

	return probe, nil
}

func (p *Probe) getMetricsClient(location string) (*azmetrics.Client, error) {
	if client, ok := p.metricsClientCache.Get(location); ok {
		return client, nil
	}

	metricsEndpoint := fmt.Sprintf("https://%s.metrics.monitor.azure.com", location)

	client, err := azmetrics.NewClient(metricsEndpoint, p.cred, &azmetrics.ClientOptions{
		ClientOptions: p.azClientOptions,
	})
	if err != nil {
		return nil, fmt.Errorf("error creating metrics client: %w", err)
	}

	p.metricsClientCache.Set(location, client, math.MaxInt64)

	return client, nil
}

func (p *Probe) ServeHTTP(reg prometheus.Registerer) http.HandlerFunc {
	return func(w http.ResponseWriter, request *http.Request) {
		config, err := GetConfigFromRequest(request)
		if err != nil {
			_ = level.Error(p.logger).Log("msg", "error parsing request", "err", err)
			http.Error(w, err.Error(), http.StatusBadRequest)

			return
		}

		logger := log.With(p.logger,
			"client", request.RemoteAddr,
			"query", request.URL.RawQuery,
			"resource_type", config.ResourceType,
			"metric_namespace", config.MetricNamespace,
			"metric_names", config.MetricNames,
		)

		probeRequest := &Request{
			config:  config,
			probe:   p,
			Request: *request,
			Logger:  logger,
		}

		registry := prometheus.NewRegistry()
		registry.MustRegister(probeRequest)

		promhttp.HandlerFor(registry, promhttp.HandlerOpts{
			Registry: reg,
			ErrorLog: stdlog.New(log.NewStdlibAdapter(p.logger), "ERROR: ", stdlog.LstdFlags),
		}).ServeHTTP(w, request)
	}
}
