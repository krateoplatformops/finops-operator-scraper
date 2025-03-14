# FinOps Operator Scraper
This repository is part of the wider exporting architecture for the Krateo Composable FinOps and manages the creation of the scrapers reading the FOCUS cost reports from the Prometheus Exporters.

## Summary
1. [Overview](#overview)
2. [Architecture](#architecture)
3. [Examples](#examples)
4. [Configuration](#configuration)

## Overview
This component is tasked with the creation of a generic scraper, according to the description given in a Custom Resource (CR). After the creation of the CR, the operator reads the "scraper" configuration part and creates two resources: a deployment with a generic prometheus scraper inside and a configmap containing the configuration. The scraper parses the prometheus data and obtains the given database-config to upload all metrics to a database.

## Architecture
![Krateo Composable FinOps Operator Scraper](resources/images/KCF-operator-scraper.png)

## Examples
```yaml
apiVersion: finops.krateo.io/v1
kind: DatabaseConfig
metadata:
  name: # DatabaseConfig name
  namespace: # DatabaseConfig namespace
spec:
  username: # username string
  passwordSecretRef: # object reference to secret with password
    name: # secret name
    namespace: # secret namespace
    key: # secret key
---
apiVersion: finops.krateo.io/v1
kind: ScraperConfig
metadata:
  name: # ScraperConfig name
  namespace: # ScraperConfig namespace
spec:
  scraperConfig:
    tableName: # tableName in the database to upload the data to
    api: # the API to call with the prometheus exporter
      path: # the path inside the domain
      verb: GET # the method to call the API with
      endpointRef: # secret with the url in the format http(s)://host:port
        name: 
        namespace:
    pollingInterval: # time duration, e.g., 12h30m
    scraperDatabaseConfigRef: # See above kind DatabaseConfig
      name: # name of the databaseConfigRef CR 
      namespace: # namespace of the databaseConfigRef CR
```

### Example Use Case for Pricing Visualization
The Composable FinOps can be used to display pricing in the Krateo Composable Portal cards through a dedicated composition. You can find out more here: [krateo-v2-template-finops-example-pricing-vm-azure](https://github.com/krateoplatformops/krateo-v2-template-finops-example-pricing-vm-azure).

## Configuration

### Prerequisites
- go version v1.21.0+
- docker version 17.03+.
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.

### Dependencies
You need to install CrateDB in the cluster and configure the [finops-database-handler](https://github.com/krateoplatformops/finops-database-handler).

### Installation with HELM
```sh
$ helm repo add krateo https://charts.krateo.io
$ helm repo update krateo
$ helm install finops-operator-scraper krateo/finops-operator-scraper
```
### Configuration
The database-config CR is required.

The scraper container is created in the namespace of the CR. The scraper container looks for a secret in the CR namespace called `registry-credentials`, configurable in the HELM chart.