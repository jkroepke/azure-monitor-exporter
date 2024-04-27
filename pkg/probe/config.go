package probe

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
)

//nolint:cyclop
func GetConfigFromRequest(request *http.Request) (*Config, error) {
	query := request.URL.Query()

	probeConfig := &Config{}
	if len(query["subscriptionID"]) != 0 {
		probeConfig.Subscriptions = query["subscriptionID"]
	} else if len(query["subscriptionID[]"]) != 0 {
		probeConfig.Subscriptions = query["subscriptionID[]"]
	}

	probeConfig.ResourceType = query.Get("resourceType")
	if len(query["resourceType"]) != 1 || probeConfig.ResourceType == "" {
		return nil, errors.New("'resourceType' parameter must be specified once")
	}

	switch {
	case len(query["metricName"]) != 0:
		probeConfig.MetricNames = query["metricName"]
	case len(query["metricName[]"]) != 0:
		probeConfig.MetricNames = query["metricName[]"]
	default:
		return nil, errors.New("'metricName' parameter must be specified")
	}

	probeConfig.Query = "Resources"
	if len(query["query"]) == 1 {
		probeConfig.Query = query.Get("query")
	}

	switch {
	case len(query["aggregation"]) == 1:
		probeConfig.Aggregation = to.Ptr(query.Get("aggregation"))
	case len(query["aggregation"]) >= 1:
		probeConfig.Aggregation = to.Ptr(strings.Join(query["aggregation"], ","))
	case len(query["aggregation[]"]) == 1:
		probeConfig.Aggregation = to.Ptr(query.Get("aggregation[]"))
	case len(query["aggregation[]"]) >= 1:
		probeConfig.Aggregation = to.Ptr(strings.Join(query["aggregation[]"], ","))
	}

	if len(query["interval"]) == 1 {
		probeConfig.Interval = to.Ptr(query.Get("interval"))
	} else if len(query["interval"]) > 1 {
		return nil, errors.New("'interval' parameter must be specified once")
	}

	if len(query["filter"]) == 1 {
		probeConfig.Filter = to.Ptr(query.Get("filter"))
	} else if len(query["filter"]) > 1 {
		return nil, errors.New("'filter' parameter must be specified once")
	}

	if len(query["metricPrefix"]) == 1 {
		probeConfig.MetricPrefix = query.Get("metricPrefix")
	} else if len(query["metricPrefix"]) > 1 {
		return nil, errors.New("'metricPrefix' parameter must be specified once")
	}

	if probeConfig.MetricPrefix == "" {
		probeConfig.MetricPrefix = "azure_monitor"
	}

	probeConfig.MetricNamespace = query.Get("metricNamespace")

	if len(query["metricNamespace"]) > 1 {
		return nil, errors.New("'metricNamespace' parameter must be specified once")
	}

	if probeConfig.MetricNamespace == "" {
		probeConfig.MetricNamespace = probeConfig.ResourceType
	}

	if len(query["top"]) == 1 {
		topInt64, err := strconv.ParseInt(query.Get("top"), 10, 32)
		if err != nil {
			return nil, errors.New("'top' parameter must be a number")
		}

		probeConfig.Top = to.Ptr(int32(topInt64))
	} else if len(query["top"]) >= 1 {
		return nil, errors.New("'top' parameter must be specified once")
	}

	if len(query["queryCacheExpiration"]) == 1 {
		var err error

		probeConfig.QueryCacheCacheExpiration, err = time.ParseDuration(query.Get("queryCacheExpiration"))
		if err != nil {
			return nil, errors.New("'queryCacheExpiration' parameter must be a duration")
		}
	} else if len(query["queryCacheExpiration"]) >= 1 {
		return nil, errors.New("'queryCacheExpiration' parameter must be specified once")
	}

	return probeConfig, nil
}
