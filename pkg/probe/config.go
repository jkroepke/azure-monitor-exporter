package probe

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
)

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
		return nil, fmt.Errorf("'resourceType' parameter must be specified once")
	}

	if len(query["metricName"]) != 0 {
		probeConfig.MetricNames = query["metricName"]
	} else if len(query["metricName[]"]) != 0 {
		probeConfig.MetricNames = query["metricName[]"]
	} else {
		return nil, fmt.Errorf("'metricName' parameter must be specified")
	}

	probeConfig.Query = "Resources"
	if len(query["query"]) == 1 {
		probeConfig.Query = query.Get("query")
	}

	if len(query["aggregation"]) == 1 {
		probeConfig.Aggregation = to.Ptr(query.Get("aggregation"))
	} else if len(query["aggregation"]) >= 1 {
		probeConfig.Aggregation = to.Ptr(strings.Join(query["aggregation"], ","))
	} else if len(query["aggregation[]"]) == 1 {
		probeConfig.Aggregation = to.Ptr(query.Get("aggregation[]"))
	} else if len(query["aggregation[]"]) >= 1 {
		probeConfig.Aggregation = to.Ptr(strings.Join(query["aggregation[]"], ","))
	}

	if len(query["interval"]) == 1 {
		probeConfig.Interval = to.Ptr(query.Get("interval"))
	} else if len(query["interval"]) > 1 {
		return nil, fmt.Errorf("'interval' parameter must be specified once")
	}

	if len(query["filter"]) == 1 {
		probeConfig.Filter = to.Ptr(query.Get("filter"))
	} else if len(query["filter"]) > 1 {
		return nil, fmt.Errorf("'filter' parameter must be specified once")
	}

	if len(query["metricPrefix"]) == 1 {
		probeConfig.MetricPrefix = query.Get("metricPrefix")
	} else if len(query["metricPrefix"]) > 1 {
		return nil, fmt.Errorf("'metricPrefix' parameter must be specified once")
	}

	if probeConfig.MetricPrefix == "" {
		probeConfig.MetricPrefix = "azure_monitor"
	}

	probeConfig.MetricNamespace = query.Get("metricNamespace")
	if len(query["metricNamespace"]) > 1 {
		return nil, fmt.Errorf("'metricNamespace' parameter must be specified once")
	}

	if probeConfig.MetricNamespace == "" {
		probeConfig.MetricNamespace = probeConfig.ResourceType
	}

	if len(query["top"]) == 1 {
		topInt64, err := strconv.ParseInt(query.Get("top"), 10, 32)
		if err != nil {
			return nil, fmt.Errorf("'top' parameter must be a number")
		}

		probeConfig.Top = to.Ptr(int32(topInt64))
	} else if len(query["top"]) >= 1 {
		return nil, fmt.Errorf("'top' parameter must be specified once")
	}

	if len(query["queryCacheExpiration"]) == 1 {
		var err error

		probeConfig.QueryCacheCacheExpiration, err = time.ParseDuration(query.Get("queryCacheExpiration"))
		if err != nil {
			return nil, fmt.Errorf("'queryCacheExpiration' parameter must be a duration")
		}
	} else if len(query["queryCacheExpiration"]) >= 1 {
		return nil, fmt.Errorf("'queryCacheExpiration' parameter must be specified once")
	}

	return probeConfig, nil
}
