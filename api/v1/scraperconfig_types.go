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
// +kubebuilder:object:generate=true
package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorPackage "github.com/krateoplatformops/finops-operator-exporter/api/v1"
)

// ScraperConfigStatus defines the observed state of ScraperConfig
type ScraperConfigStatus struct {
	ActiveScraper corev1.ObjectReference `json:"active,omitempty"`
	ConfigMap     corev1.ObjectReference `json:"configMaps,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// ScraperConfig is the Schema for the scraperconfigs API
type ScraperConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   operatorPackage.ScraperConfig `json:"spec,omitempty"`
	Status ScraperConfigStatus           `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ScraperConfigList contains a list of ScraperConfig
type ScraperConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ScraperConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ScraperConfig{}, &ScraperConfigList{})
}
