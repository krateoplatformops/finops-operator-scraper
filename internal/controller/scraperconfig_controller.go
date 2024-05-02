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

package controller

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	finopsv1 "github.com/krateoplatformops/finops-operator-scraper/api/v1"

	"github.com/krateoplatformops/finops-operator-scraper/internal/utils"
)

// ScraperConfigReconciler reconciles a ScraperConfig object
type ScraperConfigReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=finops.krateo.io,namespace=finops,resources=scraperconfigs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=finops.krateo.io,namespace=finops,resources=scraperconfigs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=finops.krateo.io,namespace=finops,resources=scraperconfigs/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps,namespace=finops,resources=deployments,verbs=get;create;update;watch;list
//+kubebuilder:rbac:groups=core,namespace=finops,resources=configmaps,verbs=get;create;update;list

func (r *ScraperConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.Log.WithValues("FinOps.V1", req.NamespacedName)
	var err error

	// Get the request object
	var scraperConfig finopsv1.ScraperConfig
	if err := r.Get(ctx, req.NamespacedName, &scraperConfig); err != nil {
		logger.Error(err, "unable to fetch finopsv1.ScraperConfig")
		return ctrl.Result{Requeue: false}, client.IgnoreNotFound(err)
	}

	// Check if a deployment for this configuration already exists
	existingObjDeployment := &appsv1.Deployment{}
	ExistingDeploymentNamespace := scraperConfig.Status.ActiveScraper.Namespace
	ExistingDeploymentName := scraperConfig.Status.ActiveScraper.Name
	ExistingConfigMapNamespace := scraperConfig.Status.ConfigMap.Namespace
	ExistingConfigMapName := scraperConfig.Status.ConfigMap.Name

	// ConfigMap status objRef and pointer for GET
	existingObjConfigMap := &corev1.ConfigMap{}

	// Check if all elements of the deployment exist

	_ = r.Get(ctx, types.NamespacedName{Namespace: ExistingDeploymentNamespace, Name: ExistingDeploymentName}, existingObjDeployment)
	_ = r.Get(ctx, types.NamespacedName{Namespace: ExistingConfigMapNamespace, Name: ExistingConfigMapName}, existingObjConfigMap)
	// If any the objects does not exist, something happend, reconcile spec-status
	if existingObjDeployment.Name == "" || existingObjConfigMap.Name == "" {
		if err = r.createScraperFromScratch(ctx, req, scraperConfig); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	} else if err = r.checkScraperStatus(ctx, scraperConfig); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ScraperConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&finopsv1.ScraperConfig{}).
		Complete(r)
}

func (r *ScraperConfigReconciler) createScraperFromScratch(ctx context.Context, req ctrl.Request, scraperConfig finopsv1.ScraperConfig) error {

	var err error
	// Create the ConfigMap first
	// Check if the ConfigMap exists
	genericScraperConfigMap := &corev1.ConfigMap{}
	_ = r.Get(context.Background(), types.NamespacedName{
		Namespace: req.Namespace,
		Name:      scraperConfig.Name + "-configmap",
	}, genericScraperConfigMap)
	// If it does not exist, create it
	if genericScraperConfigMap.ObjectMeta.Name == "" {
		genericScraperConfigMap, err = utils.GetGenericScraperConfigMap(scraperConfig)
		if err != nil {
			return err
		}
		err = r.Create(ctx, genericScraperConfigMap)
		if err != nil {
			return err
		}
	}

	// Create the generic exporter deployment
	genericScraperDeployment := &appsv1.Deployment{}
	_ = r.Get(context.Background(), types.NamespacedName{
		Namespace: req.Namespace,
		Name:      scraperConfig.Name + "-deployment",
	}, genericScraperDeployment)
	if genericScraperDeployment.ObjectMeta.Name == "" {
		genericScraperDeployment, err = utils.GetGenericScraperDeployment(scraperConfig)
		if err != nil {
			return err
		}
		// Create the actual deployment
		err = r.Create(ctx, genericScraperDeployment)
		if err != nil {
			return err
		}
	}

	scraperConfig.Status.ActiveScraper = corev1.ObjectReference{
		Kind:      genericScraperDeployment.Kind,
		Namespace: genericScraperDeployment.Namespace,
		Name:      genericScraperDeployment.Name,
	}
	scraperConfig.Status.ConfigMap = corev1.ObjectReference{
		Kind:      genericScraperConfigMap.Kind,
		Namespace: genericScraperConfigMap.Namespace,
		Name:      genericScraperConfigMap.Name,
	}
	return nil
}

func (r *ScraperConfigReconciler) checkScraperStatus(ctx context.Context, scraperConfig finopsv1.ScraperConfig) error {
	genericExporterConfigMap, err := utils.GetGenericScraperConfigMap(scraperConfig)
	if err != nil {
		return err
	}
	genericExporterDeployment, _ := utils.GetGenericScraperDeployment(scraperConfig)

	err = r.Update(ctx, genericExporterConfigMap)
	if err != nil {
		return err
	}

	err = r.Update(ctx, genericExporterDeployment)
	if err != nil {
		return err
	}

	return nil
}
