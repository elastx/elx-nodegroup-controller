/*
Copyright 2022.

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

package e2e_test

import (
	"context"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	k8sv1alpha2 "github.com/elastx/elx-nodegroup-controller/api/v1alpha2"
)

// uniqueName returns a name that is unique per test run to prevent conflicts
// when tests are run multiple times without cluster cleanup.
func uniqueName(base string) string {
	return fmt.Sprintf("%s-%d", base, time.Now().UnixNano()%1_000_000)
}

// createNodeGroup creates a NodeGroup and registers a DeferCleanup to delete it.
func createNodeGroup(ng *k8sv1alpha2.NodeGroup) {
	Expect(k8sClient.Create(context.Background(), ng)).To(Succeed())
	DeferCleanup(func() {
		fetched := &k8sv1alpha2.NodeGroup{}
		if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: ng.Name}, fetched); err == nil {
			// Remove finalizer so deletion is immediate even if controller is not running
			fetched.Finalizers = nil
			_ = k8sClient.Update(context.Background(), fetched)
			_ = k8sClient.Delete(context.Background(), fetched)
		}
	})
}

var _ = Describe("NodeGroup e2e", func() {

	Describe("applying labels via spec.members", func() {
		It("applies labels to an explicit member node and removes them on deletion", func() {
			ngName := uniqueName("e2e-labels")
			labelKey := "e2e-test-label"
			labelVal := "from-nodegroup"

			ng := &k8sv1alpha2.NodeGroup{
				ObjectMeta: metav1.ObjectMeta{Name: ngName},
				Spec: k8sv1alpha2.NodeGroupSpec{
					Members: []string{testNodeName},
					Labels:  map[string]string{labelKey: labelVal},
				},
			}
			createNodeGroup(ng)

			By("waiting for the label to appear on the target node")
			node := &corev1.Node{}
			Eventually(func() string {
				if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: testNodeName}, node); err != nil {
					return ""
				}
				return node.Labels[labelKey]
			}, e2eTimeout, e2eInterval).Should(Equal(labelVal))

			By("deleting the NodeGroup")
			fetched := &k8sv1alpha2.NodeGroup{}
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: ngName}, fetched)).To(Succeed())
			Expect(k8sClient.Delete(context.Background(), fetched)).To(Succeed())

			By("waiting for the label to be removed from the node")
			Eventually(func() bool {
				if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: testNodeName}, node); err != nil {
					return false
				}
				_, hasLabel := node.Labels[labelKey]
				return !hasLabel
			}, e2eTimeout, e2eInterval).Should(BeTrue())
		})

		It("applies multiple labels simultaneously", func() {
			ngName := uniqueName("e2e-multilabel")
			ng := &k8sv1alpha2.NodeGroup{
				ObjectMeta: metav1.ObjectMeta{Name: ngName},
				Spec: k8sv1alpha2.NodeGroupSpec{
					Members: []string{testNodeName},
					Labels: map[string]string{
						"e2e-label-a": "value-a",
						"e2e-label-b": "value-b",
					},
				},
			}
			createNodeGroup(ng)

			node := &corev1.Node{}
			Eventually(func() bool {
				if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: testNodeName}, node); err != nil {
					return false
				}
				return node.Labels["e2e-label-a"] == "value-a" && node.Labels["e2e-label-b"] == "value-b"
			}, e2eTimeout, e2eInterval).Should(BeTrue())
		})

		It("re-applies labels that are manually removed from a node", func() {
			ngName := uniqueName("e2e-persistent")
			labelKey := "e2e-persistent-label"

			ng := &k8sv1alpha2.NodeGroup{
				ObjectMeta: metav1.ObjectMeta{Name: ngName},
				Spec: k8sv1alpha2.NodeGroupSpec{
					Members: []string{testNodeName},
					Labels:  map[string]string{labelKey: "persistent"},
				},
			}
			createNodeGroup(ng)

			node := &corev1.Node{}
			By("waiting for label to be applied")
			Eventually(func() string {
				if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: testNodeName}, node); err != nil {
					return ""
				}
				return node.Labels[labelKey]
			}, e2eTimeout, e2eInterval).Should(Equal("persistent"))

			By("manually removing the label")
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: testNodeName}, node)).To(Succeed())
			delete(node.Labels, labelKey)
			Expect(k8sClient.Update(context.Background(), node)).To(Succeed())

			By("waiting for the controller to re-apply the label")
			Eventually(func() string {
				if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: testNodeName}, node); err != nil {
					return ""
				}
				return node.Labels[labelKey]
			}, e2eTimeout, e2eInterval).Should(Equal("persistent"))
		})
	})

	Describe("applying taints via spec.members", func() {
		var taintNode string

		BeforeEach(func() {
			// Prefer a worker node for taint tests so as not to disrupt scheduling
			// on a control-plane-only cluster.
			if len(workerNodes) > 0 {
				taintNode = workerNodes[0].Name
			} else {
				// Only control-plane available — use PreferNoSchedule to be safe.
				taintNode = testNodeName
			}
		})

		It("applies a NoSchedule taint to a member node and removes it on deletion", func() {
			ngName := uniqueName("e2e-taint")
			taint := corev1.Taint{
				Key:    "e2e-test-taint",
				Value:  "true",
				Effect: corev1.TaintEffectNoSchedule,
			}

			ng := &k8sv1alpha2.NodeGroup{
				ObjectMeta: metav1.ObjectMeta{Name: ngName},
				Spec: k8sv1alpha2.NodeGroupSpec{
					Members: []string{taintNode},
					Taints:  []corev1.Taint{taint},
				},
			}
			createNodeGroup(ng)

			node := &corev1.Node{}
			By("waiting for the taint to appear on the node")
			Eventually(func() bool {
				if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: taintNode}, node); err != nil {
					return false
				}
				for _, t := range node.Spec.Taints {
					if t.MatchTaint(&taint) {
						return true
					}
				}
				return false
			}, e2eTimeout, e2eInterval).Should(BeTrue())

			By("deleting the NodeGroup")
			fetched := &k8sv1alpha2.NodeGroup{}
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: ngName}, fetched)).To(Succeed())
			Expect(k8sClient.Delete(context.Background(), fetched)).To(Succeed())

			By("waiting for the taint to be removed from the node")
			Eventually(func() bool {
				if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: taintNode}, node); err != nil {
					return false
				}
				for _, t := range node.Spec.Taints {
					if t.MatchTaint(&taint) {
						return false
					}
				}
				return true
			}, e2eTimeout, e2eInterval).Should(BeTrue())
		})

		It("re-applies a taint that is manually removed from a node", func() {
			ngName := uniqueName("e2e-taint-persistent")
			taint := corev1.Taint{
				Key:    "e2e-persistent-taint",
				Value:  "yes",
				Effect: corev1.TaintEffectPreferNoSchedule,
			}

			ng := &k8sv1alpha2.NodeGroup{
				ObjectMeta: metav1.ObjectMeta{Name: ngName},
				Spec: k8sv1alpha2.NodeGroupSpec{
					Members: []string{taintNode},
					Taints:  []corev1.Taint{taint},
				},
			}
			createNodeGroup(ng)

			node := &corev1.Node{}
			By("waiting for taint to be applied")
			Eventually(func() bool {
				if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: taintNode}, node); err != nil {
					return false
				}
				for _, t := range node.Spec.Taints {
					if t.MatchTaint(&taint) {
						return true
					}
				}
				return false
			}, e2eTimeout, e2eInterval).Should(BeTrue())

			By("manually removing the taint")
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: taintNode}, node)).To(Succeed())
			filtered := []corev1.Taint{}
			for _, t := range node.Spec.Taints {
				if !t.MatchTaint(&taint) {
					filtered = append(filtered, t)
				}
			}
			node.Spec.Taints = filtered
			Expect(k8sClient.Update(context.Background(), node)).To(Succeed())

			By("waiting for the controller to re-apply the taint")
			Eventually(func() bool {
				if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: taintNode}, node); err != nil {
					return false
				}
				for _, t := range node.Spec.Taints {
					if t.MatchTaint(&taint) {
						return true
					}
				}
				return false
			}, e2eTimeout, e2eInterval).Should(BeTrue())
		})
	})

	Describe("dynamic membership via spec.nodeGroupNames", func() {
		It("applies labels to nodes whose names contain a matching segment", func() {
			// Find nodes whose names contain a unique segment we can target.
			// For a kind cluster, node names look like "kind-control-plane" or "kind-worker".
			// We pick a unique segment from the first node's name.
			if len(clusterNodes) == 0 {
				Skip("no nodes available")
			}
			targetNode := clusterNodes[0]
			nameParts := strings.Split(targetNode.Name, "-")
			if len(nameParts) == 0 {
				Skip("cannot extract a name segment from node name: " + targetNode.Name)
			}
			// Use the last segment as the group name (e.g. "plane" from "kind-control-plane")
			segment := nameParts[len(nameParts)-1]

			ngName := uniqueName("e2e-ngnames")
			labelKey := "e2e-ngnames-label"

			ng := &k8sv1alpha2.NodeGroup{
				ObjectMeta: metav1.ObjectMeta{Name: ngName},
				Spec: k8sv1alpha2.NodeGroupSpec{
					NodeGroupNames: []string{segment},
					Labels:         map[string]string{labelKey: "from-ngnames"},
				},
			}
			createNodeGroup(ng)

			node := &corev1.Node{}
			Eventually(func() string {
				if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: targetNode.Name}, node); err != nil {
					return ""
				}
				return node.Labels[labelKey]
			}, e2eTimeout, e2eInterval).Should(Equal("from-ngnames"))
		})
	})

	Describe("NodeGroup finalizer lifecycle", func() {
		It("adds a finalizer to a newly created NodeGroup", func() {
			ngName := uniqueName("e2e-finalizer")
			ng := &k8sv1alpha2.NodeGroup{
				ObjectMeta: metav1.ObjectMeta{Name: ngName},
				Spec: k8sv1alpha2.NodeGroupSpec{
					Labels: map[string]string{"e2e-finalizer": "true"},
				},
			}
			createNodeGroup(ng)

			fetched := &k8sv1alpha2.NodeGroup{}
			Eventually(func() bool {
				if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: ngName}, fetched); err != nil {
					return false
				}
				return len(fetched.Finalizers) > 0
			}, e2eTimeout, e2eInterval).Should(BeTrue())

			Expect(fetched.Finalizers).To(ContainElement("k8s.elx.cloud/finalizer"))
		})

		It("removes the finalizer and allows deletion once nodes are cleaned up", func() {
			ngName := uniqueName("e2e-del")
			ng := &k8sv1alpha2.NodeGroup{
				ObjectMeta: metav1.ObjectMeta{Name: ngName},
				Spec: k8sv1alpha2.NodeGroupSpec{
					Members: []string{testNodeName},
					Labels:  map[string]string{"e2e-del-label": "yes"},
				},
			}
			Expect(k8sClient.Create(context.Background(), ng)).To(Succeed())

			By("waiting for the label to appear (confirms controller is active)")
			node := &corev1.Node{}
			Eventually(func() string {
				if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: testNodeName}, node); err != nil {
					return ""
				}
				return node.Labels["e2e-del-label"]
			}, e2eTimeout, e2eInterval).Should(Equal("yes"))

			By("deleting the NodeGroup")
			Expect(k8sClient.Delete(context.Background(), ng)).To(Succeed())

			By("waiting for the NodeGroup to be fully deleted (finalizer removed)")
			Eventually(func() bool {
				fetched := &k8sv1alpha2.NodeGroup{}
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: ngName}, fetched)
				return err != nil // object gone = true
			}, e2eTimeout, e2eInterval).Should(BeTrue())

			By("verifying the label was removed from the node")
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: testNodeName}, node)).To(Succeed())
			Expect(node.Labels).NotTo(HaveKey("e2e-del-label"))
		})
	})
})
