package utils

import (
	"os"

	"gopkg.in/yaml.v3"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	finopsv1 "operator-scraper/api/v1"
)

var repository = os.Getenv("REPO")

type ScraperConfigFile struct {
	DatabaseConfigRef DatabaseConfigRef `yaml:"databaseConfigRef"`
	Exporter          Exporter          `yaml:"exporter"`
}

type DatabaseConfigRef struct {
	Name      string `yaml:"name"`
	Namespace string `yaml:"namespace"`
}

type Exporter struct {
	Url                  string `yaml:"url"`
	PollingIntervalHours int    `yaml:"pollingIntervalHours"`
	TableName            string `yaml:"tableName"`
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
							Image:           repository + "/finops-prometheus-scraper-generic:0.1.0",
							ImagePullPolicy: corev1.PullAlways,
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "config-volume",
									MountPath: "/config",
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
							Name: "registry-credentials-default",
						},
					},
				},
			},
		},
	}, nil
}

func GetGenericScraperConfigMap(scraperConfig finopsv1.ScraperConfig) (*corev1.ConfigMap, error) {
	scraperConfigFile := ScraperConfigFile{}
	databaseConfigRef := DatabaseConfigRef{}
	exporter := Exporter{}

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
