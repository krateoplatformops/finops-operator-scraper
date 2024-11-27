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
	"errors"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	finopsDataTypes "github.com/krateoplatformops/finops-data-types/api/v1"
	finopsv1 "github.com/krateoplatformops/finops-operator-scraper/api/v1"
	prv1 "github.com/krateoplatformops/provider-runtime/apis/common/v1"
	"github.com/krateoplatformops/provider-runtime/pkg/controller"
	"github.com/krateoplatformops/provider-runtime/pkg/event"
	"github.com/krateoplatformops/provider-runtime/pkg/logging"
	"github.com/krateoplatformops/provider-runtime/pkg/ratelimiter"
	"github.com/krateoplatformops/provider-runtime/pkg/reconciler"
	"github.com/krateoplatformops/provider-runtime/pkg/resource"

	clientHelper "github.com/krateoplatformops/finops-operator-scraper/internal/helpers/kube/client"
	"github.com/krateoplatformops/finops-operator-scraper/internal/helpers/kube/comparators"
	utils "github.com/krateoplatformops/finops-operator-scraper/internal/utils"
)

const (
	errNotScraperConfig = "managed resource is not an scraper config custom resource"
)

//+kubebuilder:rbac:groups=finops.krateo.io,namespace=finops,resources=scraperconfigs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=finops.krateo.io,namespace=finops,resources=scraperconfigs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=finops.krateo.io,namespace=finops,resources=scraperconfigs/finalizers,verbs=update
//+kubebuilder:rbac:groups=finops.krateo.io,namespace=finops,resources=scraperconfigs,verbs=get;create;update
//+kubebuilder:rbac:groups=finops.krateo.io,namespace=finops,resources=databaseconfigs,verbs=get;create;update
//+kubebuilder:rbac:groups=apps,namespace=finops,resources=deployments,verbs=get;create;delete;list;update;watch
//+kubebuilder:rbac:groups=core,namespace=finops,resources=configmaps,verbs=get;create;delete;list;update

func Setup(mgr ctrl.Manager, o controller.Options) error {
	name := reconciler.ControllerName(finopsv1.GroupKind)

	log := o.Logger.WithValues("controller", name)
	log.Info("controller", "name", name)

	recorder := mgr.GetEventRecorderFor(name)

	r := reconciler.NewReconciler(mgr,
		resource.ManagedKind(finopsv1.GroupVersionKind),
		reconciler.WithExternalConnecter(&connector{
			log:          log,
			recorder:     recorder,
			pollInterval: o.PollInterval,
		}),
		reconciler.WithPollInterval(o.PollInterval),
		reconciler.WithLogger(log),
		reconciler.WithRecorder(event.NewAPIRecorder(recorder)))

	log.Debug("polling rate", "rate", o.PollInterval)

	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		WithOptions(o.ForControllerRuntime()).
		For(&finopsv1.ScraperConfig{}).
		Complete(ratelimiter.New(name, r, o.GlobalRateLimiter))
}

type connector struct {
	pollInterval time.Duration
	log          logging.Logger
	recorder     record.EventRecorder
}

func (c *connector) Connect(ctx context.Context, mg resource.Managed) (reconciler.ExternalClient, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve rest.InClusterConfig: %w", err)
	}

	dynClient, err := clientHelper.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("unable to create dynamic client: %w", err)
	}

	return &external{
		cfg:             cfg,
		dynClient:       dynClient,
		sinceLastUpdate: make(map[string]time.Time),
		pollInterval:    c.pollInterval,
		log:             c.log,
		rec:             c.recorder,
	}, nil
}

type external struct {
	cfg             *rest.Config
	dynClient       *dynamic.DynamicClient
	sinceLastUpdate map[string]time.Time
	pollInterval    time.Duration
	log             logging.Logger
	rec             record.EventRecorder
}

func (c *external) Disconnect(_ context.Context) error {
	return nil // NOOP
}

