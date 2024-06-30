package probe

import (
	"net/http"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/monitor/query/azmetrics"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resourcegraph/armresourcegraph"
	"github.com/go-kit/log"
	"github.com/jkroepke/azure-monitor-exporter/pkg/cache"
	"github.com/prometheus/client_golang/prometheus"
)

type Resources struct {
	Resources        map[string]map[string][]string
	AdditionalLabels map[string]map[string]string
}

type Probe struct {
	request *http.Request
	logger  log.Logger
	cred    azcore.TokenCredential

	resourceGraphClient  *armresourcegraph.Client
	metricsClientOptions *azmetrics.ClientOptions
	metricsClients       map[string]*azmetrics.Client
	metricsClientMu      *sync.Mutex

	subscriptions []string
	config        *Config

	queryCache *cache.Cache[Resources]

	scrapeDurationDesc *prometheus.Desc
	scrapeSuccessDesc  *prometheus.Desc
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
