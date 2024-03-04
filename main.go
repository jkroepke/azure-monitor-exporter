package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/monitor/query/azmetrics"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resourcegraph/armresourcegraph"
	"github.com/alecthomas/kingpin/v2"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	_ "github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/promlog"
	"github.com/prometheus/common/promlog/flag"
	"github.com/prometheus/common/version"
	"github.com/prometheus/exporter-toolkit/web"
	webflag "github.com/prometheus/exporter-toolkit/web/kingpinflag"
)

var (
	subscriptionCache = map[string]any{}
)

func main() {
	toolkitFlags := webflag.AddFlags(kingpin.CommandLine, ":8080")
	metricsPath := kingpin.Flag(
		"web.telemetry-path",
		"Path under which to expose metrics.",
	).Default("/metrics").String()

	promlogConfig := &promlog.Config{}
	flag.AddFlags(kingpin.CommandLine, promlogConfig)

	kingpin.Version(version.Print("azure-monitor-exporter"))
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()
	logger := promlog.New(promlogConfig)

	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		_ = level.Error(logger).Log("msg", "Error obtain azure credentials", "err", err)
		os.Exit(1)
	}

	http.Handle(*metricsPath, promhttp.Handler()) // Normal metrics endpoint for SNMP exporter itself.
	// Endpoint to do SNMP scrapes.
	http.HandleFunc("/probe", func(w http.ResponseWriter, r *http.Request) {
		handler(w, r, logger, cred)
	})

	landingConfig := web.LandingConfig{
		Name:        "SNMP Exporter",
		Description: "Prometheus Exporter for SNMP targets",
		Version:     version.Info(),
		Form: web.LandingForm{
			Action: "/probe",
			Inputs: []web.LandingFormInput{
				{
					Label:       "Subscription",
					Type:        "text",
					Name:        "subscription",
					Placeholder: "0000000",
				},
				{
					Label:       "Resource Graph Query",
					Type:        "text",
					Name:        "query",
					Placeholder: "resources | where type == 'Microsoft.Compute/virtualMachines'",
				},
			},
		},
		Links: []web.LandingLinks{
			{
				Address: *metricsPath,
				Text:    "Metrics",
			},
		},
	}
	landingPage, err := web.NewLandingPage(landingConfig)
	if err != nil {
		_ = level.Error(logger).Log("err", err)
		os.Exit(1)
	}
	http.Handle("/", landingPage)

	srv := &http.Server{}
	if err := web.ListenAndServe(srv, toolkitFlags, logger); err != nil {
		_ = level.Error(logger).Log("msg", "Error starting HTTP server", "err", err)
		os.Exit(1)
	}
}

func handler(w http.ResponseWriter, r *http.Request, logger log.Logger, cred azcore.TokenCredential) {
	//TODO: Timeout
	ctx, cancel := context.WithDeadline(r.Context(), time.Now().Add(10*time.Second))
	defer cancel()

	//TODO: caching
	clientFactory, err := armresourcegraph.NewClientFactory(cred, nil)
	if err != nil {
		_ = level.Error(logger).Log("msg", "Error creating resource graph client", "err", err)
		http.Error(w, "'subscription' parameter must be specified once", http.StatusBadRequest)
		return
	}

	config, err := GetConfigFromRequest(r)
	if err != nil {
		_ = level.Error(logger).Log("msg", "Error parsing request", "err", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	skipToken := ""
	client := clientFactory.NewClient()

	resources := map[string]map[string][]*string{}

	for {
		res, err := client.Resources(ctx, armresourcegraph.QueryRequest{
			Options: &armresourcegraph.QueryRequestOptions{
				ResultFormat: to.Ptr(armresourcegraph.ResultFormatObjectArray),
				SkipToken:    to.Ptr(skipToken),
			},
			Query: to.Ptr(fmt.Sprintf("%s\n| project id, subscriptionID, type, location", config.Query)),
		}, nil)

		if err != nil {
			_ = level.Error(logger).Log("msg", "Error querying resource graph", "err", err)
			http.Error(w, "Error querying resource graph", http.StatusInternalServerError)
			return
		}

		if *res.Count == 0 {
			return
		}

		if rows, ok := res.Data.([]map[string]string); ok {
			for _, row := range rows {
				if _, ok := resources[row["subscriptionID"]]; !ok {
					resources[row["subscriptionID"]] = make(map[string][]*string)
				}

				if _, ok := resources[row["subscriptionID"]][row["location"]]; !ok {
					resources[row["subscriptionID"]][row["location"]] = make([]*string, 0, 100)
				}

				if _, ok := row["location"]; !ok {
					_ = level.Error(logger).Log("msg", "Error querying resource graph",
						"err", "missing column 'location' in response", "resp", fmt.Sprintf("%+v", row))

					http.Error(w, "Error querying resource graph", http.StatusInternalServerError)

					return
				}

				if _, ok := row["id"]; !ok {
					_ = level.Error(logger).Log("msg", "Error querying resource graph",
						"err", "missing column 'id' in response", "resp", fmt.Sprintf("%+v", row))

					http.Error(w, "Error querying resource graph", http.StatusInternalServerError)

					return
				}

				resources[row["subscriptionID"]][row["location"]] = append(
					resources[row["subscriptionID"]][row["location"]],
					to.Ptr(row["id"]),
				)
			}
		} else {
			_ = level.Error(logger).Log(
				"msg", "Error querying resource graph",
				"err", "unexpected type",
				"resp", fmt.Sprintf("%+v", res),
			)

			http.Error(w, "Error querying resource graph", http.StatusInternalServerError)
			return
		}

		if res.SkipToken == nil || *res.SkipToken == "" {
			break
		}

		skipToken = *res.SkipToken
	}

	registry := prometheus.NewRegistry()

	for subscriptionID, regions := range resources {
		for region, resourceIDs := range regions {
			//TODO: caching
			client, err := azmetrics.NewClient("https://"+region+".metrics.monitor.azure.com", cred, nil)
			if err != nil {
				_ = level.Error(logger).Log("msg", "Error creating metrics client", "err", err)
				http.Error(w, "Error creating metrics client", http.StatusInternalServerError)
				return
			}

			resp, err := client.QueryResources(ctx, subscriptionID, config.MetricNamespace, config.MetricNames,
				azmetrics.ResourceIDList{ResourceIDs: resourceIDs}, &config.QueryResourcesOptions,
			)
			if err != nil {
				_ = level.Error(logger).Log("msg", "Error querying metrics", "err", err)
				http.Error(w, "Error querying metrics", http.StatusInternalServerError)
				return
			}

		}
	}
}
