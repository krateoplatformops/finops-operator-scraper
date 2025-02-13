package test

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"os"

	"testing"
	"time"

	"github.com/rs/zerolog/log"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	operatorlogger "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
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
	"github.com/krateoplatformops/provider-runtime/pkg/controller"
	"github.com/krateoplatformops/provider-runtime/pkg/logging"
	"github.com/krateoplatformops/provider-runtime/pkg/ratelimiter"

	operatorscraper "github.com/krateoplatformops/finops-operator-scraper/internal/controller"
	clientHelper "github.com/krateoplatformops/finops-operator-scraper/internal/helpers/kube/client"
	informer_factory "github.com/krateoplatformops/finops-operator-scraper/internal/informers"
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
	operatorExporterControllerTag      = "0.4.0"
	exporterRegistry                   = "ghcr.io/krateoplatformops"
	exporterVersion                    = "0.4.0"
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
			if p := e2eutils.RunCommand("helm repo add krateo https://charts.krateo.io"); p.Err() != nil {
				return ctx, fmt.Errorf("helm error while adding repository: %s %v", p.Out(), p.Err())
			}

			if p := e2eutils.RunCommand("helm repo update krateo"); p.Err() != nil {
				return ctx, fmt.Errorf("helm error while updating helm: %s %v", p.Out(), p.Err())
			}

			if p := e2eutils.RunCommand(
				fmt.Sprintf("helm install finops-operator-exporter krateo/finops-operator-exporter -n %s --set controllerManager.image.repository=%s/finops-operator-exporter --set image.tag=%s --set imagePullSecrets[0].name=registry-credentials --set image.pullPolicy=Always --set env.REGISTRY=%s --set env.SCRAPER_VERSION=%s", testNamespace, operatorExporterControllerRegistry, operatorExporterControllerTag, exporterRegistry, exporterVersion),
			); p.Err() != nil {
				return ctx, fmt.Errorf("helm error while installing chart: %s %v", p.Out(), p.Err())
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
	mgrCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	controllerCreationSig := make(chan bool, 1)
	create := features.New("Create").
		WithLabel("type", "CR and resources").
		Setup(func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			r, err := resources.New(c.Client().RESTConfig())
			if err != nil {
				t.Fail()
			}

			operatorscraperapi.AddToScheme(r.GetScheme())
			r.WithNamespace(testNamespace)

			// Start the controller manager
			err = startTestManager(mgrCtx, r.GetScheme())
			if err != nil {
				t.Fatal(err)
			}

			ctx = context.WithValue(ctx, contextKey("client"), r)

			err = decoder.DecodeEachFile(
				ctx, os.DirFS(deploymentsPath), "*",
				decoder.CreateHandler(r),
				decoder.MutateNamespace(testNamespace),
			)
			if err != nil {
				t.Fatalf("Failed due to error: %s", err)
			}

			if err := wait.For(
				conditions.New(r).DeploymentAvailable("webservice-api-mock-deployment", testNamespace),
				wait.WithTimeout(120*time.Second),
				wait.WithInterval(5*time.Second),
			); err != nil {
				t.Fatal(fmt.Errorf("timed out while waiting for webservice-api-mock-deployment: %w", err))
			}

			// Create test resources
			err = decoder.DecodeEachFile(
				ctx, os.DirFS(toTest), "*",
				decoder.CreateHandler(r),
				decoder.MutateNamespace(testNamespace),
			)
			if err != nil {
				t.Fatalf("Failed due to error: %s", err)
			}

			if err := wait.For(
				conditions.New(r).DeploymentAvailable("finops-operator-exporter", testNamespace),
				wait.WithTimeout(120*time.Second),
				wait.WithInterval(5*time.Second),
			); err != nil {
				log.Printf("Timed out while waiting for finops-operator-exporter deployment: %s", err)
			}

			time.Sleep(5 * time.Second)

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

			crGet := &operatorscraperapi.ScraperConfig{}
			err = r.Get(ctx, testName, testNamespace, crGet)
			if err != nil {
				t.Fatal(err)
			}

			if crGet.Status.ActiveScraper.Name == "" || crGet.Status.ConfigMap.Name == "" {
				t.Fatal(fmt.Errorf("missing status update"))
			}

			return ctx
		}).
		Assess("ConfigCorrect", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			r := ctx.Value(contextKey("client")).(*resources.Resources)

			if err := wait.For(
				conditions.New(r).DeploymentAvailable(testName+"-deployment", testNamespace),
				wait.WithTimeout(120*time.Second),
				wait.WithInterval(5*time.Second),
			); err != nil {
				t.Fatal(fmt.Errorf("timed out while waiting for %s-deployment: %w", testName, err))
			}

			time.Sleep(20 * time.Second) // Give the container time to finish startup and error out

			pods := &corev1.PodList{}
			r.List(ctx, pods, resources.WithLabelSelector("scraper="+testName))
			if len(pods.Items) > 1 {
				t.Fatal(fmt.Errorf("number of pods created for config more than 1: %d", len(pods.Items)))
			}

			for _, condition := range pods.Items[0].Status.Conditions {
				if condition.Type == corev1.PodReady {
					if condition.Status != corev1.ConditionTrue {
						t.Fatal(fmt.Errorf("pod %s is not ready: %s %s", pods.Items[0].Name, condition.Type, condition.Status))
					} else {
						for _, container := range pods.Items[0].Status.ContainerStatuses {
							if container.Name == "scraper" {
								if container.RestartCount > 1 {
									t.Fatal(fmt.Errorf("pod %s has restarts: %d", pods.Items[0].Name, container.RestartCount))
								}
							}
						}
					}
				}
			}

			return ctx
		}).Feature()

	delete := features.New("Delete").
		WithLabel("type", "Resources").
		Setup(func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			return ctx
		}).
		Assess("Deployment", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			r := ctx.Value(contextKey("client")).(*resources.Resources)

			resource := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
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
				ObjectMeta: metav1.ObjectMeta{
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

func startTestManager(ctx context.Context, scheme *runtime.Scheme) error {
	os.Setenv("REGISTRY", "ghcr.io/krateoplatformops")
	os.Setenv("REGISTRY_CREDENTIALS", "registry-credentials")
	os.Setenv("SCRAPER_VERSION", "0.4.0")
	os.Setenv("URL_DB_WEBSERVICE", "http://finops-database-handler."+testNamespace+":8088")

	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var secureMetrics bool
	var enableHTTP2 bool
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", false,
		"If set the metrics endpoint is served securely")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancelation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		c.NextProtos = []string{"http/1.1"}
	}

	tlsOpts := []func(*tls.Config){}
	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	webhookServer := webhook.NewServer(webhook.Options{
		TLSOpts: tlsOpts,
	})

	namespaceCacheConfigMap := make(map[string]cache.Config)
	namespaceCacheConfigMap[testNamespace] = cache.Config{}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress:   metricsAddr,
			SecureServing: secureMetrics,
			TLSOpts:       tlsOpts,
		},
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "950d638b.krateo.io",
		Cache:                  cache.Options{DefaultNamespaces: namespaceCacheConfigMap},
	})
	if err != nil {
		os.Exit(1)
	}

	gv_apps := schema.GroupVersion{
		Group:   "apps",
		Version: "v1",
	}
	gv_core := schema.GroupVersion{
		Group:   "",
		Version: "v1",
	}
	cfg := ctrl.GetConfigOrDie()

	dynClient, err := clientHelper.New(cfg)
	if err != nil {
		os.Exit(1)
	}
	informerFactory := informer_factory.InformerFactory{
		DynClient: dynClient,
		Logger:    logging.NewLogrLogger(operatorlogger.Log.WithName("operator-scraper-informer")),
	}

	informerFactory.StartInformer(testNamespace, gv_apps.WithResource("deployments"))
	informerFactory.StartInformer(testNamespace, gv_core.WithResource("configmaps"))

	o := controller.Options{
		Logger:                  logging.NewLogrLogger(operatorlogger.Log.WithName("operator-scraper")),
		MaxConcurrentReconciles: 1,
		PollInterval:            100,
		GlobalRateLimiter:       ratelimiter.NewGlobal(1),
	}

	if err := operatorscraper.Setup(mgr, o); err != nil {
		return err
	}

	go func() {
		if err := mgr.Start(ctx); err != nil {
			log.Error().Err(err).Msg("Failed to start manager")
		}
	}()

	return nil
}
