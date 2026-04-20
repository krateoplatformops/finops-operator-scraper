# finops-operator-scraper

A Kubernetes operator that creates and manages generic scrapers to read FOCUS cost reports from Prometheus exporters and upload them to a database, as part of the Krateo Composable FinOps architecture.

📖 **Full documentation**: [docs.krateo.io — finops-operator-scraper](https://docs.krateo.io/key-concepts/kcf/finops-components/finops-operator-scraper)

---

## Key features

- Automatically provisions a Prometheus scraper deployment and configmap from a single Custom Resource
- Parses Prometheus-format metrics and uploads them to CrateDB via the finops-database-handler
- Configurable polling intervals and database targets per scraper instance

## Requirements

| Dependency | Minimum version |
|------------|----------------|
| Kubernetes | v1.31 |
| Krateo | v3.0.0 |
| finops-database-handler | v0.5.3 |
| CrateDB | v5.9.6 |

## Install

```bash
helm repo add krateo https://charts.krateo.io
helm repo update
helm install finops-operator-scraper krateo/finops-operator-scraper --namespace krateo-system --create-namespace
```

> For advanced installation options, custom values, and upgrade instructions, see the [installation guide](https://docs.krateo.io/key-concepts/kcf/finops-components/finops-operator-scraper).

## Environment variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `POLLING_INTERVAL` | No | `300` | Polling interval of the operator in seconds |
| `MAX_RECONCILE_RATE` | No | `1` | Number of workers for the operator |
| `REGISTRY` | No | `ghcr.io/krateoplatformops` | Registry to pull the exporter image from |
| `REGISTRY_CREDENTIALS` | No | `registry-credentials` | Name of the secret holding registry credentials |
| `SCRAPER_VERSION` | No | `0.5.0` | Version of the exporter image |
| `SCRAPER_NAME` | No | `finops-prometheus-exporter` | Name of the exporter image |
| `URL_DB_WEBSERVICE` | No | http://finops-database-handler.finops:8088 | URL for the finops-database-handler service |
