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
	"encoding/json"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	testArtifactURL      = "ghcr.io/octokode/kyverno-policies:latest"
	testArtifactType     = "oci-image"
	testArtifactProvider = "github"
	testNamespace        = "default"
	testName             = "test-artifact"
)

func TestKyvernoArtifactSpec(t *testing.T) {
	url := testArtifactURL
	artifactType := testArtifactType
	provider := testArtifactProvider
	interval := int32(300)

	spec := KyvernoArtifactSpec{
		ArtifactUrl:      &url,
		ArtifactType:     &artifactType,
		ArtifactProvider: &provider,
		PollingInterval:  &interval,
	}

	if spec.ArtifactUrl == nil || *spec.ArtifactUrl != testArtifactURL {
		t.Errorf("Expected ArtifactUrl %q, got %v", testArtifactURL, spec.ArtifactUrl)
	}

	if spec.ArtifactType == nil || *spec.ArtifactType != testArtifactType {
		t.Errorf("Expected ArtifactType %q, got %v", testArtifactType, spec.ArtifactType)
	}

	if spec.ArtifactProvider == nil || *spec.ArtifactProvider != testArtifactProvider {
		t.Errorf("Expected ArtifactProvider %q, got %v", testArtifactProvider, spec.ArtifactProvider)
	}

	if spec.PollingInterval == nil || *spec.PollingInterval != interval {
		t.Errorf("Expected PollingInterval %d, got %v", interval, spec.PollingInterval)
	}
}

func TestKyvernoArtifactSpecOptionalFields(t *testing.T) {
	spec := KyvernoArtifactSpec{}

	if spec.ArtifactUrl != nil {
		t.Error("Expected ArtifactUrl to be nil")
	}

	if spec.ArtifactType != nil {
		t.Error("Expected ArtifactType to be nil")
	}

	if spec.ArtifactProvider != nil {
		t.Error("Expected ArtifactProvider to be nil")
	}

	if spec.PollingInterval != nil {
		t.Error("Expected PollingInterval to be nil")
	}
}

func TestKyvernoArtifactStatus(t *testing.T) {
	conditions := []metav1.Condition{
		{
			Type:               "Available",
			Status:             metav1.ConditionTrue,
			Reason:             "ArtifactReady",
			Message:            "Artifact is available",
			LastTransitionTime: metav1.Now(),
		},
		{
			Type:               "Progressing",
			Status:             metav1.ConditionFalse,
			Reason:             "Complete",
			Message:            "Artifact processing complete",
			LastTransitionTime: metav1.Now(),
		},
	}

	status := KyvernoArtifactStatus{
		Conditions: conditions,
	}

	if len(status.Conditions) != 2 {
		t.Errorf("Expected 2 conditions, got %d", len(status.Conditions))
	}

	if status.Conditions[0].Type != "Available" {
		t.Errorf("Expected first condition type 'Available', got %q", status.Conditions[0].Type)
	}

	if status.Conditions[0].Status != metav1.ConditionTrue {
		t.Errorf("Expected first condition status True, got %v", status.Conditions[0].Status)
	}
}

func TestKyvernoArtifact(t *testing.T) {
	url := testArtifactURL
	artifactType := testArtifactType

	artifact := KyvernoArtifact{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "kyverno.octokode.io/v1alpha1",
			Kind:       "KyvernoArtifact",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      testName,
			Namespace: testNamespace,
			Labels: map[string]string{
				"app": "kyverno-artifact-manager",
			},
		},
		Spec: KyvernoArtifactSpec{
			ArtifactUrl:  &url,
			ArtifactType: &artifactType,
		},
		Status: KyvernoArtifactStatus{
			Conditions: []metav1.Condition{
				{
					Type:               "Available",
					Status:             metav1.ConditionTrue,
					Reason:             "ArtifactReady",
					LastTransitionTime: metav1.Now(),
				},
			},
		},
	}

	if artifact.Name != testName {
		t.Errorf("Expected name %q, got %q", testName, artifact.Name)
	}

	if artifact.Namespace != testNamespace {
		t.Errorf("Expected namespace %q, got %q", testNamespace, artifact.Namespace)
	}

	if artifact.Kind != "KyvernoArtifact" {
		t.Errorf("Expected kind 'KyvernoArtifact', got %q", artifact.Kind)
	}

	if artifact.APIVersion != "kyverno.octokode.io/v1alpha1" {
		t.Errorf("Expected APIVersion 'kyverno.octokode.io/v1alpha1', got %q", artifact.APIVersion)
	}

	if artifact.Spec.ArtifactUrl == nil || *artifact.Spec.ArtifactUrl != testArtifactURL {
		t.Errorf("Expected spec.ArtifactUrl %q, got %v", testArtifactURL, artifact.Spec.ArtifactUrl)
	}

	if len(artifact.Status.Conditions) != 1 {
		t.Errorf("Expected 1 condition, got %d", len(artifact.Status.Conditions))
	}
}

