package utils

import (
	"os"
	"strings"

	"gopkg.in/yaml.v3"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	finopsdatatypes "github.com/krateoplatformops/finops-data-types/api/v1"
	finopsv1 "github.com/krateoplatformops/finops-operator-scraper/api/v1"
)

type ScraperConfigFile struct {
	DatabaseConfigRef finopsdatatypes.ObjectRef `yaml:"databaseConfigRef"`
	Exporter          Exporter                  `yaml:"exporter"`
}

type Exporter struct {
	API             finopsdatatypes.API `yaml:"api"`
	PollingInterval metav1.Duration     `yaml:"pollingInterval"`
	TableName       string              `yaml:"tableName"`
	MetricType      string              `yaml:"metricType"`
}

func Int32Ptr(i int32) *int32 { return &i }

func GetGenericScraperDeployment(scraperConfig *finopsv1.ScraperConfig) (*appsv1.Deployment, error) {
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      scraperConfig.Name + "-deployment",
			Namespace: scraperConfig.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: scraperConfig.APIVersion,
					Kind:       scraperConfig.Kind,
					Name:       scraperConfig.Name,
					UID:        scraperConfig.UID,
				},
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: Int32Ptr(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"scraper": scraperConfig.Name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"scraper": scraperConfig.Name,
					},
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: "db-config-getter-sa",
					Containers: []corev1.Container{
						{
							Name:            "scraper",
							Image:           strings.TrimSuffix(os.Getenv("REGISTRY"), "/") + "/finops-prometheus-scraper-generic:" + strings.TrimSuffix(os.Getenv("SCRAPER_VERSION"), "latest"),
							ImagePullPolicy: corev1.PullAlways,
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "config-volume",
									MountPath: "/config",
								},
							},
							Env: []corev1.EnvVar{
								{
									Name:  "URL_DB_WEBSERVICE",
									Value: os.Getenv("URL_DB_WEBSERVICE"),
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "config-volume",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: scraperConfig.Name + "-configmap",
									},
								},
							},
						},
					},
					ImagePullSecrets: []corev1.LocalObjectReference{
						{
							Name: os.Getenv("REGISTRY_CREDENTIALS"),
						},
					},
				},
			},
		},
	}
	deployment.APIVersion = "apps/v1"
	return deployment, nil
}

func GetGenericScraperConfigMap(scraperConfig *finopsv1.ScraperConfig) (*corev1.ConfigMap, error) {
	scraperConfigFile := ScraperConfigFile{}
	databaseConfigRef := finopsdatatypes.ObjectRef{}
	exporter := Exporter{}

	exporter.MetricType = scraperConfig.Spec.MetricType
	exporter.API = scraperConfig.Spec.API
	exporter.PollingInterval = scraperConfig.Spec.PollingInterval
	exporter.TableName = scraperConfig.Spec.TableName

	databaseConfigRef.Name = scraperConfig.Spec.ScraperDatabaseConfigRef.Name
	databaseConfigRef.Namespace = scraperConfig.Spec.ScraperDatabaseConfigRef.Namespace

	scraperConfigFile.DatabaseConfigRef = databaseConfigRef
	scraperConfigFile.Exporter = exporter

	yamlData, err := yaml.Marshal(scraperConfigFile)
	if err != nil {
		return &corev1.ConfigMap{}, err
	}

	binaryData := make(map[string][]byte)
	binaryData["config.yaml"] = yamlData
	configmap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      scraperConfig.Name + "-configmap",
			Namespace: scraperConfig.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: scraperConfig.APIVersion,
					Kind:       scraperConfig.Kind,
					Name:       scraperConfig.Name,
					UID:        scraperConfig.UID,
				},
			},
		},
		BinaryData: binaryData,
	}
	configmap.APIVersion = "v1"
	return configmap, nil
}
