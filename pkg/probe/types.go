package probe

import (
	"net/http"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/monitor/query/azmetrics"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resourcegraph/armresourcegraph"
	"github.com/go-kit/log"
	"github.com/jkroepke/azure-monitor-exporter/pkg/cache"
	"github.com/prometheus/client_golang/prometheus"
)

type Probe struct {
	logger log.Logger
	cred   azcore.TokenCredential

	subscriptions []string

	resourceGraphClient *armresourcegraph.Client
	azClientOptions     azcore.ClientOptions

	queryCache         *cache.Cache[Resources]
	metricsClientCache *cache.Cache[azmetrics.Client]

	scrapeDurationDesc *prometheus.Desc
	scrapeSuccessDesc  *prometheus.Desc
}

type Request struct {
	http.Request
	log.Logger

	config *Config
	probe  *Probe
}

type Resources struct {
	Resources        map[string]map[string][]string
	AdditionalLabels map[string]map[string]string
}

type Config struct {
	Subscriptions   []string
	ResourceType    string
	Query           string
	MetricNamespace string
	MetricNames     []string
	MetricPrefix    string

	QueryCacheCacheExpiration time.Duration

	azmetrics.QueryResourcesOptions
}
