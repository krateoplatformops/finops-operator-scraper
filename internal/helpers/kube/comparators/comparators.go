package comparators

import (
	"strings"

	finopsDataTypes "github.com/krateoplatformops/finops-data-types/api/v1"
	finopsv1 "github.com/krateoplatformops/finops-operator-scraper/api/v1"
	"github.com/krateoplatformops/finops-operator-scraper/internal/utils"
	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

func CheckConfigMap(configMap corev1.ConfigMap, scraperConfig finopsv1.ScraperConfig) bool {
	if configMap.Name != scraperConfig.Name+"-configmap" {
		return false
	}

	scraperConfigFile := utils.ScraperConfigFile{}
	databaseConfigRef := finopsDataTypes.ObjectRef{}
	exporter := utils.Exporter{}

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
		return false
	}

	if yamlDataFromLive, ok := configMap.BinaryData["config.yaml"]; ok {
		if strings.Replace(string(yamlData), " ", "", -1) != strings.Replace(string(yamlDataFromLive), " ", "", -1) {
			log.Logger.Info().Msg("bytes different")
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

func CheckDeployment(deployment appsv1.Deployment, scraperConfig finopsv1.ScraperConfig) bool {
	if deployment.Name != scraperConfig.Name+"-deployment" {
		log.Logger.Debug().Msg("Name does not respect naming convention")
		return false
	}

	ownerReferencesLive := deployment.OwnerReferences
	if len(ownerReferencesLive) != 1 {
		log.Logger.Debug().Msg("Owner reference length not one")
		return false
	}

	if ownerReferencesLive[0].Kind != scraperConfig.Kind ||
		ownerReferencesLive[0].Name != scraperConfig.Name ||
		ownerReferencesLive[0].UID != scraperConfig.UID ||
		ownerReferencesLive[0].APIVersion != scraperConfig.APIVersion {
		log.Logger.Debug().Msg("Owner reference wrong")
		return false
	}

	if *deployment.Spec.Replicas != 1 {
		log.Logger.Debug().Msgf("Replicas not one: %s = %d", "replicas", deployment.Spec.Replicas)
		return false
	}

	if len(deployment.Spec.Selector.MatchLabels) == 0 {
		log.Logger.Debug().Msg("Selector not found")
		return false
	} else if deployment.Spec.Selector.MatchLabels["scraper"] != scraperConfig.Name {
		log.Logger.Debug().Msg("Selector label scraper not equal to config name")
		return false
	}

	if len(deployment.Spec.Template.ObjectMeta.Labels) == 0 {
		log.Logger.Debug().Msg("No labels found")
		return false
	} else if deployment.Spec.Template.ObjectMeta.Labels["scraper"] != scraperConfig.Name {
		log.Logger.Debug().Msg("Label scraper not equal to config name")
		return false
	}

	if len(deployment.Spec.Template.Spec.Containers) != 1 {
		log.Logger.Debug().Msg("Container not equal to 1")
		return false
	} else {
		if len(deployment.Spec.Template.Spec.Containers[0].VolumeMounts) == 0 {
			log.Logger.Debug().Msg("No volume mount found")
			return false
		} else {
			found := false
			for _, volumeMount := range deployment.Spec.Template.Spec.Containers[0].VolumeMounts {
				if volumeMount.Name == "config-volume" && volumeMount.MountPath == "/config" {
					found = true
				}
			}
			if !found {
				log.Logger.Debug().Msg("Volume mount not found")
				return false
			}
		}

		if len(deployment.Spec.Template.Spec.Volumes) == 0 {
			log.Logger.Debug().Msg("No volumes found")
			return false
		} else {
			found := false
			for _, volume := range deployment.Spec.Template.Spec.Volumes {
				if volume.Name == "config-volume" && volume.VolumeSource.ConfigMap.LocalObjectReference.Name == scraperConfig.Name+"-configmap" {
					found = true
				}
			}
			if !found {
				log.Logger.Debug().Msg("Volume not found")
				return false
			}
		}
	}

	// Container image and secret name are not checked on purpose, since they may need to be different from the default values

	return true
}