func TestKyvernoArtifactList(t *testing.T) {
	url1 := "ghcr.io/octokode/policies1:latest"
	url2 := "ghcr.io/octokode/policies2:latest"

	list := KyvernoArtifactList{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "kyverno.octokode.io/v1alpha1",
			Kind:       "KyvernoArtifactList",
		},
		ListMeta: metav1.ListMeta{
			ResourceVersion: "1",
		},
		Items: []KyvernoArtifact{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "artifact1",
					Namespace: testNamespace,
				},
				Spec: KyvernoArtifactSpec{
					ArtifactUrl: &url1,
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "artifact2",
					Namespace: testNamespace,
				},
				Spec: KyvernoArtifactSpec{
					ArtifactUrl: &url2,
				},
			},
		},
	}

	if len(list.Items) != 2 {
		t.Errorf("Expected 2 items, got %d", len(list.Items))
	}

	if list.Items[0].Name != "artifact1" {
		t.Errorf("Expected first item name 'artifact1', got %q", list.Items[0].Name)
	}

	if list.Items[1].Name != "artifact2" {
		t.Errorf("Expected second item name 'artifact2', got %q", list.Items[1].Name)
	}

	if list.Kind != "KyvernoArtifactList" {
		t.Errorf("Expected kind 'KyvernoArtifactList', got %q", list.Kind)
	}
}

func TestKyvernoArtifactJSONSerialization(t *testing.T) {
	url := testArtifactURL
	artifactType := testArtifactType
	provider := testArtifactProvider
	interval := int32(300)

	original := KyvernoArtifact{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "kyverno.octokode.io/v1alpha1",
			Kind:       "KyvernoArtifact",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      testName,
			Namespace: testNamespace,
		},
		Spec: KyvernoArtifactSpec{
			ArtifactUrl:      &url,
			ArtifactType:     &artifactType,
			ArtifactProvider: &provider,
			PollingInterval:  &interval,
		},
	}

	// Marshal to JSON
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Failed to marshal artifact: %v", err)
	}

	// Unmarshal from JSON
	var decoded KyvernoArtifact
	err = json.Unmarshal(data, &decoded)
	if err != nil {
		t.Fatalf("Failed to unmarshal artifact: %v", err)
	}

	// Verify fields
	if decoded.Name != original.Name {
		t.Errorf("Expected name %q, got %q", original.Name, decoded.Name)
	}

	if decoded.Namespace != original.Namespace {
		t.Errorf("Expected namespace %q, got %q", original.Namespace, decoded.Namespace)
	}

	if decoded.Spec.ArtifactUrl == nil || *decoded.Spec.ArtifactUrl != *original.Spec.ArtifactUrl {
		t.Errorf("Expected ArtifactUrl %q, got %v", *original.Spec.ArtifactUrl, decoded.Spec.ArtifactUrl)
	}

	if decoded.Spec.ArtifactType == nil || *decoded.Spec.ArtifactType != *original.Spec.ArtifactType {
		t.Errorf("Expected ArtifactType %q, got %v", *original.Spec.ArtifactType, decoded.Spec.ArtifactType)
	}

	if decoded.Spec.ArtifactProvider == nil || *decoded.Spec.ArtifactProvider != *original.Spec.ArtifactProvider {
		t.Errorf("Expected ArtifactProvider %q, got %v", *original.Spec.ArtifactProvider, decoded.Spec.ArtifactProvider)
	}

	if decoded.Spec.PollingInterval == nil || *decoded.Spec.PollingInterval != *original.Spec.PollingInterval {
		t.Errorf("Expected PollingInterval %d, got %v", *original.Spec.PollingInterval, decoded.Spec.PollingInterval)
	}
}

