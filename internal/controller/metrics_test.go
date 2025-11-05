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

package controller

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestMetricsRegistration(t *testing.T) {
	// Test that metrics are registered
	if ArtifactCount == nil {
		t.Error("ArtifactCount metric is nil")
	}
	if ArtifactsByPhase == nil {
		t.Error("ArtifactsByPhase metric is nil")
	}
}

func TestArtifactCountMetric(t *testing.T) {
	// Set a test value
	ArtifactCount.Set(5)

	// Verify the value
	value := testutil.ToFloat64(ArtifactCount)
	if value != 5 {
		t.Errorf("Expected ArtifactCount to be 5, got %f", value)
	}
}

func TestArtifactsByPhaseMetric(t *testing.T) {
	// Reset and set test values
	ArtifactsByPhase.Reset()
	ArtifactsByPhase.WithLabelValues("Running").Set(3)
	ArtifactsByPhase.WithLabelValues("Pending").Set(2)

	// Verify the values
	runningValue := testutil.ToFloat64(ArtifactsByPhase.WithLabelValues("Running"))
	if runningValue != 3 {
		t.Errorf("Expected Running artifacts to be 3, got %f", runningValue)
	}

	pendingValue := testutil.ToFloat64(ArtifactsByPhase.WithLabelValues("Pending"))
	if pendingValue != 2 {
		t.Errorf("Expected Pending artifacts to be 2, got %f", pendingValue)
	}
}