func (e *external) Observe(ctx context.Context, mg resource.Managed) (reconciler.ExternalObservation, error) {
	scraperConfig, ok := mg.(*finopsv1.ScraperConfig)
	if !ok {
		return reconciler.ExternalObservation{}, errors.New(errNotScraperConfig)
	}

	// Check if a deployment for this configuration already exists
	existingObjDeployment, err := clientHelper.GetObj(ctx,
		&finopsDataTypes.ObjectRef{
			Name:      scraperConfig.Status.ActiveScraper.Name,
			Namespace: scraperConfig.Status.ActiveScraper.Namespace},
		"apps/v1", "deployments", e.dynClient)
	if err != nil {
		e.rec.Eventf(scraperConfig, corev1.EventTypeWarning, "could not get scraperconfig deployment", "object name: %s", scraperConfig.Status.ActiveScraper.Name)
		return reconciler.ExternalObservation{
			ResourceExists: false,
		}, nil
	}

	// ConfigMap status objRef and pointer for GET
	existingObjConfigMap, err := clientHelper.GetObj(ctx,
		&finopsDataTypes.ObjectRef{
			Name:      scraperConfig.Status.ConfigMap.Name,
			Namespace: scraperConfig.Status.ConfigMap.Namespace},
		"v1", "configmaps", e.dynClient)
	if err != nil {
		e.rec.Eventf(scraperConfig, corev1.EventTypeWarning, "could not get scraperconfig configmap", "object name: %s", scraperConfig.Status.ConfigMap.Name)
		return reconciler.ExternalObservation{
			ResourceExists: false,
		}, nil
	}

	// Check if all elements of the deployment exist
	// If any the objects does not exist, something happend, reconcile spec-status
	if existingObjDeployment.GetName() == "" || existingObjConfigMap.GetName() == "" {
		return reconciler.ExternalObservation{
			ResourceExists: false,
		}, nil
	} else {
		status, notUpToDate, err := checkScraperStatus(ctx, scraperConfig, e.dynClient)
		if err != nil {
			return reconciler.ExternalObservation{
				ResourceExists: false,
			}, fmt.Errorf("could not check scraper status: %v", err)
		}
		scraperConfig.SetConditions(prv1.Available())
		if !status {
			e.rec.Eventf(scraperConfig, corev1.EventTypeWarning, "scraper resources not up-to-date", "object name: %s - first not up-to-date: %s", scraperConfig.Name, notUpToDate)
			return reconciler.ExternalObservation{
				ResourceExists:   true,
				ResourceUpToDate: false,
			}, nil
		}
		return reconciler.ExternalObservation{
			ResourceExists:   true,
			ResourceUpToDate: true,
		}, nil
	}
}

func (e *external) Create(ctx context.Context, mg resource.Managed) error {
	scraperConfig, ok := mg.(*finopsv1.ScraperConfig)
	if !ok {
		return errors.New(errNotScraperConfig)
	}

	scraperConfig.SetConditions(prv1.Creating())

	err := createScraperFromScratch(ctx, scraperConfig, e.dynClient)
	if err != nil {
		return err
	}

	e.rec.Eventf(scraperConfig, corev1.EventTypeNormal, "Completed create", "object name: %s", scraperConfig.Name)

	return nil
}

func (e *external) Update(ctx context.Context, mg resource.Managed) error {
	scraperConfig, ok := mg.(*finopsv1.ScraperConfig)
	if !ok {
		return errors.New(errNotScraperConfig)
	}

	genericScraperConfigMap, err := utils.GetGenericScraperConfigMap(scraperConfig)
	if err != nil {
		return err
	}
	genericScraperConfigMapUnstructured, err := clientHelper.ToUnstructured(genericScraperConfigMap)
	if err != nil {
		return err
	}

	genericScraperDeployment, _ := utils.GetGenericScraperDeployment(scraperConfig)
	genericScraperDeploymentUnstructured, err := clientHelper.ToUnstructured(genericScraperDeployment)
	if err != nil {
		return err
	}

	err = clientHelper.UpdateObj(ctx, genericScraperConfigMapUnstructured, "configmaps", e.dynClient)
	if err != nil {
		return err
	}

	err = clientHelper.UpdateObj(ctx, genericScraperDeploymentUnstructured, "deployments", e.dynClient)
	if err != nil {
		return err
	}

	e.rec.Eventf(scraperConfig, corev1.EventTypeNormal, "Completed update", "object name: %s", scraperConfig.Name)
	e.sinceLastUpdate[scraperConfig.Name+scraperConfig.Namespace] = time.Now()

	return nil
}

