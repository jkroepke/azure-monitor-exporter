package main

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/monitor/query/azmetrics"
)

type ProbeConfig struct {
	Query           string
	MetricNamespace string
	MetricNames     []string
	MetricPrefix    string

	azmetrics.QueryResourcesOptions
}

func GetConfigFromRequest(request *http.Request) (*ProbeConfig, error) {
	query := request.URL.Query()

	probeConfig := &ProbeConfig{}

	probeConfig.Query = query.Get("query")
	if len(query["query"]) != 1 || probeConfig.Query == "" {
		return nil, fmt.Errorf("'query' parameter must be specified once")
	}

	probeConfig.Aggregation = to.Ptr(query.Get("aggregation"))
	if len(query["aggregation"]) > 1 {
		return nil, fmt.Errorf("'aggregation' parameter must be specified once")
	}

	probeConfig.Interval = to.Ptr(query.Get("interval"))
	if len(query["interval"]) > 1 {
		return nil, fmt.Errorf("'interval' parameter must be specified once")
	}

	probeConfig.Filter = to.Ptr(query.Get("filter"))
	if len(query["filter"]) > 1 {
		return nil, fmt.Errorf("'filter' parameter must be specified once")
	}

	probeConfig.MetricPrefix = query.Get("metricPrefix")
	if len(query["filter"]) > 1 {
		return nil, fmt.Errorf("'metricPrefix' parameter must be specified once")
	}
	if probeConfig.MetricPrefix == "" {
		probeConfig.MetricPrefix = "azure_monitor"
	}

	probeConfig.MetricNamespace = query.Get("metricNamespace")
	if len(query["metricNamespace"]) > 1 {
		return nil, fmt.Errorf("'metricNamespace' parameter must be specified once")
	}

	queryTop := query.Get("top")
	if len(query["top"]) > 1 {
		return nil, fmt.Errorf("'top' parameter must be specified once")
	}

	if queryTop != "" {
		topInt64, err := strconv.ParseInt(queryTop, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("'top' parameter must be a number")
		}

		probeConfig.Top = to.Ptr(int32(topInt64))
	}

	return probeConfig, nil
}