func TestKyvernoArtifactJSONOmitEmpty(t *testing.T) {
	artifact := KyvernoArtifact{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "kyverno.octokode.io/v1alpha1",
			Kind:       "KyvernoArtifact",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      testName,
			Namespace: testNamespace,
		},
		Spec: KyvernoArtifactSpec{},
	}

	data, err := json.Marshal(artifact)
	if err != nil {
		t.Fatalf("Failed to marshal artifact: %v", err)
	}

	// Convert to map to check fields
	var result map[string]interface{}
	err = json.Unmarshal(data, &result)
	if err != nil {
		t.Fatalf("Failed to unmarshal to map: %v", err)
	}

	spec, ok := result["spec"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected spec to be present")
	}

	// Check that optional fields are omitted when empty
	if _, exists := spec["url"]; exists {
		t.Error("Expected 'url' to be omitted when nil")
	}

	if _, exists := spec["type"]; exists {
		t.Error("Expected 'type' to be omitted when nil")
	}

	if _, exists := spec["provider"]; exists {
		t.Error("Expected 'provider' to be omitted when nil")
	}

	if _, exists := spec["pollingInterval"]; exists {
		t.Error("Expected 'pollingInterval' to be omitted when nil")
	}
}

func TestKyvernoArtifactConditionTypes(t *testing.T) {
	testCases := []struct {
		name           string
		conditionType  string
		status         metav1.ConditionStatus
		reason         string
		message        string
		expectedStatus metav1.ConditionStatus
	}{
		{
			name:           "Available condition",
			conditionType:  "Available",
			status:         metav1.ConditionTrue,
			reason:         "ArtifactReady",
			message:        "Artifact is available",
			expectedStatus: metav1.ConditionTrue,
		},
		{
			name:           "Progressing condition",
			conditionType:  "Progressing",
			status:         metav1.ConditionTrue,
			reason:         "Syncing",
			message:        "Syncing artifact",
			expectedStatus: metav1.ConditionTrue,
		},
		{
			name:           "Degraded condition",
			conditionType:  "Degraded",
			status:         metav1.ConditionTrue,
			reason:         "SyncFailed",
			message:        "Failed to sync artifact",
			expectedStatus: metav1.ConditionTrue,
		},
		{
			name:           "Unknown status",
			conditionType:  "Available",
			status:         metav1.ConditionUnknown,
			reason:         "Unknown",
			message:        "Status unknown",
			expectedStatus: metav1.ConditionUnknown,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			condition := metav1.Condition{
				Type:               tc.conditionType,
				Status:             tc.status,
				Reason:             tc.reason,
				Message:            tc.message,
				LastTransitionTime: metav1.Now(),
			}

			status := KyvernoArtifactStatus{
				Conditions: []metav1.Condition{condition},
			}

			if len(status.Conditions) != 1 {
				t.Errorf("Expected 1 condition, got %d", len(status.Conditions))
			}

			if status.Conditions[0].Type != tc.conditionType {
				t.Errorf("Expected condition type %q, got %q", tc.conditionType, status.Conditions[0].Type)
			}

			if status.Conditions[0].Status != tc.expectedStatus {
				t.Errorf("Expected status %v, got %v", tc.expectedStatus, status.Conditions[0].Status)
			}

			if status.Conditions[0].Reason != tc.reason {
				t.Errorf("Expected reason %q, got %q", tc.reason, status.Conditions[0].Reason)
			}
		})
	}
}

func TestKyvernoArtifactDeepCopy(t *testing.T) {
	url := testArtifactURL
	artifactType := testArtifactType

	original := &KyvernoArtifact{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testName,
			Namespace: testNamespace,
			Labels: map[string]string{
				"test": "label",
			},
		},
		Spec: KyvernoArtifactSpec{
			ArtifactUrl:  &url,
			ArtifactType: &artifactType,
		},
		Status: KyvernoArtifactStatus{
			Conditions: []metav1.Condition{
				{
					Type:   "Available",
					Status: metav1.ConditionTrue,
				},
			},
		},
	}

	// Deep copy
	copied := original.DeepCopy()

	// Verify it's a different object
	if copied == original {
		t.Error("DeepCopy should return a different object")
	}

	// Verify fields are equal
	if copied.Name != original.Name {
		t.Errorf("Expected name %q, got %q", original.Name, copied.Name)
	}

	if copied.Namespace != original.Namespace {
		t.Errorf("Expected namespace %q, got %q", original.Namespace, copied.Namespace)
	}

	if *copied.Spec.ArtifactUrl != *original.Spec.ArtifactUrl {
		t.Errorf("Expected ArtifactUrl %q, got %q", *original.Spec.ArtifactUrl, *copied.Spec.ArtifactUrl)
	}

	// Verify modifying copy doesn't affect original
	newURL := "ghcr.io/octokode/new:latest"
	copied.Spec.ArtifactUrl = &newURL

	if *original.Spec.ArtifactUrl == newURL {
		t.Error("Modifying copy should not affect original")
	}
}

