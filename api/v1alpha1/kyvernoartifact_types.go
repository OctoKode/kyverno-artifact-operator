/*
Copyright 2025.

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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// KyvernoArtifactSpec defines the desired state of KyvernoArtifact
type KyvernoArtifactSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	// The following markers will use OpenAPI v3 schema to validate the value
	// More info: https://book.kubebuilder.io/reference/markers/crd-validation.html

	// url is the location of the artifact such as ghcr.io/OctoKode/kyverno-policies:latest
	ArtifactUrl *string `json:"url,omitempty"`
	// type is the type of artifact such as 'oci-image' or 'git-repo'. Only oci-image is supported for now.
	// +optional
	ArtifactType *string `json:"type,omitempty"`
	// provider is the artifact provider such as 'github' or 'artifactory'. Both github and artifactory are supported.
	// +optional
	ArtifactProvider *string `json:"provider,omitempty"`
	// pollingInterval is the interval in seconds to check for updates to the artifact.
	// +optional
	PollingInterval *int32 `json:"pollingInterval,omitempty"`
	// +optional
	DeletePoliciesOnTermination *bool `json:"deletePoliciesOnTermination,omitempty"`
	// reconcilePoliciesFromChecksum enables or disables policy reconciliation based on checksums.
	// +optional
	ReconcilePoliciesFromChecksum *bool `json:"reconcilePoliciesFromChecksum,omitempty"`
}

// KyvernoArtifactStatus defines the observed state of KyvernoArtifact.
type KyvernoArtifactStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// For Kubernetes API conventions, see:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	// conditions represent the current state of the KyvernoArtifact resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types include:
	// - "Available": the resource is fully functional
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain its desired state
	//
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// KyvernoArtifact is the Schema for the kyvernoartifacts API
type KyvernoArtifact struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of KyvernoArtifact
	// +required
	Spec KyvernoArtifactSpec `json:"spec"`

	// status defines the observed state of KyvernoArtifact
	// +optional
	Status KyvernoArtifactStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// KyvernoArtifactList contains a list of KyvernoArtifact
type KyvernoArtifactList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KyvernoArtifact `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KyvernoArtifact{}, &KyvernoArtifactList{})
}
