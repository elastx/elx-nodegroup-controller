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

package controllers

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/elastx/elx-nodegroup-controller/api/v1alpha2"
)

// Additional integration tests that extend nodegroup_controller_test.go.
// Each Context manages its own nodes and NodeGroups to avoid conflicts with the base test suite.

var _ = Describe("NodeGroup controller - extended", func() {
	const (
		timeout  = time.Second * 15
		interval = time.Millisecond * 250
	)

	Context("nodeGroupNames dynamic discovery", func() {
		var (
			matchedNode1 corev1.Node
			matchedNode2 corev1.Node
			unmatchedNode corev1.Node
			ng           v1alpha2.NodeGroup
		)

		BeforeEach(func() {
			// Nodes whose names contain "xgn" as a dash-separated segment
			matchedNode1 = corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "extra-xgn-worker-a"}}
			matchedNode2 = corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "extra-xgn-worker-b"}}
			// Node that must NOT be matched
			unmatchedNode = corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "extra-other-worker"}}

			for _, n := range []corev1.Node{matchedNode1, matchedNode2, unmatchedNode} {
				node := n
				Expect(k8sClient.Create(context.Background(), &node)).To(Succeed())
			}

			ng = v1alpha2.NodeGroup{
				ObjectMeta: metav1.ObjectMeta{Name: "extra-ng-xgn"},
				Spec: v1alpha2.NodeGroupSpec{
					NodeGroupNames: []string{"xgn"},
					Labels:         map[string]string{"extra-group": "xgn"},
				},
			}
			Expect(k8sClient.Create(context.Background(), &ng)).To(Succeed())
		})

		AfterEach(func() {
			ngFetched := &v1alpha2.NodeGroup{}
			if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: ng.Name}, ngFetched); err == nil {
				k8sClient.Delete(context.Background(), ngFetched) //nolint:errcheck
			}
			for _, name := range []string{matchedNode1.Name, matchedNode2.Name, unmatchedNode.Name} {
				n := &corev1.Node{}
				if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: name}, n); err == nil {
					k8sClient.Delete(context.Background(), n) //nolint:errcheck
				}
			}
		})

		It("applies labels to nodes matching nodeGroupNames and not to others", func() {
			n := &corev1.Node{}

			By("waiting for matched nodes to receive the label")
			for _, name := range []string{matchedNode1.Name, matchedNode2.Name} {
				nodeName := name
				Eventually(func() string {
					if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: nodeName}, n); err != nil {
						return ""
					}
					return n.Labels["extra-group"]
				}, timeout, interval).Should(Equal("xgn"), "node %s should have label extra-group=xgn", nodeName)
			}

			By("verifying the unmatched node did not receive the label")
			Consistently(func() string {
				if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: unmatchedNode.Name}, n); err != nil {
					return ""
				}
				return n.Labels["extra-group"]
			}, time.Second*2, interval).Should(BeEmpty(), "unmatched node should not have the label")
		})

		It("cleans up labels from matched nodes when NodeGroup is deleted", func() {
			n := &corev1.Node{}

			By("waiting for labels to be applied")
			Eventually(func() string {
				if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: matchedNode1.Name}, n); err != nil {
					return ""
				}
				return n.Labels["extra-group"]
			}, timeout, interval).Should(Equal("xgn"))

			By("deleting the NodeGroup")
			ngFetched := &v1alpha2.NodeGroup{}
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: ng.Name}, ngFetched)).To(Succeed())
			Expect(k8sClient.Delete(context.Background(), ngFetched)).To(Succeed())

			By("verifying labels are removed from matched nodes")
			Eventually(func() bool {
				if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: matchedNode1.Name}, n); err != nil {
					return false
				}
				_, hasLabel := n.Labels["extra-group"]
				return !hasLabel
			}, timeout, interval).Should(BeTrue())
		})
	})

	Context("label value update", func() {
		var (
			node corev1.Node
			ng   v1alpha2.NodeGroup
		)

		BeforeEach(func() {
			node = corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "extra-label-update-node",
					Labels: map[string]string{"update-label": "old-value"},
				},
			}
			Expect(k8sClient.Create(context.Background(), &node)).To(Succeed())

			ng = v1alpha2.NodeGroup{
				ObjectMeta: metav1.ObjectMeta{Name: "extra-ng-label-update"},
				Spec: v1alpha2.NodeGroupSpec{
					Members: []string{node.Name},
					Labels:  map[string]string{"update-label": "new-value"},
				},
			}
			Expect(k8sClient.Create(context.Background(), &ng)).To(Succeed())
		})

		AfterEach(func() {
			ngFetched := &v1alpha2.NodeGroup{}
			if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: ng.Name}, ngFetched); err == nil {
				k8sClient.Delete(context.Background(), ngFetched) //nolint:errcheck
			}
			n := &corev1.Node{}
			if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: node.Name}, n); err == nil {
				k8sClient.Delete(context.Background(), n) //nolint:errcheck
			}
		})

		It("updates an existing label to the value in the spec", func() {
			n := &corev1.Node{}
			Eventually(func() string {
				if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: node.Name}, n); err != nil {
					return ""
				}
				return n.Labels["update-label"]
			}, timeout, interval).Should(Equal("new-value"))
		})
	})

	Context("node with nil labels", func() {
		var (
			node corev1.Node
			ng   v1alpha2.NodeGroup
		)

		BeforeEach(func() {
			// Create node without any labels (Labels field is nil)
			node = corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "extra-nil-labels-node"},
			}
			Expect(k8sClient.Create(context.Background(), &node)).To(Succeed())

			ng = v1alpha2.NodeGroup{
				ObjectMeta: metav1.ObjectMeta{Name: "extra-ng-nil-labels"},
				Spec: v1alpha2.NodeGroupSpec{
					Members: []string{node.Name},
					Labels:  map[string]string{"nil-test": "applied"},
				},
			}
			Expect(k8sClient.Create(context.Background(), &ng)).To(Succeed())
		})

		AfterEach(func() {
			ngFetched := &v1alpha2.NodeGroup{}
			if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: ng.Name}, ngFetched); err == nil {
				k8sClient.Delete(context.Background(), ngFetched) //nolint:errcheck
			}
			n := &corev1.Node{}
			if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: node.Name}, n); err == nil {
				k8sClient.Delete(context.Background(), n) //nolint:errcheck
			}
		})

		It("applies labels to a node that initially has no labels", func() {
			n := &corev1.Node{}
			Eventually(func() string {
				if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: node.Name}, n); err != nil {
					return ""
				}
				return n.Labels["nil-test"]
			}, timeout, interval).Should(Equal("applied"))
		})
	})

	Context("finalization preserves labels not managed by the NodeGroup", func() {
		var (
			node corev1.Node
			ng   v1alpha2.NodeGroup
		)

		BeforeEach(func() {
			// Node has a pre-existing label that the NodeGroup does not manage
			node = corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "extra-preserve-labels-node",
					Labels: map[string]string{"pre-existing": "keep-me"},
				},
			}
			Expect(k8sClient.Create(context.Background(), &node)).To(Succeed())

			ng = v1alpha2.NodeGroup{
				ObjectMeta: metav1.ObjectMeta{Name: "extra-ng-preserve"},
				Spec: v1alpha2.NodeGroupSpec{
					Members: []string{node.Name},
					Labels:  map[string]string{"managed": "yes"},
				},
			}
			Expect(k8sClient.Create(context.Background(), &ng)).To(Succeed())
		})

		AfterEach(func() {
			n := &corev1.Node{}
			if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: node.Name}, n); err == nil {
				k8sClient.Delete(context.Background(), n) //nolint:errcheck
			}
		})

		It("only removes managed labels during finalization, leaving pre-existing labels intact", func() {
			n := &corev1.Node{}

			By("waiting for the managed label to be applied")
			Eventually(func() string {
				if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: node.Name}, n); err != nil {
					return ""
				}
				return n.Labels["managed"]
			}, timeout, interval).Should(Equal("yes"))

			By("deleting the NodeGroup")
			ngFetched := &v1alpha2.NodeGroup{}
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: ng.Name}, ngFetched)).To(Succeed())
			Expect(k8sClient.Delete(context.Background(), ngFetched)).To(Succeed())

			By("verifying the managed label is removed and pre-existing label is kept")
			Eventually(func() map[string]string {
				if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: node.Name}, n); err != nil {
					return nil
				}
				return n.Labels
			}, timeout, interval).Should(And(
				HaveKey("pre-existing"),
				Not(HaveKey("managed")),
			))
		})
	})

	Context("mixed members and nodeGroupNames", func() {
		var (
			explicitNode    corev1.Node
			dynamicNode     corev1.Node
			nonMemberNode   corev1.Node
			ng              v1alpha2.NodeGroup
		)

		BeforeEach(func() {
			explicitNode = corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "extra-explicit-member"}}
			dynamicNode = corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "extra-dyngrp-worker"}}
			nonMemberNode = corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "extra-excluded-node"}}

			for _, n := range []corev1.Node{explicitNode, dynamicNode, nonMemberNode} {
				node := n
				Expect(k8sClient.Create(context.Background(), &node)).To(Succeed())
			}

			ng = v1alpha2.NodeGroup{
				ObjectMeta: metav1.ObjectMeta{Name: "extra-ng-mixed"},
				Spec: v1alpha2.NodeGroupSpec{
					Members:        []string{explicitNode.Name},
					NodeGroupNames: []string{"dyngrp"},
					Labels:         map[string]string{"mixed-test": "yes"},
				},
			}
			Expect(k8sClient.Create(context.Background(), &ng)).To(Succeed())
		})

		AfterEach(func() {
			ngFetched := &v1alpha2.NodeGroup{}
			if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: ng.Name}, ngFetched); err == nil {
				k8sClient.Delete(context.Background(), ngFetched) //nolint:errcheck
			}
			for _, name := range []string{explicitNode.Name, dynamicNode.Name, nonMemberNode.Name} {
				n := &corev1.Node{}
				if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: name}, n); err == nil {
					k8sClient.Delete(context.Background(), n) //nolint:errcheck
				}
			}
		})

		It("applies labels to both explicit members and nodeGroupNames-matched nodes", func() {
			n := &corev1.Node{}

			for _, name := range []string{explicitNode.Name, dynamicNode.Name} {
				nodeName := name
				Eventually(func() string {
					if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: nodeName}, n); err != nil {
						return ""
					}
					return n.Labels["mixed-test"]
				}, timeout, interval).Should(Equal("yes"), "node %s should have label", nodeName)
			}

			Consistently(func() string {
				if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: nonMemberNode.Name}, n); err != nil {
					return ""
				}
				return n.Labels["mixed-test"]
			}, time.Second*2, interval).Should(BeEmpty())
		})
	})

	Context("NodeGroup with no members and no nodeGroupNames", func() {
		var ng v1alpha2.NodeGroup

		BeforeEach(func() {
			ng = v1alpha2.NodeGroup{
				ObjectMeta: metav1.ObjectMeta{Name: "extra-ng-empty"},
				Spec: v1alpha2.NodeGroupSpec{
					Labels: map[string]string{"empty-group": "true"},
				},
			}
			Expect(k8sClient.Create(context.Background(), &ng)).To(Succeed())
		})

		AfterEach(func() {
			ngFetched := &v1alpha2.NodeGroup{}
			if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: ng.Name}, ngFetched); err == nil {
				k8sClient.Delete(context.Background(), ngFetched) //nolint:errcheck
			}
		})

		It("reconciles without error and adds a finalizer", func() {
			fetched := &v1alpha2.NodeGroup{}
			Eventually(func() bool {
				if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: ng.Name}, fetched); err != nil {
					return false
				}
				return len(fetched.Finalizers) == 1
			}, timeout, interval).Should(BeTrue())
		})
	})

	Context("finalization of both labels and taints", func() {
		var (
			node corev1.Node
			ng   v1alpha2.NodeGroup
		)
		taint := corev1.Taint{
			Key:    "extra.elx.cloud/finalize-both",
			Value:  "yes",
			Effect: corev1.TaintEffectNoSchedule,
		}

		BeforeEach(func() {
			node = corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "extra-finalize-both-node"}}
			Expect(k8sClient.Create(context.Background(), &node)).To(Succeed())

			ng = v1alpha2.NodeGroup{
				ObjectMeta: metav1.ObjectMeta{Name: "extra-ng-finalize-both"},
				Spec: v1alpha2.NodeGroupSpec{
					Members: []string{node.Name},
					Labels:  map[string]string{"finalize-both": "yes"},
					Taints:  []corev1.Taint{taint},
				},
			}
			Expect(k8sClient.Create(context.Background(), &ng)).To(Succeed())
		})

		AfterEach(func() {
			n := &corev1.Node{}
			if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: node.Name}, n); err == nil {
				k8sClient.Delete(context.Background(), n) //nolint:errcheck
			}
		})

		It("removes both labels and taints when the NodeGroup is deleted", func() {
			n := &corev1.Node{}

			By("waiting for both label and taint to be applied")
			Eventually(func() bool {
				if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: node.Name}, n); err != nil {
					return false
				}
				if n.Labels["finalize-both"] != "yes" {
					return false
				}
				for _, t := range n.Spec.Taints {
					if t.MatchTaint(&taint) {
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue())

			By("deleting the NodeGroup")
			ngFetched := &v1alpha2.NodeGroup{}
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: ng.Name}, ngFetched)).To(Succeed())
			Expect(k8sClient.Delete(context.Background(), ngFetched)).To(Succeed())

			By("verifying both label and taint are removed")
			Eventually(func() bool {
				if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: node.Name}, n); err != nil {
					return false
				}
				if _, hasLabel := n.Labels["finalize-both"]; hasLabel {
					return false
				}
				for _, t := range n.Spec.Taints {
					if t.MatchTaint(&taint) {
						return false
					}
				}
				return true
			}, timeout, interval).Should(BeTrue())
		})
	})
})
