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

### To Deploy on the cluster
**Build and push your image to the location specified by `IMG`:**

```sh
make docker-build docker-push IMG=<some-registry>/operator-scraper:tag
```

**NOTE:** This image ought to be published in the personal registry you specified. 
And it is required to have access to pull the image from the working environment. 
Make sure you have the proper permission to the registry if the above commands don’t work.

**Install the CRDs into the cluster:**

```sh
make install
```

**Deploy the Manager to the cluster with the image specified by `IMG`:**
**the REPO variable is mandatory. This variable points to the repository for the prometheus-scraper-generic image**

```sh
make deploy IMG=<some-registry>/operator-scraper:tag REPO=<some-registry>
```

> **NOTE**: If you encounter RBAC errors, you may need to grant yourself cluster-admin 
privileges or be logged in as admin.

**Create instances of your solution**
You can apply the samples (examples) from the config/sample:

```sh
kubectl apply -k config/samples/
```

>**NOTE**: Ensure that the samples has default values to test it out.

### To Uninstall
**Delete the instances (CRs) from the cluster:**

```sh
kubectl delete -k config/samples/
```

**Delete the APIs(CRDs) from the cluster:**

```sh
make uninstall
```

**UnDeploy the controller from the cluster:**

```sh
make undeploy
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

