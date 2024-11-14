package utils

import (
	"os"
	"strings"

	"gopkg.in/yaml.v3"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	finopsDataTypes "github.com/krateoplatformops/finops-data-types/api/v1"
	finopsv1 "github.com/krateoplatformops/finops-operator-scraper/api/v1"
)

type ScraperConfigFile struct {
	DatabaseConfigRef finopsDataTypes.ObjectRef `yaml:"databaseConfigRef"`
	Exporter          Exporter                  `yaml:"exporter"`
}

type Exporter struct {
	Url                  string `yaml:"url"`
	PollingIntervalHours int    `yaml:"pollingIntervalHours"`
	TableName            string `yaml:"tableName"`
	MetricType           string `yaml:"metricType"`
}

func int32Ptr(i int32) *int32 { return &i }

func GetGenericScraperDeployment(scraperConfig finopsv1.ScraperConfig) (*appsv1.Deployment, error) {
	return &appsv1.Deployment{
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
			Replicas: int32Ptr(1),
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
							Image:           strings.TrimSuffix(os.Getenv("REGISTRY"), "/") + "/finops-prometheus-scraper-generic:latest",
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
	}, nil
}

func GetGenericScraperConfigMap(scraperConfig finopsv1.ScraperConfig) (*corev1.ConfigMap, error) {
	scraperConfigFile := ScraperConfigFile{}
	databaseConfigRef := finopsDataTypes.ObjectRef{}
	exporter := Exporter{}

	exporter.MetricType = scraperConfig.Spec.MetricType
	exporter.Url = scraperConfig.Spec.Url
	exporter.PollingIntervalHours = scraperConfig.Spec.PollingIntervalHours
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
	return &corev1.ConfigMap{
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
	}, nil
}
