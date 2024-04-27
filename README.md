[![CI](https://github.com/jkroepke/azure-monitor-exporter/workflows/CI/badge.svg)](https://github.com/jkroepke/azure-monitor-exporter/actions?query=workflow%3ACI)
[![GitHub license](https://img.shields.io/github/license/jkroepke/azure-monitor-exporter)](https://github.com/jkroepke/azure-monitor-exporter/blob/master/LICENSE.txt)
[![Current Release](https://img.shields.io/github/release/jkroepke/azure-monitor-exporter.svg)](https://github.com/jkroepke/azure-monitor-exporter/releases/latest)
[![GitHub all releases](https://img.shields.io/github/downloads/jkroepke/azure-monitor-exporter/total?logo=github)](https://github.com/jkroepke/azure-monitor-exporter/releases/latest)
[![Go Report Card](https://goreportcard.com/badge/github.com/jkroepke/openvpn-auth-oauth2)](https://goreportcard.com/report/github.com/jkroepke/azure-monitor-exporter)
[![codecov](https://codecov.io/gh/jkroepke/azure-monitor-exporter/graph/badge.svg?token=66VT000UYO)](https://codecov.io/gh/jkroepke/azure-monitor-exporter)

# azure-monitor-exporter

⭐ Don't forget to star this repository! ⭐

## About

azure-monitor-exporter is a Prometheus exporter for Azure Monitor. It collects metrics from Azure Monitor and exposes
them in a format that Prometheus can scrape.

## Features

- Collects metrics from Azure Monitor 
- Using the Azure Monitor get:Batch REST API to reduce the number of requests
- fetch metrics from resources found with ServiceDiscovery via [Azure ResourceGraph API based on Kusto query](https://docs.microsoft.com/en-us/azure/governance/resource-graph/overview)

## Authentication

azure-monitor-exporter supports all authentication supported by Azure SDK for Go.

### Service principal with a secret

| Variable name         | Value                                        |
|-----------------------|----------------------------------------------|
| `AZURE_CLIENT_ID`     | Application ID of an Azure service principal |
| `AZURE_TENANT_ID`     | ID of the application's Azure AD tenant      |
| `AZURE_CLIENT_SECRET` | Password of the Azure service principal      |

### Service principal with certificate

| Variable name                   | Value                                                                          |
|---------------------------------|--------------------------------------------------------------------------------|
| `AZURE_CLIENT_ID`               | Application ID of an Azure service principal                                   |
| `AZURE_TENANT_ID`               | ID of the application's Azure AD tenant                                        |
| `AZURE_CLIENT_CERTIFICATE_PATH` | Path to a certificate file including private key (without password protection) |

### Use a managed identity

| Variable name     | Value                                                                              |
|-------------------|------------------------------------------------------------------------------------|
| `AZURE_CLIENT_ID` | User-assigned managed client id. Can be avoid, if a system assign identity is used |
| `AZURE_TENANT_ID` | ID of the application's Azure AD tenant                                            |

### Use a workload identity

Refer to the [workload identity documentation](https://learn.microsoft.com/en-us/azure/aks/workload-identity-overview?tabs=dotnet#service-account-labels-and-annotations) for more information.

## Probe Configuration

HTTP endpoint: `/probe`

The probe configuration is done via the HTTP GET parameters. The following parameters are supported:

| Parameter name     | Format                                    | Description                                                                                                          | Default               |
|--------------------|-------------------------------------------|----------------------------------------------------------------------------------------------------------------------|-----------------------|
| **`resourceType`** | single string                             | resource type of resources to scrape                                                                                 | none (required value) |
| **`metricName`**   | single string                             | metric names to scrape                                                                                               | none (required value) |
| `query`            | single string                             | kusto query used against ResourceGraph to get target resources                                                       | `Resources`           |
| `subscriptionID`   | comma separated string or multiple values | SubscriptionIDs in scope                                                                                             | all accessible        |
| `aggregation`      | comma separated string or multiple values | Azure Monitor metric aggregation value (minimum, maximum, average, total, count, multiple possible separated with ,) | all available         |
| `interval`         | ISO 8601 time interval                    | Azure Monitor metric interval                                                                                        | none                  |
| `timespan`         | ISO 8601 time interval                    | Azure Monitor metric timespan                                                                                        | 1 hour                |
| `filter`           | single string                             | Azure Monitor metric filter                                                                                          | none                  |


To identify the correct `metricName`, you can use the [Azure documentation](https://learn.microsoft.com/en-us/azure/azure-monitor/reference/supported-metrics/metrics-index) and search for `Name in REST API`.


## Prometheus configuration examples

### Redis

<details>
<summary>Click to expand</summary>

```yaml
- job_name: azure-metrics-redis
  scrape_interval: 1m
  metrics_path: /probe
  params:
    resourceType:
    - Microsoft.Cache/Redis"
    metricName:
    - connectedclients
    - totalcommandsprocessed
    - cachehits
    - cachemisses
    - getcommands
    - setcommands
    - operationsPerSecond
    - evictedkeys
    - totalkeys
    - expiredkeys
    - usedmemory
    - usedmemorypercentage
    - usedmemoryRss
    - serverLoad
    - cacheWrite
    - cacheRead
    - percentProcessorTime
    - cacheLatency
    - errors
    interval: ["PT1M"]
    timespan: ["PT1M"]
    aggregation:
    - average
    - total
  static_configs:
  - targets: ["azure-metrics-exporter:8080"]
```

</details>