func (e *external) Delete(ctx context.Context, mg resource.Managed) error {
	scraperConfig, ok := mg.(*finopsv1.ScraperConfig)
	if !ok {
		return errors.New(errNotScraperConfig)
	}

	scraperConfig.SetConditions(prv1.Deleting())

	err := clientHelper.DeleteObj(ctx, &finopsDataTypes.ObjectRef{Name: scraperConfig.Name + "-deployment", Namespace: scraperConfig.Namespace}, "apps/v1", "deployments", e.dynClient)
	if err != nil {
		return fmt.Errorf("error while delete Deployment %v", err)
	}

	err = clientHelper.DeleteObj(ctx, &finopsDataTypes.ObjectRef{Name: scraperConfig.Name + "-configmap", Namespace: scraperConfig.Namespace}, "v1", "configmaps", e.dynClient)
	if err != nil {
		return fmt.Errorf("error while delete ConfigMap %v", err)
	}

	err = clientHelper.DeleteObj(ctx, &finopsDataTypes.ObjectRef{Name: scraperConfig.Name + "-service", Namespace: scraperConfig.Namespace}, "v1", "services", e.dynClient)
	if err != nil {
		return fmt.Errorf("error while delete Service %v", err)
	}

	e.rec.Eventf(scraperConfig, corev1.EventTypeNormal, "Received delete event", "removed deployment, configmap and service objects")
	return nil
}

func createScraperFromScratch(ctx context.Context, scraperConfig *finopsv1.ScraperConfig, dynClient *dynamic.DynamicClient) error {
	var err error
	// Create the ConfigMap
	genericScraperConfigMap, err := utils.GetGenericScraperConfigMap(scraperConfig)
	if err != nil {
		return err
	}
	genericScraperConfigMapUnstructured, err := clientHelper.ToUnstructured(genericScraperConfigMap)
	if err != nil {
		return err
	}
	err = clientHelper.CreateObj(ctx, genericScraperConfigMapUnstructured, "configmaps", dynClient)
	if err != nil {
		return fmt.Errorf("error while creating configmap: %v", err)
	}

	// Create the generic scraper deployment
	genericScraperDeployment, _ := utils.GetGenericScraperDeployment(scraperConfig)
	genericScraperDeploymentUnstructured, err := clientHelper.ToUnstructured(genericScraperDeployment)
	if err != nil {
		return err
	}
	err = clientHelper.CreateObj(ctx, genericScraperDeploymentUnstructured, "deployments", dynClient)
	if err != nil {
		return fmt.Errorf("error while creating deployment: %v", err)
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

	scraperConfigUnstructured, err := clientHelper.ToUnstructured(scraperConfig)
	if err != nil {
		return err
	}
	return clientHelper.UpdateStatus(ctx, scraperConfigUnstructured, "scraperconfigs", dynClient)
}

func checkScraperStatus(ctx context.Context, scraperConfig *finopsv1.ScraperConfig, dynClient *dynamic.DynamicClient) (bool, string, error) {
	// Check if a deployment for this configuration already exists
	existingObjDeploymentUnstructured, err := clientHelper.GetObj(ctx,
		&finopsDataTypes.ObjectRef{
			Name:      scraperConfig.Status.ActiveScraper.Name,
			Namespace: scraperConfig.Status.ActiveScraper.Namespace},
		"apps/v1", "deployments", dynClient)
	if err != nil {
		return false, "", fmt.Errorf("could not obtain scraper deployment: %v", err)
	}
	existingObjDeployment := &appsv1.Deployment{}
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(existingObjDeploymentUnstructured.Object, existingObjDeployment)
	if err != nil {
		return false, "", err
	}
	if !comparators.CheckDeployment(*existingObjDeployment, *scraperConfig) {
		return false, "Deployment", nil
	}

	// ConfigMap status objRef
	existingObjConfigMapUnstructured, err := clientHelper.GetObj(ctx,
		&finopsDataTypes.ObjectRef{
			Name:      scraperConfig.Status.ConfigMap.Name,
			Namespace: scraperConfig.Status.ConfigMap.Namespace},
		"v1", "configmaps", dynClient)
	if err != nil {
		return false, "", fmt.Errorf("could not obtain scraper configmap: %v", err)
	}
	existingObjConfigMap := &corev1.ConfigMap{}
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(existingObjConfigMapUnstructured.Object, existingObjConfigMap)
	if err != nil {
		return false, "", err
	}
	if !comparators.CheckConfigMap(*existingObjConfigMap, *scraperConfig) {
		return false, "ConfigMap", nil
	}

	return true, "", nil
}
