package test

import (
	"context"
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/e2e-framework/klient/decoder"
	"sigs.k8s.io/e2e-framework/klient/k8s/resources"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/klient/wait/conditions"
	"sigs.k8s.io/e2e-framework/pkg/env"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/envfuncs"
	"sigs.k8s.io/e2e-framework/pkg/features"
	"sigs.k8s.io/e2e-framework/support/kind"
	e2eutils "sigs.k8s.io/e2e-framework/support/utils"

	operatorscraperapi "github.com/krateoplatformops/finops-operator-scraper/api/v1"
	"github.com/krateoplatformops/finops-operator-scraper/internal/helpers/kube/comparators"
	"github.com/krateoplatformops/finops-operator-scraper/internal/utils"
)

type contextKey string

var (
	testenv env.Environment
)

const (
	testNamespace   = "finops-test" // If you changed this test environment, you need to change the RoleBinding in the "deploymentsPath" folder
	crdsPath        = "../../config/crd/bases"
	deploymentsPath = "./manifests/deployments"
	toTest          = "./manifests/to_test/"

	testName = "exporterscraperconfig-sample" + "-scraper"

	operatorExporterControllerRegistry = "ghcr.io/krateoplatformops"
	operatorExporterControllerTag      = "0.3.2"
	exporterRegistry                   = "ghcr.io/krateoplatformops"
)

func TestMain(m *testing.M) {
	testenv = env.New()
	kindClusterName := "krateo-test"

	// Use pre-defined environment funcs to create a kind cluster prior to test run
	testenv.Setup(
		envfuncs.CreateCluster(kind.NewProvider(), kindClusterName),
		envfuncs.CreateNamespace(testNamespace),
		envfuncs.SetupCRDs(crdsPath, "*"),
		func(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
			// install finops-operator-exporter
			if p := e2eutils.RunCommand(
				fmt.Sprintf("helm install finops-operator-exporter krateo/finops-operator-exporter -n %s --set controllerManager.image.repository=%s/finops-operator-exporter --set image.tag=%s --set imagePullSecrets[0].name=registry-credentials --set image.pullPolicy=Always --set env.REGISTRY=%s", testNamespace, operatorExporterControllerRegistry, operatorExporterControllerTag, exporterRegistry),
			); p.Err() != nil {
				return ctx, p.Err()
			}
			return ctx, nil
		},
	)

	// Use pre-defined environment funcs to teardown kind cluster after tests
	testenv.Finish(
		envfuncs.DeleteNamespace(testNamespace),
		envfuncs.TeardownCRDs(crdsPath, "*"),
		func(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
			// install finops-operator-exporter
			if p := e2eutils.RunCommand(
				fmt.Sprintf("helm uninstall finops-operator-exporter -n %s", testNamespace),
			); p.Err() != nil {
				return ctx, p.Err()
			}
			return ctx, nil
		},
		envfuncs.DestroyCluster(kindClusterName),
	)

	// launch package tests
	os.Exit(testenv.Run(m))
}

