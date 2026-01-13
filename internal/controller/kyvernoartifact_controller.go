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
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	kyvernov1alpha1 "github.com/OctoKode/kyverno-artifact-operator/api/v1alpha1"
)

const (
	providerGitHub = "github"
)

// +kubebuilder:rbac:groups=kyverno.octokode.io,resources=kyvernoartifacts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kyverno.octokode.io,resources=kyvernoartifacts/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kyverno.octokode.io,resources=kyvernoartifacts/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kyverno.io,resources=policies;clusterpolicies,verbs=get;list;watch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.22.1/pkg/reconcile
func (r *KyvernoArtifactReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Fetch the KyvernoArtifact instance
	var kyvernoArtifact kyvernov1alpha1.KyvernoArtifact
	if err := r.Get(ctx, req.NamespacedName, &kyvernoArtifact); err != nil {
		if errors.IsNotFound(err) {
			// Resource was deleted - this is expected, pods will be garbage collected via owner references
			log.Info("KyvernoArtifact deleted, associated pods will be cleaned up automatically", "name", req.Name, "namespace", req.Namespace)
			return ctrl.Result{}, nil
		}
		// Unexpected error
		log.Error(err, "unable to fetch KyvernoArtifact")
		return ctrl.Result{}, err
	}

	// Add your reconciliation logic here
	log.Info("Reconciling KyvernoArtifact", "Name", kyvernoArtifact.Name, "Url", kyvernoArtifact.Spec.ArtifactUrl, "PollingInterval", kyvernoArtifact.Spec.PollingInterval)

	podName := fmt.Sprintf("kyverno-artifact-manager-%s", kyvernoArtifact.Name)
	pod := &corev1.Pod{}
	err := r.Get(ctx, client.ObjectKey{Name: podName, Namespace: kyvernoArtifact.Namespace}, pod)

	if err != nil && errors.IsNotFound(err) {
		// Validate that ArtifactUrl is set
		if kyvernoArtifact.Spec.ArtifactUrl == nil || *kyvernoArtifact.Spec.ArtifactUrl == "" {
			err := fmt.Errorf("spec.ArtifactUrl is required but not set")
			log.Error(err, "unable to create Pod without artifact URL")
			return ctrl.Result{}, err
		}

		artifactUrl := *kyvernoArtifact.Spec.ArtifactUrl

		pollingInterval := "60"
		if kyvernoArtifact.Spec.PollingInterval != nil {
			pollingInterval = fmt.Sprintf("%d", *kyvernoArtifact.Spec.PollingInterval)
		}

		// Determine provider from spec, default to "github" for backward compatibility
		provider := providerGitHub
		if kyvernoArtifact.Spec.ArtifactProvider != nil && *kyvernoArtifact.Spec.ArtifactProvider != "" {
			provider = *kyvernoArtifact.Spec.ArtifactProvider
		}

		// Build environment variables based on provider
		envVars := []corev1.EnvVar{
			{
				Name:  "IMAGE_BASE",
				Value: artifactUrl,
			},
			{
				Name:  "POLL_INTERVAL",
				Value: pollingInterval,
			},
			{
				Name:  "PROVIDER",
				Value: provider,
			},
			{
				Name:  "ARTIFACT_NAME",
				Value: kyvernoArtifact.Name,
			},
		}

		if kyvernoArtifact.Spec.DeletePoliciesOnTermination != nil && *kyvernoArtifact.Spec.DeletePoliciesOnTermination {
			envVars = append(envVars, corev1.EnvVar{
				Name: "WATCHER_DELETE_POLICIES_ON_TERMINATION",
				//nolint:goconst // Required value for environment variable
				Value: "true",
			})
		}

		if kyvernoArtifact.Spec.ReconcilePoliciesFromChecksum != nil {
			envVars = append(envVars, corev1.EnvVar{
				Name:  "WATCHER_CHECKSUM_RECONCILIATION_ENABLED",
				Value: fmt.Sprintf("%t", *kyvernoArtifact.Spec.ReconcilePoliciesFromChecksum),
			})
		}

		if kyvernoArtifact.Spec.PollForTagChanges != nil {
			envVars = append(envVars, corev1.EnvVar{
				Name:  "WATCHER_POLL_FOR_TAG_CHANGES_ENABLED",
				Value: fmt.Sprintf("%t", *kyvernoArtifact.Spec.PollForTagChanges),
			})
		}

		// Add provider-specific credentials
		switch provider {
		case providerGitHub:
			envVars = append(envVars, corev1.EnvVar{
				Name: "GITHUB_TOKEN",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						Key: r.Config.GitHubTokenKey,
						LocalObjectReference: corev1.LocalObjectReference{
							Name: r.Config.SecretName,
						},
					},
				},
			})
		case "artifactory":
			envVars = append(envVars, corev1.EnvVar{
				Name: "ARTIFACTORY_USERNAME",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						Key: r.Config.ArtifactoryUsernameKey,
						LocalObjectReference: corev1.LocalObjectReference{
							Name: r.Config.SecretName,
						},
					},
				},
			}, corev1.EnvVar{
				Name: "ARTIFACTORY_PASSWORD",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						Key: r.Config.ArtifactoryPasswordKey,
						LocalObjectReference: corev1.LocalObjectReference{
							Name: r.Config.SecretName,
						},
					},
				},
			})
		}

		// Inject WATCHER_IMAGE and POD_NAMESPACE for self-reconciliation.
		// WATCHER_IMAGE provides the expected image version for the watcher pod to compare against.
		// POD_NAMESPACE allows the watcher to discover other pods in its own namespace for reconciliation.
		envVars = append(envVars, corev1.EnvVar{
			Name:  "WATCHER_IMAGE",
			Value: r.Config.WatcherImage,
		}, corev1.EnvVar{
			Name: "POD_NAMESPACE",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.namespace",
				},
			},
		})

		pod = &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName,
				Namespace: kyvernoArtifact.Namespace,
				// Apply standardized labels to the watcher pod for better discoverability and management.
				// These labels are crucial for the watcher's self-reconciliation logic to find other watcher pods.
				Labels: map[string]string{
					"app.kubernetes.io/name":       "kyverno-artifact-watcher",
					"app.kubernetes.io/instance":   kyvernoArtifact.Name, // Identifies the KyvernoArtifact resource instance
					"app.kubernetes.io/managed-by": "kyverno-artifact-operator",
					"app.kubernetes.io/component":  "watcher",
				},
			},
			Spec: corev1.PodSpec{
				ServiceAccountName: r.Config.WatcherServiceAccount,
				Containers: []corev1.Container{
					{
						Name:            "watcher",
						Image:           r.Config.WatcherImage,
						ImagePullPolicy: corev1.PullAlways,
						Args:            []string{"-watcher"},
						Env:             envVars,
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      "tmp",
								MountPath: "/tmp",
							},
						},
					},
				},
				Volumes: []corev1.Volume{
					{
						Name: "tmp",
						VolumeSource: corev1.VolumeSource{
							EmptyDir: &corev1.EmptyDirVolumeSource{},
						},
					},
				},
				RestartPolicy: corev1.RestartPolicyAlways,
			},
		}

		if err := controllerutil.SetControllerReference(&kyvernoArtifact, pod, r.Scheme); err != nil {
			log.Error(err, "unable to set controller reference for Pod")
			return ctrl.Result{}, err
		}

		if err := r.Create(ctx, pod); err != nil {
			log.Error(err, "unable to create Pod",
				"KyvernoArtifact.Name", kyvernoArtifact.Name,
				"KyvernoArtifact.Namespace", kyvernoArtifact.Namespace,
				"Pod.Name", pod.Name,
				"Pod.ServiceAccountName", pod.Spec.ServiceAccountName,
				"Pod.Image", pod.Spec.Containers[0].Image,
			)
			return ctrl.Result{}, err
		}
		log.Info("Created Pod", "Name", podName)
	} else if err != nil {
		log.Error(err, "unable to fetch Pod")
		return ctrl.Result{}, err
	} else {
		// Pod exists - check if it needs to be recreated

		// Check if pod is in a terminal state
		if pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodSucceeded {
			log.Info("Pod is in terminal state, deleting for recreation", "Name", podName, "Phase", pod.Status.Phase)
			if err := r.Delete(ctx, pod); err != nil && !errors.IsNotFound(err) {
				log.Error(err, "unable to delete Pod in terminal state")
				return ctrl.Result{}, err
			}
			// The Owns() relationship will trigger reconciliation when the pod is deleted
			return ctrl.Result{}, nil
		}

		// Check if the pod configuration needs to be updated by comparing env vars
		needsUpdate := false

		// Get current artifact URL and polling interval from spec
		currentArtifactUrl := ""
		if kyvernoArtifact.Spec.ArtifactUrl != nil {
			currentArtifactUrl = *kyvernoArtifact.Spec.ArtifactUrl
		}

		currentPollingInterval := "60"
		if kyvernoArtifact.Spec.PollingInterval != nil {
			currentPollingInterval = fmt.Sprintf("%d", *kyvernoArtifact.Spec.PollingInterval)
		}

		currentProvider := providerGitHub
		if kyvernoArtifact.Spec.ArtifactProvider != nil && *kyvernoArtifact.Spec.ArtifactProvider != "" {
			currentProvider = *kyvernoArtifact.Spec.ArtifactProvider
		}

		// Check if the pod's environment variables match the current spec
		if len(pod.Spec.Containers) > 0 {
			container := pod.Spec.Containers[0]
			envMap := make(map[string]string)
			for _, env := range container.Env {
				if env.Value != "" {
					envMap[env.Name] = env.Value
				}
			}

			// Check if IMAGE_BASE or POLL_INTERVAL or PROVIDER has changed
			if envMap["IMAGE_BASE"] != currentArtifactUrl {
				log.Info("Pod needs update: IMAGE_BASE changed", "old", envMap["IMAGE_BASE"], "new", currentArtifactUrl)
				needsUpdate = true
			}
			if envMap["POLL_INTERVAL"] != currentPollingInterval {
				log.Info("Pod needs update: POLL_INTERVAL changed", "old", envMap["POLL_INTERVAL"], "new", currentPollingInterval)
				needsUpdate = true
			}
			if envMap["PROVIDER"] != currentProvider {
				log.Info("Pod needs update: PROVIDER changed", "old", envMap["PROVIDER"], "new", currentProvider)
				needsUpdate = true
			}

			// Check if WATCHER_POLL_FOR_TAG_CHANGES_ENABLED has changed
			//nolint:goconst // This is the default in the watcher
			currentPollForTagChanges := "true"
			if kyvernoArtifact.Spec.PollForTagChanges != nil {
				currentPollForTagChanges = fmt.Sprintf("%t", *kyvernoArtifact.Spec.PollForTagChanges)
			}
			podPollForTagChanges, ok := envMap["WATCHER_POLL_FOR_TAG_CHANGES_ENABLED"]
			if !ok {
				// If the env var is not set in the pod, assume the default value
				//nolint:goconst // This is the default in the watcher
				podPollForTagChanges = "true"
			}
			if podPollForTagChanges != currentPollForTagChanges {
				log.Info("Pod needs update: WATCHER_POLL_FOR_TAG_CHANGES_ENABLED changed", "old", podPollForTagChanges, "new", currentPollForTagChanges)
				needsUpdate = true
			}

			// Check if the pod's image needs to be updated
			// Crucial check for watcher self-reconciliation: ensure the watcher pod is running the latest image.
			// If the image of the running pod's container doesn't match the expected WatcherImage from the controller's config,
			// it indicates that the operator itself has been upgraded and this watcher pod is now outdated.
			// Deleting it will cause Kubernetes to recreate the pod with the correct (latest) image.
			if container.Image != r.Config.WatcherImage {
				log.Info("Pod needs update: watcher image changed", "old", container.Image, "new", r.Config.WatcherImage)
				needsUpdate = true
			}
		}

		if needsUpdate {
			log.Info("Pod configuration changed, deleting for recreation", "Name", podName)
			if err := r.Delete(ctx, pod); err != nil && !errors.IsNotFound(err) {
				log.Error(err, "unable to delete Pod for update")
				return ctrl.Result{}, err
			}
			// The Owns() relationship will trigger reconciliation when the pod is deleted
			return ctrl.Result{}, nil
		}

		log.Info("Pod already exists and is running", "Name", podName, "Phase", pod.Status.Phase)
	}

	// Update metrics after successful reconciliation
	r.updateMetrics(ctx)

	return ctrl.Result{}, nil
}

