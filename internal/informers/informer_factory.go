/*
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
*/

package informers

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	finopsDataTypes "github.com/krateoplatformops/finops-data-types/api/v1"
	finopsv1 "github.com/krateoplatformops/finops-operator-scraper/api/v1"
	clientHelper "github.com/krateoplatformops/finops-operator-scraper/internal/helpers/kube/client"
	"github.com/krateoplatformops/finops-operator-scraper/internal/helpers/kube/comparators"
	"github.com/krateoplatformops/finops-operator-scraper/internal/utils"
	"github.com/krateoplatformops/provider-runtime/pkg/logging"
	appsv1 "k8s.io/api/apps/v1"

	corev1 "k8s.io/api/core/v1"
)

type InformerFactory struct {
	Logger    logging.Logger
	DynClient *dynamic.DynamicClient
}

func (r *InformerFactory) StartInformer(namespace string, gvr schema.GroupVersionResource) {
	inClusterConfig, _ := rest.InClusterConfig()

	inClusterConfig.APIPath = "/apis"
	inClusterConfig.GroupVersion = &finopsv1.GroupVersion

	clientSet, err := dynamic.NewForConfig(inClusterConfig)
	if err != nil {
		panic(err)
	}

	fac := dynamicinformer.NewFilteredDynamicSharedInformerFactory(clientSet, 0, namespace, nil)
	informer := fac.ForResource(gvr).Informer()

	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		DeleteFunc: func(obj interface{}) {
			item := obj.(*unstructured.Unstructured)
			var toReplace string
			switch kind := item.GetKind(); kind {
			case "Deployment":
				toReplace = "-deployment"
			case "ConfigMap":
				toReplace = "-configmap"
			}

			ScraperConfig, err := r.getConfigurationCR(context.TODO(), strings.Replace(item.GetName(), toReplace, "", 1), item.GetNamespace())
			if err != nil {
				r.Logger.Info("could not obtain ScraperConfig, everything deleted, stopping", "resource name", item.GetName(), "resource kind", item.GetKind(), "resource namespace", item.GetNamespace())
				return
			}

			if !ScraperConfig.DeletionTimestamp.IsZero() {
				r.Logger.Info("resource was deleted, however config is marked for deletion, stopping", "resource name", item.GetKind(), "deletion timestamp", ScraperConfig.DeletionTimestamp)
				return
			}
			err = r.createRestoreObject(context.TODO(), ScraperConfig, item.GetKind(), true)
			if err != nil {
				r.Logger.Info("unable to create resource again, stopping", "resource name", item.GetKind())
				return
			}
			r.Logger.Info("Created new copy of resource object", "resource kind", item.GetKind(), "resource namespace", item.GetNamespace(), "resource name", item.GetName())
		},
		UpdateFunc: func(oldObj interface{}, newObj interface{}) {
			item := newObj.(*unstructured.Unstructured)
			found := false
			if ownerReferences := item.GetOwnerReferences(); len(ownerReferences) > 0 {
				for _, ownerReference := range ownerReferences {
					if ownerReference.Kind == "ScraperConfig" {
						found = true
					}
				}
			}
			if !found {
				return
			}

			var toReplace string
			switch kind := item.GetKind(); kind {
			case "Deployment":
				toReplace = "-deployment"
			case "ConfigMap":
				toReplace = "-configmap"
			}

			ScraperConfig, err := r.getConfigurationCR(context.TODO(), strings.Replace(item.GetName(), toReplace, "", 1), item.GetNamespace())
			if err != nil {
				r.Logger.Info("could not obtain ScraperConfig, everything deleted, stopping", "resource name", item.GetName(), "resource kind", item.GetKind(), "resource namespace", item.GetNamespace())
				return
			}

			restore := false

			switch kind := item.GetKind(); kind {
			case "Deployment":
				deploymentObj := &appsv1.Deployment{}
				err = runtime.DefaultUnstructuredConverter.FromUnstructured(item.Object, deploymentObj)
				if err != nil {
					r.Logger.Info("unable to parse unstructured, stopping", "resource kind", item.GetKind())
					return
				}
				if !comparators.CheckDeployment(*deploymentObj, *ScraperConfig) {
					restore = true
				}
			case "ConfigMap":
				configMapObj := &corev1.ConfigMap{}
				err = runtime.DefaultUnstructuredConverter.FromUnstructured(item.Object, configMapObj)
				if err != nil {
					r.Logger.Info("unable to parse unstructured, stopping", "resource kind", item.GetKind())
					return
				}
				if !comparators.CheckConfigMap(*configMapObj, *ScraperConfig) {
					restore = true
				}
			}

			if restore {
				err = r.createRestoreObject(context.TODO(), ScraperConfig, item.GetKind(), false)
				if err != nil {
					r.Logger.Info("unable to create resource again, stopping", "resource kind", item.GetKind())
					return
				}
				r.Logger.Info("Restored resource object", "resource kind", item.GetKind(), "resource namespace", item.GetNamespace(), "resource name", item.GetName())
			}
		},
	})

	go informer.Run(make(<-chan struct{}))
}

func (r *InformerFactory) getConfigurationCR(ctx context.Context, name string, namespace string) (*finopsv1.ScraperConfig, error) {
	configurationName := &finopsDataTypes.ObjectRef{Name: name, Namespace: namespace}

	ScraperConfigUnstructured, err := clientHelper.GetObj(ctx, configurationName, "finops.krateo.io/v1", "scraperconfigs", r.DynClient)
	if err != nil {
		return &finopsv1.ScraperConfig{}, fmt.Errorf("unable to fetch finopsv1.ScraperConfig: %v", err)
	}
	ScraperConfig := &finopsv1.ScraperConfig{}
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(ScraperConfigUnstructured.Object, ScraperConfig)
	if err != nil {
		return &finopsv1.ScraperConfig{}, fmt.Errorf("unable to convert unstructured to finopsv1.ScraperConfig: %v", err)
	}

	return ScraperConfig, nil
}

func (r *InformerFactory) createRestoreObject(ctx context.Context, ScraperConfig *finopsv1.ScraperConfig, objectToRestoreKind string, create bool) error {
	// Switch su kind
	var err error
	var genericObjectUnstructured *unstructured.Unstructured
	var resource string

	switch objectToRestoreKind {
	case "Deployment":
		genericObject, _ := utils.GetGenericScraperDeployment(ScraperConfig)
		genericObjectUnstructured, err = clientHelper.ToUnstructured(genericObject)
		if err != nil {
			return err
		}
		resource = "deployments"
	case "ConfigMap":
		genericObject, _ := utils.GetGenericScraperConfigMap(ScraperConfig)
		genericObjectUnstructured, err = clientHelper.ToUnstructured(genericObject)
		if err != nil {
			return err
		}
		resource = "configmaps"
	}
	if err != nil {
		return err
	}

	if create {
		err = clientHelper.CreateObj(ctx, genericObjectUnstructured, resource, r.DynClient)
	} else {
		err = clientHelper.UpdateObj(ctx, genericObjectUnstructured, resource, r.DynClient)
	}
	if err != nil {
		return err
	}
	return nil
}