func TestScraper(t *testing.T) {
	controllerCreationSig := make(chan bool, 2)
	create := features.New("Create").
		WithLabel("type", "CR and resources").
		Setup(func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			r, err := resources.New(c.Client().RESTConfig())
			if err != nil {
				t.Fail()
			}
			operatorscraperapi.AddToScheme(r.GetScheme())
			r.WithNamespace(testNamespace)

			ctx = context.WithValue(ctx, contextKey("client"), r)

			err = decoder.DecodeEachFile(
				ctx, os.DirFS(deploymentsPath), "*",
				decoder.CreateHandler(r),
				decoder.MutateNamespace(testNamespace),
			)
			if err != nil {
				t.Fatalf("Failed due to error: %s", err)
			}

			err = decoder.DecodeEachFile(
				ctx, os.DirFS(toTest), "*",
				decoder.CreateHandler(r),
				decoder.MutateNamespace(testNamespace),
			)
			if err != nil {
				t.Fatalf("Failed due to error: %s", err)
			}

			if err := wait.For(
				conditions.New(r).DeploymentAvailable("finops-operator-exporter-controller-manager", testNamespace),
				wait.WithTimeout(120*time.Second),
				wait.WithInterval(5*time.Second),
			); err != nil {
				log.Printf("Timed out while waiting for finops-operator-exporter deployment: %s", err)
			}
			if err := wait.For(
				conditions.New(r).DeploymentAvailable("finops-operator-scraper-controller-manager", testNamespace),
				wait.WithTimeout(60*time.Second),
				wait.WithInterval(5*time.Second),
			); err != nil {
				log.Printf("Timed out while waiting for finops-operator-scraper deployment: %s", err)
			}
			controllerCreationSig <- true
			return ctx
		}).
		Assess("CR", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			r := ctx.Value(contextKey("client")).(*resources.Resources)

			crGet := &operatorscraperapi.ScraperConfig{}
			err := r.Get(ctx, testName, testNamespace, crGet)
			if err != nil {
				t.Fatal(err)
			}
			return ctx
		}).
		Assess("Resources", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			r := ctx.Value(contextKey("client")).(*resources.Resources)

			deployment := &appsv1.Deployment{}
			configmap := &corev1.ConfigMap{}

			select {
			case <-time.After(180 * time.Second):
				t.Fatal("Timed out wating for controller creation")
			case created := <-controllerCreationSig:
				if !created {
					t.Fatal("Operator deployment not ready")
				}
			}

			err := r.Get(ctx, testName+"-deployment", testNamespace, deployment)
			if err != nil {
				t.Fatal(err)
			}

			err = r.Get(ctx, testName+"-configmap", testNamespace, configmap)
			if err != nil {
				t.Fatal(err)
			}

			return ctx
		}).Feature()

	delete := features.New("Delete").
		WithLabel("type", "Resources").
		Setup(func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			r, err := resources.New(c.Client().RESTConfig())
			if err != nil {
				t.Fail()
			}
			operatorscraperapi.AddToScheme(r.GetScheme())
			r.WithNamespace(testNamespace)

			ctx = context.WithValue(ctx, contextKey("client"), r)

			return ctx
		}).
		Assess("Deployment", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			r := ctx.Value(contextKey("client")).(*resources.Resources)

			resource := &appsv1.Deployment{
				ObjectMeta: v1.ObjectMeta{
					Name:      testName + "-deployment",
					Namespace: testNamespace,
				},
			}
			err := r.Delete(ctx, resource)
			if err != nil {
				t.Fatal(err)
			}

			time.Sleep(5 * time.Second) // Wait for informer to re-create

			err = r.Get(ctx, testName+"-deployment", testNamespace, resource)
			if err != nil {
				t.Fatal(err)
			}
			return ctx
		}).
		Assess("ConfigMap", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			r := ctx.Value(contextKey("client")).(*resources.Resources)

			resource := &corev1.ConfigMap{
				ObjectMeta: v1.ObjectMeta{
					Name:      testName + "-configmap",
					Namespace: testNamespace,
				},
			}
			err := r.Delete(ctx, resource)
			if err != nil {
				t.Fatal(err)
			}

			time.Sleep(5 * time.Second) // Wait for informer to re-create

			err = r.Get(ctx, testName+"-configmap", testNamespace, resource)
			if err != nil {
				t.Fatal(err)
			}
			return ctx
		}).Feature()

	modify := features.New("Modify").
		WithLabel("type", "Resources").
		Setup(func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			r, err := resources.New(c.Client().RESTConfig())
			if err != nil {
				t.Fail()
			}
			operatorscraperapi.AddToScheme(r.GetScheme())
			r.WithNamespace(testNamespace)

			ctx = context.WithValue(ctx, contextKey("client"), r)

			return ctx
		}).
		Assess("Deployment", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			r := ctx.Value(contextKey("client")).(*resources.Resources)

			exporterScraperConfig := &operatorscraperapi.ScraperConfig{}
			err := r.Get(ctx, testName, testNamespace, exporterScraperConfig)
			if err != nil {
				t.Fatal(err)
			}
			// This is necessary because it does not get compiled automatically by the GET
			exporterScraperConfig.TypeMeta.Kind = "ScraperConfig"
			exporterScraperConfig.TypeMeta.APIVersion = "finops.krateo.io/v1"

			resource := &appsv1.Deployment{}
			err = r.Get(ctx, testName+"-deployment", testNamespace, resource)
			if err != nil {
				t.Fatal(err)
			}

			resource.Spec.Replicas = utils.Int32Ptr(2)

			err = r.Update(ctx, resource)
			if err != nil {
				t.Fatal(err)
			}

			time.Sleep(5 * time.Second) // Wait for informer to restore

			resource = &appsv1.Deployment{}
			err = r.Get(ctx, testName+"-deployment", testNamespace, resource)
			if err != nil {
				t.Fatal(err)
			}

			if !comparators.CheckDeployment(*resource, *exporterScraperConfig) {
				t.Fatal("Deployment not restored by informer")
			}
			return ctx
		}).
		Assess("ConfigMap", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			r := ctx.Value(contextKey("client")).(*resources.Resources)

			scraperConfig := &operatorscraperapi.ScraperConfig{}
			err := r.Get(ctx, testName, testNamespace, scraperConfig)
			if err != nil {
				t.Fatal(err)
			}
			// This is necessary because it does not get compiled automatically by the GET
			scraperConfig.TypeMeta.Kind = "ScraperConfig"
			scraperConfig.TypeMeta.APIVersion = "finops.krateo.io/v1"

			resource := &corev1.ConfigMap{}
			err = r.Get(ctx, testName+"-configmap", testNamespace, resource)
			if err != nil {
				t.Fatal(err)
			}

			resource.BinaryData["config.yaml"] = []byte("broken")

			err = r.Update(ctx, resource)
			if err != nil {
				t.Fatal(err)
			}

			time.Sleep(5 * time.Second) // Wait for informer to restore

			resource = &corev1.ConfigMap{}
			err = r.Get(ctx, testName+"-configmap", testNamespace, resource)
			if err != nil {
				t.Fatal(err)
			}

			if !comparators.CheckConfigMap(*resource, *scraperConfig) {
				t.Fatal("ConfigMap not restored by informer")
			}
			return ctx
		}).Feature()

	// test feature
	testenv.Test(t, create, delete, modify)
}