// updateMetrics collects and updates Prometheus metrics for KyvernoArtifacts
func (r *KyvernoArtifactReconciler) updateMetrics(ctx context.Context) {
	// List all KyvernoArtifact resources
	var artifactList kyvernov1alpha1.KyvernoArtifactList
	if err := r.List(ctx, &artifactList); err != nil {
		// Log but don't fail reconciliation if metrics update fails
		logf.FromContext(ctx).Error(err, "unable to list KyvernoArtifacts for metrics")
		return
	}

	// Update total count
	ArtifactCount.Set(float64(len(artifactList.Items)))

	// Count by pod phase
	phaseCount := make(map[string]int)
	for _, artifact := range artifactList.Items {
		podName := fmt.Sprintf("kyverno-artifact-manager-%s", artifact.Name)
		pod := &corev1.Pod{}
		err := r.Get(ctx, client.ObjectKey{Name: podName, Namespace: artifact.Namespace}, pod)

		var phase string
		if err != nil {
			phase = "Unknown"
		} else {
			phase = string(pod.Status.Phase)
		}
		phaseCount[phase]++
	}

	// Reset all phase metrics first
	ArtifactsByPhase.Reset()

	// Set the counts
	for phase, count := range phaseCount {
		ArtifactsByPhase.WithLabelValues(phase).Set(float64(count))
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *KyvernoArtifactReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kyvernov1alpha1.KyvernoArtifact{}).
		Owns(&corev1.Pod{}).
		Named("kyvernoartifact").
		Complete(r)
}
