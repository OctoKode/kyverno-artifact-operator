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
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestGroupVersion(t *testing.T) {
	expectedGroup := "kyverno.octokode.io"
	expectedVersion := "v1alpha1"

	if GroupVersion.Group != expectedGroup {
		t.Errorf("Expected group %q, got %q", expectedGroup, GroupVersion.Group)
	}

	if GroupVersion.Version != expectedVersion {
		t.Errorf("Expected version %q, got %q", expectedVersion, GroupVersion.Version)
	}
}

func TestGroupVersionString(t *testing.T) {
	expected := "kyverno.octokode.io/v1alpha1"
	actual := GroupVersion.String()

	if actual != expected {
		t.Errorf("Expected GroupVersion string %q, got %q", expected, actual)
	}
}

func TestSchemeBuilder(t *testing.T) {
	if SchemeBuilder == nil {
		t.Fatal("SchemeBuilder should not be nil")
	}

	if SchemeBuilder.GroupVersion != GroupVersion {
		t.Errorf("Expected SchemeBuilder.GroupVersion to be %v, got %v", GroupVersion, SchemeBuilder.GroupVersion)
	}
}

func TestAddToScheme(t *testing.T) {
	if AddToScheme == nil {
		t.Fatal("AddToScheme should not be nil")
	}

	scheme := runtime.NewScheme()
	err := AddToScheme(scheme)
	if err != nil {
		t.Fatalf("Failed to add to scheme: %v", err)
	}

	// Verify KyvernoArtifact is registered
	gvk := schema.GroupVersionKind{
		Group:   "kyverno.octokode.io",
		Version: "v1alpha1",
		Kind:    "KyvernoArtifact",
	}

	knownTypes := scheme.KnownTypes(GroupVersion)
	if _, exists := knownTypes[gvk.Kind]; !exists {
		t.Errorf("Expected KyvernoArtifact to be registered in scheme")
	}

	// Verify KyvernoArtifactList is registered
	gvkList := schema.GroupVersionKind{
		Group:   "kyverno.octokode.io",
		Version: "v1alpha1",
		Kind:    "KyvernoArtifactList",
	}

	if _, exists := knownTypes[gvkList.Kind]; !exists {
		t.Errorf("Expected KyvernoArtifactList to be registered in scheme")
	}
}

func TestSchemeRegistration(t *testing.T) {
	scheme := runtime.NewScheme()
	err := AddToScheme(scheme)
	if err != nil {
		t.Fatalf("Failed to add to scheme: %v", err)
	}

	// Test that we can create objects using the scheme
	artifact := &KyvernoArtifact{}
	artifact.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   GroupVersion.Group,
		Version: GroupVersion.Version,
		Kind:    "KyvernoArtifact",
	})

	gvks, _, err := scheme.ObjectKinds(artifact)
	if err != nil {
		t.Fatalf("Failed to get object kinds: %v", err)
	}

	if len(gvks) == 0 {
		t.Error("Expected at least one GVK for KyvernoArtifact")
	}

	found := false
	for _, gvk := range gvks {
		if gvk.Group == GroupVersion.Group && gvk.Version == GroupVersion.Version && gvk.Kind == "KyvernoArtifact" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected to find KyvernoArtifact GVK in scheme")
	}
}

func TestGroupVersionWithKind(t *testing.T) {
	testCases := []struct {
		name        string
		kind        string
		expectedGVK schema.GroupVersionKind
	}{
		{
			name: "KyvernoArtifact",
			kind: "KyvernoArtifact",
			expectedGVK: schema.GroupVersionKind{
				Group:   "kyverno.octokode.io",
				Version: "v1alpha1",
				Kind:    "KyvernoArtifact",
			},
		},
		{
			name: "KyvernoArtifactList",
			kind: "KyvernoArtifactList",
			expectedGVK: schema.GroupVersionKind{
				Group:   "kyverno.octokode.io",
				Version: "v1alpha1",
				Kind:    "KyvernoArtifactList",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gvk := GroupVersion.WithKind(tc.kind)

			if gvk.Group != tc.expectedGVK.Group {
				t.Errorf("Expected group %q, got %q", tc.expectedGVK.Group, gvk.Group)
			}

			if gvk.Version != tc.expectedGVK.Version {
				t.Errorf("Expected version %q, got %q", tc.expectedGVK.Version, gvk.Version)
			}

			if gvk.Kind != tc.expectedGVK.Kind {
				t.Errorf("Expected kind %q, got %q", tc.expectedGVK.Kind, gvk.Kind)
			}
		})
	}
}

func TestSchemeAllTypesRegistered(t *testing.T) {
	scheme := runtime.NewScheme()
	err := AddToScheme(scheme)
	if err != nil {
		t.Fatalf("Failed to add to scheme: %v", err)
	}

	knownTypes := scheme.KnownTypes(GroupVersion)

	requiredTypes := []string{
		"KyvernoArtifact",
		"KyvernoArtifactList",
	}

	for _, typeName := range requiredTypes {
		if _, exists := knownTypes[typeName]; !exists {
			t.Errorf("Type %q should be registered in scheme", typeName)
		}
	}
}
