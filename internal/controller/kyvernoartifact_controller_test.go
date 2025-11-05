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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kyvernov1alpha1 "github.com/OctoKode/kyverno-artifact-operator/api/v1alpha1"
)

var _ = Describe("KyvernoArtifact Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"
		const timeout = "10s"
		const interval = "250ms"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default", // TODO(user):Modify as needed
		}
		kyvernoartifact := &kyvernov1alpha1.KyvernoArtifact{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind KyvernoArtifact")
			err := k8sClient.Get(ctx, typeNamespacedName, kyvernoartifact)
			if err != nil && errors.IsNotFound(err) {
				resource := &kyvernov1alpha1.KyvernoArtifact{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: kyvernov1alpha1.KyvernoArtifactSpec{
						ArtifactUrl:      ptrString("ghcr.io/foo/policies:v0.0.1"),
						ArtifactType:     ptrString("github"),
						ArtifactProvider: ptrString("oci-image"),
						PollingInterval:  ptrInt32(30),
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())

				By("Checking the created resource has the correct polling interval")
				createdResource := &kyvernov1alpha1.KyvernoArtifact{}
				Expect(k8sClient.Get(ctx, typeNamespacedName, createdResource)).To(Succeed())
				Expect(*createdResource.Spec.PollingInterval).To(Equal(int32(30)))
				Expect(*createdResource.Spec.ArtifactUrl).To(Equal("ghcr.io/foo/policies:v0.0.1"))
				Expect(*createdResource.Spec.ArtifactType).To(Equal("github"))
				Expect(*createdResource.Spec.ArtifactProvider).To(Equal("oci-image"))
			}
		})

		AfterEach(func() {
			// Cleanup logic after each test, like removing the resource instance.
			resource := &kyvernov1alpha1.KyvernoArtifact{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				By("Cleanup the specific resource instance KyvernoArtifact")
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &KyvernoArtifactReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				Config: DefaultConfig(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking that a pod was created")
			podName := fmt.Sprintf("kyverno-artifact-manager-%s", resourceName)
			pod := &corev1.Pod{}
			Eventually(func() error {
				return k8sClient.Get(ctx, client.ObjectKey{Name: podName, Namespace: "default"}, pod)
			}, timeout, interval).Should(Succeed())
			Expect(pod.Name).To(Equal(podName))
			Expect(pod.Spec.ServiceAccountName).To(Equal("kyverno-artifact-operator-kyverno-artifact-watcher"))

			By("Checking the pod has correct environment variables")
			Expect(pod.Spec.Containers).To(HaveLen(1))
			container := pod.Spec.Containers[0]

			// Find and verify environment variables
			envMap := make(map[string]string)
			for _, env := range container.Env {
				if env.Value != "" {
					envMap[env.Name] = env.Value
				}
			}

			Expect(envMap["IMAGE_BASE"]).To(Equal("ghcr.io/foo/policies:v0.0.1"))
			Expect(envMap["POLL_INTERVAL"]).To(Equal("30"))
		})

		It("should recreate the pod if it is deleted", func() {
			By("Reconciling to create the initial pod")
			controllerReconciler := &KyvernoArtifactReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				Config: DefaultConfig(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying the pod exists")
			podName := fmt.Sprintf("kyverno-artifact-manager-%s", resourceName)
			pod := &corev1.Pod{}
			Eventually(func() error {
				return k8sClient.Get(ctx, client.ObjectKey{Name: podName, Namespace: "default"}, pod)
			}, timeout, interval).Should(Succeed())
			originalUID := pod.UID

			By("Deleting the pod")
			Expect(k8sClient.Delete(ctx, pod)).To(Succeed())
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: podName, Namespace: "default"}, pod)
				return errors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue())

			By("Reconciling again to recreate the pod")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying the pod was recreated with a new UID")
			Eventually(func() error {
				return k8sClient.Get(ctx, client.ObjectKey{Name: podName, Namespace: "default"}, pod)
			}, timeout, interval).Should(Succeed())
			Expect(pod.UID).NotTo(Equal(originalUID))
		})

		It("should recreate the pod when the spec is updated", func() {
			By("Reconciling to create the initial pod")
			controllerReconciler := &KyvernoArtifactReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				Config: DefaultConfig(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying the initial pod exists")
			podName := fmt.Sprintf("kyverno-artifact-manager-%s", resourceName)
			pod := &corev1.Pod{}
			Eventually(func() error {
				return k8sClient.Get(ctx, client.ObjectKey{Name: podName, Namespace: "default"}, pod)
			}, timeout, interval).Should(Succeed())
			originalUID := pod.UID

			By("Verifying initial environment variables")
			Expect(pod.Spec.Containers).To(HaveLen(1))
			container := pod.Spec.Containers[0]
			envMap := make(map[string]string)
			for _, env := range container.Env {
				if env.Value != "" {
					envMap[env.Name] = env.Value
				}
			}
			Expect(envMap["POLL_INTERVAL"]).To(Equal("30"))
			Expect(envMap["IMAGE_BASE"]).To(Equal("ghcr.io/foo/policies:v0.0.1"))

			By("Updating the KyvernoArtifact spec")
			resource := &kyvernov1alpha1.KyvernoArtifact{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())
			resource.Spec.PollingInterval = ptrInt32(120)
			resource.Spec.ArtifactUrl = ptrString("ghcr.io/neworg/new-policies:v2.0")
			Expect(k8sClient.Update(ctx, resource)).To(Succeed())

			By("Reconciling after the update")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying the pod was deleted")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: podName, Namespace: "default"}, pod)
				return errors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue())

			By("Reconciling again to recreate the pod with new config")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying the pod was recreated with updated environment variables")
			Eventually(func() error {
				return k8sClient.Get(ctx, client.ObjectKey{Name: podName, Namespace: "default"}, pod)
			}, timeout, interval).Should(Succeed())
			Expect(pod.UID).NotTo(Equal(originalUID))

			Expect(pod.Spec.Containers).To(HaveLen(1))
			newContainer := pod.Spec.Containers[0]
			newEnvMap := make(map[string]string)
			for _, env := range newContainer.Env {
				if env.Value != "" {
					newEnvMap[env.Name] = env.Value
				}
			}
			Expect(newEnvMap["POLL_INTERVAL"]).To(Equal("120"))
			Expect(newEnvMap["IMAGE_BASE"]).To(Equal("ghcr.io/neworg/new-policies:v2.0"))
		})

		It("should set owner reference on the pod for automatic cleanup", func() {
			By("Reconciling to create the initial pod")
			controllerReconciler := &KyvernoArtifactReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				Config: DefaultConfig(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Reconciling again in case the pod was deleted for recreation")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying the pod exists with correct owner reference")
			podName := fmt.Sprintf("kyverno-artifact-manager-%s", resourceName)
			pod := &corev1.Pod{}
			Eventually(func() error {
				return k8sClient.Get(ctx, client.ObjectKey{Name: podName, Namespace: "default"}, pod)
			}, timeout, interval).Should(Succeed())

			By("Checking that the pod has an owner reference to the KyvernoArtifact")
			Expect(pod.OwnerReferences).To(HaveLen(1))
			ownerRef := pod.OwnerReferences[0]
			Expect(ownerRef.Kind).To(Equal("KyvernoArtifact"))
			Expect(ownerRef.Name).To(Equal(resourceName))
			Expect(*ownerRef.Controller).To(BeTrue())
			Expect(*ownerRef.BlockOwnerDeletion).To(BeTrue())
		})
	})
})
