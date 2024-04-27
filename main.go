package main

import (
	"context"
	"fmt"
	stdlog "log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/subscription/armsubscription"
	"github.com/alecthomas/kingpin/v2"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/jkroepke/azure-monitor-exporter/pkg/cache"
	"github.com/jkroepke/azure-monitor-exporter/pkg/probe"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	versionCollector "github.com/prometheus/client_golang/prometheus/collectors/version"
	_ "github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/promlog"
	"github.com/prometheus/common/promlog/flag"
	"github.com/prometheus/common/version"
	"github.com/prometheus/exporter-toolkit/web"
	webflag "github.com/prometheus/exporter-toolkit/web/kingpinflag"
)

func main() {
	reg := prometheus.NewRegistry()
	webConfig := webflag.AddFlags(kingpin.CommandLine, ":8080")

	kingpin.Version(version.Print("azure-monitor-exporter"))

	promlogConfig := &promlog.Config{}
	flag.AddFlags(kingpin.CommandLine, promlogConfig)
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()

	logger := promlog.New(promlogConfig)

	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		_ = level.Error(logger).Log("msg", "Error obtain azure credentials", "err", err)

		os.Exit(1)
	}

	subscriptions, err := discoverSubscriptions(cred)
	if err != nil {
		_ = level.Error(logger).Log("msg", "Error obtain azure credentials", "err", err)

		os.Exit(1)
	}

	_ = level.Info(logger).Log("msg", "discovered subscriptions", "subscriptions", strings.Join(subscriptions, ","))

	// Add go runtime metrics and process collectors.
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		versionCollector.NewCollector("azure_monitor_exporter"),
	)

	queryCache := cache.NewCache[probe.Resources]()

	http.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{
		Registry: reg,
		ErrorLog: stdlog.New(log.NewStdlibAdapter(logger), "ERROR: ", stdlog.LstdFlags),
	}))
	http.HandleFunc("/probe", func(w http.ResponseWriter, r *http.Request) {
		probeCollector, err := probe.New(r, logger, cred, subscriptions, queryCache)
		if err != nil {
			_ = level.Error(logger).Log("msg", "Error creating probe", "err", err)
			http.Error(w, err.Error(), http.StatusBadRequest)

			return
		}

		registry := prometheus.NewRegistry()
		registry.MustRegister(probeCollector)

		promhttp.HandlerFor(registry, promhttp.HandlerOpts{
			Registry: reg,
			ErrorLog: stdlog.New(log.NewStdlibAdapter(logger), "ERROR: ", stdlog.LstdFlags),
		}).ServeHTTP(w, r)
	})

	landingPage, err := web.NewLandingPage(web.LandingConfig{
		Name:        "azure-monitor-exporter",
		Description: "Prometheus Exporter for Azure Monitor",
		Version:     version.Info(),
		Form: web.LandingForm{
			Action: "/probe",
			Inputs: []web.LandingFormInput{
				{
					Label:       "Resource Graph Query",
					Type:        "text",
					Name:        "query",
					Placeholder: "resources | where type == 'microsoft.compute/virtualmachines'",
				},
			},
		},
		Links: []web.LandingLinks{
			{
				Address: "/metrics",
				Text:    "Metrics",
			},
		},
	})
	if err != nil {
		_ = level.Error(logger).Log("err", err)

		os.Exit(1)
	}

	http.Handle("/", landingPage)

	srv := &http.Server{
		ReadHeaderTimeout: time.Second * 3,
		ErrorLog:          stdlog.New(log.NewStdlibAdapter(logger), "ERROR: ", stdlog.LstdFlags),
	}

	if err := web.ListenAndServe(srv, webConfig, logger); err != nil {
		_ = level.Error(logger).Log("msg", "Error starting HTTP server", "err", err)

		os.Exit(1)
	}
}

func discoverSubscriptions(cred azcore.TokenCredential) ([]string, error) {
	subscriptionClient, err := armsubscription.NewSubscriptionsClient(cred, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create subscription client: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	subscriptions := make([]string, 0)

	pager := subscriptionClient.NewListPager(nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to advance page: %w", err)
		}

		for _, v := range page.Value {
			subscriptions = append(subscriptions, *v.SubscriptionID)
		}
	}

	return subscriptions, nil
}