func TestKyvernoArtifactListDeepCopy(t *testing.T) {
	url := testArtifactURL

	original := &KyvernoArtifactList{
		Items: []KyvernoArtifact{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "artifact1",
				},
				Spec: KyvernoArtifactSpec{
					ArtifactUrl: &url,
				},
			},
		},
	}

	// Deep copy
	copied := original.DeepCopy()

	// Verify it's a different object
	if copied == original {
		t.Error("DeepCopy should return a different object")
	}

	// Verify length
	if len(copied.Items) != len(original.Items) {
		t.Errorf("Expected %d items, got %d", len(original.Items), len(copied.Items))
	}

	// Verify modifying copy doesn't affect original
	copied.Items = append(copied.Items, KyvernoArtifact{
		ObjectMeta: metav1.ObjectMeta{
			Name: "artifact2",
		},
	})

	if len(original.Items) == len(copied.Items) {
		t.Error("Modifying copy should not affect original")
	}
}

func TestKyvernoArtifactMetadataLabels(t *testing.T) {
	artifact := KyvernoArtifact{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testName,
			Namespace: testNamespace,
			Labels: map[string]string{
				"app":             "kyverno-artifact-manager",
				"managed-by":      "kyverno-watcher",
				"policy-version":  "v1.0.0",
				"artifact-source": "github",
			},
		},
	}

	expectedLabels := map[string]string{
		"app":             "kyverno-artifact-manager",
		"managed-by":      "kyverno-watcher",
		"policy-version":  "v1.0.0",
		"artifact-source": "github",
	}

	if len(artifact.Labels) != len(expectedLabels) {
		t.Errorf("Expected %d labels, got %d", len(expectedLabels), len(artifact.Labels))
	}

	for key, expectedValue := range expectedLabels {
		if actualValue, exists := artifact.Labels[key]; !exists {
			t.Errorf("Expected label %q to exist", key)
		} else if actualValue != expectedValue {
			t.Errorf("Expected label %q to be %q, got %q", key, expectedValue, actualValue)
		}
	}
}

func TestKyvernoArtifactMetadataAnnotations(t *testing.T) {
	artifact := KyvernoArtifact{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testName,
			Namespace: testNamespace,
			Annotations: map[string]string{
				"description":           "Test artifact",
				"last-sync":             "2025-11-25T10:00:00Z",
				"artifact-digest":       "sha256:abcdef123456",
				"sync-interval-seconds": "300",
			},
		},
	}

	if len(artifact.Annotations) != 4 {
		t.Errorf("Expected 4 annotations, got %d", len(artifact.Annotations))
	}

	if artifact.Annotations["description"] != "Test artifact" {
		t.Errorf("Expected description annotation, got %q", artifact.Annotations["description"])
	}

	if artifact.Annotations["artifact-digest"] != "sha256:abcdef123456" {
		t.Errorf("Expected artifact-digest annotation, got %q", artifact.Annotations["artifact-digest"])
	}
}

func TestKyvernoArtifactDeepCopyObject(t *testing.T) {
	url := testArtifactURL

	original := &KyvernoArtifact{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testName,
			Namespace: testNamespace,
		},
		Spec: KyvernoArtifactSpec{
			ArtifactUrl: &url,
		},
	}

	// Test DeepCopyObject
	copied := original.DeepCopyObject()

	// Verify it returns a runtime.Object
	if copied == nil {
		t.Fatal("DeepCopyObject should not return nil")
	}

	// Type assert back to KyvernoArtifact
	copiedArtifact, ok := copied.(*KyvernoArtifact)
	if !ok {
		t.Fatal("DeepCopyObject should return *KyvernoArtifact")
	}

	// Verify fields
	if copiedArtifact.Name != original.Name {
		t.Errorf("Expected name %q, got %q", original.Name, copiedArtifact.Name)
	}

	// Verify it's a different object
	if copiedArtifact == original {
		t.Error("DeepCopyObject should return a different object")
	}
}

