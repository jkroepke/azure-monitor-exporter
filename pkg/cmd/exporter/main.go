package exporter

import (
	"context"
	"errors"
	"fmt"
	stdlog "log"
	"net/http"
	_ "net/http/pprof" //nolint:gosec // pprof is a debugging tool
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	azlog "github.com/Azure/azure-sdk-for-go/sdk/azcore/log"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/subscription/armsubscription"
	"github.com/alecthomas/kingpin/v2"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/jkroepke/azure-monitor-exporter/pkg/cache"
	"github.com/jkroepke/azure-monitor-exporter/pkg/probe"
	"github.com/jkroepke/azure-monitor-exporter/pkg/tracing"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	versionCollector "github.com/prometheus/client_golang/prometheus/collectors/version"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/promlog"
	"github.com/prometheus/common/promlog/flag"
	"github.com/prometheus/common/version"
	"github.com/prometheus/exporter-toolkit/web"
	webflag "github.com/prometheus/exporter-toolkit/web/kingpinflag"
)

func Run() int {
	reg := prometheus.NewRegistry()

	kingpin.Version(version.Print("azure-monitor-exporter"))

	webConfig := webflag.AddFlags(kingpin.CommandLine, ":8080")
	logRetries := kingpin.Flag("log.retries", "Log Azure REST API retries").Default("false").Envar("AZURE_MONITOR_EXPORTER_LOG_RETRIES").Bool()

	promlogConfig := &promlog.Config{}
	flag.AddFlags(kingpin.CommandLine, promlogConfig)
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()

	logger := promlog.New(promlogConfig)

	exporterTracing := tracing.New(reg, http.DefaultTransport)
	httpClient := &http.Client{
		Transport: exporterTracing.Transport,
	}

	if *logRetries {
		azlog.SetEvents(azlog.EventRetryPolicy)
		azlog.SetListener(func(cls azlog.Event, msg string) {
			if cls == azlog.EventRetryPolicy {
				if strings.HasPrefix(msg, "response 2") ||
					strings.HasPrefix(msg, "=====> Try=") ||
					strings.HasPrefix(msg, "End Try") ||
					msg == "exit due to non-retriable status code" {
					return
				}

				_ = level.Warn(logger).Log("msg", msg)
			}
		})
	}

	cred, err := azidentity.NewDefaultAzureCredential(&azidentity.DefaultAzureCredentialOptions{
		ClientOptions: azcore.ClientOptions{
			Transport: httpClient,
		},
	})
	if err != nil {
		_ = level.Error(logger).Log("msg", "Error obtain azure credentials", "err", err)

		return 1
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	subscriptions, err := discoverSubscriptions(ctx, cred, httpClient)
	if err != nil {
		_ = level.Error(logger).Log("msg", "Error obtain azure credentials", "err", err)

		return 1
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
		probeCollector, err := probe.New(logger, httpClient, r, cred, subscriptions, queryCache)
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
					Placeholder: "resources",
				},
				{
					Label:       "Resource Graph Query",
					Type:        "text",
					Name:        "resourceType",
					Placeholder: "microsoft.compute/virtualmachines",
				},
				{
					Label:       "Metric Names",
					Type:        "text",
					Name:        "metricName",
					Placeholder: "vmAvailabilityMetric",
				},
				{
					Label:       "Interval",
					Type:        "text",
					Name:        "interval",
					Placeholder: "PT5M",
				},
				{
					Label:       "Interval",
					Type:        "text",
					Name:        "interval",
					Placeholder: "PT5M",
				},
				{
					Label:       "Cache",
					Type:        "text",
					Name:        "queryCacheExpiration",
					Placeholder: "60s",
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

		return 1
	}

	http.Handle("/", landingPage)

	srv := &http.Server{
		ReadHeaderTimeout: time.Second * 3,
		ErrorLog:          stdlog.New(log.NewStdlibAdapter(logger), "ERROR: ", stdlog.LstdFlags),
	}

	// graceful shutdown on SIGTERM or SIGINT signal
	go func() {
		termCh := make(chan os.Signal, 1)
		signal.Notify(termCh, os.Interrupt, syscall.SIGTERM)
		<-termCh

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		_ = srv.Shutdown(ctx)
	}()

	if err := web.ListenAndServe(srv, webConfig, logger); err != nil {
		if errors.Is(err, http.ErrServerClosed) {
			return 0
		}

		_ = level.Error(logger).Log("msg", "Error starting HTTP server", "err", err)

		return 1
	}

	return 0
}

func discoverSubscriptions(ctx context.Context, cred azcore.TokenCredential, httpClient *http.Client) ([]string, error) {
	subscriptionClient, err := armsubscription.NewSubscriptionsClient(cred, &arm.ClientOptions{
		ClientOptions: azcore.ClientOptions{
			Transport: httpClient,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create subscription client: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
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
