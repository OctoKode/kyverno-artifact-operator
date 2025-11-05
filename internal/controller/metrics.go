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
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// ArtifactCount is a Prometheus metric that tracks the number of KyvernoArtifact resources
	ArtifactCount = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "kyverno_artifacts_total",
			Help: "Total number of KyvernoArtifact resources being managed",
		},
	)

	// ArtifactsByPhase tracks the number of artifacts by their pod phase
	ArtifactsByPhase = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kyverno_artifacts_by_phase",
			Help: "Number of KyvernoArtifact resources by pod phase",
		},
		[]string{"phase"},
	)
)

func init() {
	// Register custom metrics with the controller-runtime metrics registry
	metrics.Registry.MustRegister(ArtifactCount)
	metrics.Registry.MustRegister(ArtifactsByPhase)
}