func TestKyvernoArtifactListDeepCopyObject(t *testing.T) {
	url := testArtifactURL

	original := &KyvernoArtifactList{
		Items: []KyvernoArtifact{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "artifact1",
				},
				Spec: KyvernoArtifactSpec{
					ArtifactUrl: &url,
				},
			},
		},
	}

	// Test DeepCopyObject
	copied := original.DeepCopyObject()

	// Verify it returns a runtime.Object
	if copied == nil {
		t.Fatal("DeepCopyObject should not return nil")
	}

	// Type assert back to KyvernoArtifactList
	copiedList, ok := copied.(*KyvernoArtifactList)
	if !ok {
		t.Fatal("DeepCopyObject should return *KyvernoArtifactList")
	}

	// Verify fields
	if len(copiedList.Items) != len(original.Items) {
		t.Errorf("Expected %d items, got %d", len(original.Items), len(copiedList.Items))
	}

	// Verify it's a different object
	if copiedList == original {
		t.Error("DeepCopyObject should return a different object")
	}
}

func TestKyvernoArtifactSpecDeepCopy(t *testing.T) {
	url := testArtifactURL
	artifactType := testArtifactType
	provider := testArtifactProvider
	interval := int32(300)

	original := &KyvernoArtifactSpec{
		ArtifactUrl:      &url,
		ArtifactType:     &artifactType,
		ArtifactProvider: &provider,
		PollingInterval:  &interval,
	}

	// Deep copy
	copied := original.DeepCopy()

	// Verify it's a different object
	if copied == original {
		t.Error("DeepCopy should return a different object")
	}

	// Verify fields are equal
	if *copied.ArtifactUrl != *original.ArtifactUrl {
		t.Errorf("Expected ArtifactUrl %q, got %q", *original.ArtifactUrl, *copied.ArtifactUrl)
	}

	if *copied.ArtifactType != *original.ArtifactType {
		t.Errorf("Expected ArtifactType %q, got %q", *original.ArtifactType, *copied.ArtifactType)
	}

	if *copied.ArtifactProvider != *original.ArtifactProvider {
		t.Errorf("Expected ArtifactProvider %q, got %q", *original.ArtifactProvider, *copied.ArtifactProvider)
	}

	if *copied.PollingInterval != *original.PollingInterval {
		t.Errorf("Expected PollingInterval %d, got %d", *original.PollingInterval, *copied.PollingInterval)
	}

	// Verify modifying copy doesn't affect original
	newURL := "ghcr.io/octokode/new:latest"
	copied.ArtifactUrl = &newURL

	if *original.ArtifactUrl == newURL {
		t.Error("Modifying copy should not affect original")
	}
}

func TestKyvernoArtifactStatusDeepCopy(t *testing.T) {
	original := &KyvernoArtifactStatus{
		Conditions: []metav1.Condition{
			{
				Type:    "Available",
				Status:  metav1.ConditionTrue,
				Reason:  "Ready",
				Message: "Artifact is ready",
			},
			{
				Type:    "Progressing",
				Status:  metav1.ConditionFalse,
				Reason:  "Complete",
				Message: "Processing complete",
			},
		},
	}

	// Deep copy
	copied := original.DeepCopy()

	// Verify it's a different object
	if copied == original {
		t.Error("DeepCopy should return a different object")
	}

	// Verify conditions length
	if len(copied.Conditions) != len(original.Conditions) {
		t.Errorf("Expected %d conditions, got %d", len(original.Conditions), len(copied.Conditions))
	}

	// Verify condition fields
	if copied.Conditions[0].Type != original.Conditions[0].Type {
		t.Errorf("Expected type %q, got %q", original.Conditions[0].Type, copied.Conditions[0].Type)
	}

	// Verify modifying copy doesn't affect original
	copied.Conditions = append(copied.Conditions, metav1.Condition{
		Type:   "Degraded",
		Status: metav1.ConditionFalse,
	})

	if len(original.Conditions) == len(copied.Conditions) {
		t.Error("Modifying copy should not affect original")
	}
}

func TestKyvernoArtifactSpecNilFields(t *testing.T) {
	spec := &KyvernoArtifactSpec{}

	// Deep copy with nil fields
	copied := spec.DeepCopy()

	if copied.ArtifactUrl != nil {
		t.Error("Expected ArtifactUrl to be nil")
	}

	if copied.ArtifactType != nil {
		t.Error("Expected ArtifactType to be nil")
	}

	if copied.ArtifactProvider != nil {
		t.Error("Expected ArtifactProvider to be nil")
	}

	if copied.PollingInterval != nil {
		t.Error("Expected PollingInterval to be nil")
	}
}

func TestKyvernoArtifactStatusEmptyConditions(t *testing.T) {
	original := &KyvernoArtifactStatus{
		Conditions: []metav1.Condition{},
	}

	// Deep copy with empty conditions
	copied := original.DeepCopy()

	if len(copied.Conditions) != 0 {
		t.Errorf("Expected 0 conditions, got %d", len(copied.Conditions))
	}
}
