package comparators_test

import (
	"testing"

	finopsDataTypes "github.com/krateoplatformops/finops-data-types/api/v1"
	finopsv1 "github.com/krateoplatformops/finops-operator-scraper/api/v1"
	"github.com/krateoplatformops/finops-operator-scraper/internal/helpers/kube/comparators"
	utils "github.com/krateoplatformops/finops-operator-scraper/internal/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func baseScraperConfig() *finopsv1.ScraperConfig {
	return &finopsv1.ScraperConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "finops.krateo.io/v1",
			Kind:       "ScraperConfig",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-scraper",
			Namespace: "krateo-system",
			UID:       types.UID("test-uid-1234"),
		},
		Spec: finopsDataTypes.ScraperConfigSpec{
			TableName:       "test_table",
			MetricType:      "cost",
			PollingInterval: metav1.Duration{},
			ScraperDatabaseConfigRef: finopsDataTypes.ObjectRef{
				Name:      "db-config",
				Namespace: "krateo-system",
			},
		},
	}
}

// buildConfigMapFromScraper replicates what GetGenericScraperConfigMap does,
// so the test is not coupled to that function's implementation.
func buildConfigMapFromScraper(t *testing.T, scraperConfig *finopsv1.ScraperConfig) corev1.ConfigMap {
	t.Helper()
	cm, err := utils.GetGenericScraperConfigMap(scraperConfig)
	if err != nil {
		t.Fatalf("failed to build configmap: %v", err)
	}
	return *cm
}

// TestCheckConfigMap_WithoutGeneric_Passes verifies that when the ScraperConfig
// has no Generic field, the comparator correctly identifies the ConfigMap as up-to-date.
func TestCheckConfigMap_WithoutGeneric_Passes(t *testing.T) {
	scraperConfig := baseScraperConfig()
	// Generic is nil — no mismatch expected

	cm := buildConfigMapFromScraper(t, scraperConfig)

	result := comparators.CheckConfigMap(cm, *scraperConfig)
	if !result {
		t.Error("expected CheckConfigMap to return true (up-to-date), but got false")
	}
}

// TestCheckConfigMap_WithGeneric_FailsDueToBug demonstrates the bug:
// GetGenericScraperConfigMap writes the Generic field into config.yaml,
// but CheckConfigMap never sets it when rebuilding the struct for comparison,
// so the byte comparison always fails and the resource appears out-of-date.
//
// This test is EXPECTED TO FAIL until the bug is fixed by adding:
//
//	exporter.Generic = scraperConfig.Spec.Generic
//
// inside CheckConfigMap.
func TestCheckConfigMap_WithGeneric_FailsDueToBug(t *testing.T) {
	scraperConfig := baseScraperConfig()
	scraperConfig.Spec.Generic = &finopsDataTypes.Generic{
		ValueColumnIndex: 2,
		MetricName:       "cpu_usage",
	}

	// The ConfigMap is created correctly — it includes the Generic field.
	cm := buildConfigMapFromScraper(t, scraperConfig)

	// CheckConfigMap rebuilds the struct WITHOUT Generic, so the YAML won't match.
	result := comparators.CheckConfigMap(cm, *scraperConfig)
	if !result {
		// This is the bug firing: the resource is flagged as out-of-date every loop.
		t.Error("BUG CONFIRMED: CheckConfigMap returned false for a valid up-to-date ConfigMap. " +
			"Fix: add `exporter.Generic = scraperConfig.Spec.Generic` inside CheckConfigMap.")
	}
}
