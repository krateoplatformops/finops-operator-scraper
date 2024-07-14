# operator-scraper
This repository is part of a wider exporting architecture for the FinOps Cost and Usage Specification (FOCUS). This component is tasked with the creation of a generic scraper, according to the description given in a Custom Resource (CR). After the creation of the CR, the operator reads the "scraper" configuration part and creates two resources: a deployment with a generic prometheus scraper inside and a configmap containing the configuration. The scraper parses the prometheus data and obtains the given database-config to upload all metrics to a database.

## Dependencies
There is the need to have an active Databricks cluster, with SQL warehouse and notebooks configured. Its login details must be placed in the database-config CR.

## Configuration
The database-config CR is required.
The deployment of the operator needs a secret for the repository, called `registry-credentials` in the namespace `finops`.

The scraper container is created in the namespace of the CR. The scraper container looks for a secret in the CR namespace called `registry-credentials-default`

## Installation
### Prerequisites
- go version v1.21.0+
- docker version 17.03+.
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.


### Installation with HELM
```sh
$ helm repo add krateo https://charts.krateo.io
$ helm repo update krateo
$ helm install finops-operator-scraper krateo/finops-operator-scraper
```

## License

Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

## Databricks token
The Databricks token can be obtained through the dashboard UI.

