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

package informer

import (
	"bytes"
	"context"
	"strings"

	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/krateoplatformops/finops-operator-scraper/internal/utils"

	operatorPackage "github.com/krateoplatformops/finops-operator-exporter/api/v1"
	finopsv1 "github.com/krateoplatformops/finops-operator-scraper/api/v1"

	corev1 "k8s.io/api/core/v1"
)

type ConfigMapReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *ConfigMapReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.Log.WithValues("FinOps.V1", "CONFIGMAP")
	var err error

	var configMap corev1.ConfigMap
	// ConfigMap does not exist, check if the ScraperConfig exists
	if err = r.Get(ctx, req.NamespacedName, &configMap); err != nil {
		logger.Info("unable to fetch corev1.ConfigMap " + req.Name + " " + req.Namespace)
		scraperConfig, err := r.getConfigurationCR(ctx, strings.Replace(req.Name, "-configmap", "", 1), req.Namespace)
		if err != nil {
			logger.Info("Unable to fetch ScraperConfig for " + strings.Replace(req.Name, "-configmap", "", 1) + " " + req.Namespace)
			return ctrl.Result{Requeue: false}, client.IgnoreNotFound(err)
		}
		err = r.createRestoreConfigMapAgain(ctx, scraperConfig, false)
		if err != nil {
			logger.Error(err, "Unable to create ConfigMap again "+req.Name+" "+req.Namespace)
			return ctrl.Result{Requeue: false}, client.IgnoreNotFound(err)
		}
		logger.Info("Created configmap again: " + req.Name + " " + req.Namespace)

	}

	if ownerReferences := configMap.GetOwnerReferences(); len(ownerReferences) > 0 {
		if ownerReferences[0].Kind == "ScraperConfig" {
			logger.Info("Called for " + req.Name + " " + req.Namespace + " owner: " + ownerReferences[0].Kind)
			scraperConfig, err := r.getConfigurationCR(ctx, strings.Replace(req.Name, "-configmap", "", 1), req.Namespace)
			if err != nil {
				return ctrl.Result{}, err
			}
			if !checkConfigMap(configMap, scraperConfig) {
				err = r.createRestoreConfigMapAgain(ctx, scraperConfig, true)
				if err != nil {
					return ctrl.Result{}, err
				}
				logger.Info("Updated configmap: " + req.Name + " " + req.Namespace)
			}
		}
	} else {
		return ctrl.Result{Requeue: false}, nil
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ConfigMapReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.ConfigMap{}).
		Complete(r)
}

func (r *ConfigMapReconciler) getConfigurationCR(ctx context.Context, name string, namespace string) (finopsv1.ScraperConfig, error) {
	var scraperConfig finopsv1.ScraperConfig
	configurationName := types.NamespacedName{Name: name, Namespace: namespace}
	if err := r.Get(ctx, configurationName, &scraperConfig); err != nil {
		log.Log.Error(err, "unable to fetch finopsv1.ScraperConfig")
		return finopsv1.ScraperConfig{}, err
	}
	return scraperConfig, nil
}

func (r *ConfigMapReconciler) createRestoreConfigMapAgain(ctx context.Context, scraperConfig finopsv1.ScraperConfig, restore bool) error {
	genericScraperConfigMap, err := utils.GetGenericScraperConfigMap(scraperConfig)
	if err != nil {
		return err
	}
	if restore {
		err = r.Update(ctx, genericScraperConfigMap)
	} else {
		err = r.Create(ctx, genericScraperConfigMap)
	}
	if err != nil {
		return err
	}
	return nil
}

func checkConfigMap(configMap corev1.ConfigMap, scraperConfig finopsv1.ScraperConfig) bool {
	if configMap.Name != scraperConfig.Name+"-configmap" {
		return false
	}

	scraperConfigFile := utils.ScraperConfigFile{}
	databaseConfigRef := operatorPackage.ScraperDatabaseConfigRef{}
	exporter := utils.Exporter{}

	exporter.Url = scraperConfig.Spec.Url
	exporter.PollingIntervalHours = scraperConfig.Spec.PollingIntervalHours
	exporter.TableName = scraperConfig.Spec.TableName

	databaseConfigRef.Name = scraperConfig.Spec.ScraperDatabaseConfigRef.Name
	databaseConfigRef.Namespace = scraperConfig.Spec.ScraperDatabaseConfigRef.Namespace

	scraperConfigFile.DatabaseConfigRef = databaseConfigRef
	scraperConfigFile.Exporter = exporter

	yamlData, err := yaml.Marshal(scraperConfigFile)
	if err != nil {
		return false
	}

	binaryData := configMap.BinaryData
	if yamlDataFromLive, ok := binaryData["config.yaml"]; ok {
		if !bytes.Equal(yamlData, yamlDataFromLive) {
			return false
		}
	} else {
		return false
	}

	ownerReferencesLive := configMap.OwnerReferences
	if len(ownerReferencesLive) != 1 {
		return false
	}

	if ownerReferencesLive[0].Kind != scraperConfig.Kind ||
		ownerReferencesLive[0].Name != scraperConfig.Name ||
		ownerReferencesLive[0].UID != scraperConfig.UID ||
		ownerReferencesLive[0].APIVersion != scraperConfig.APIVersion {
		return false
	}

	return true
}
