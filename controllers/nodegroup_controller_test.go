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

var _ = Describe("NodeGroup controller", func() {
	const (
		timeout  = time.Second * 10
		duration = time.Second * 10
		interval = time.Millisecond * 250
	)

	var (
		nodes = []corev1.Node{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "node1",
					Labels: map[string]string{},
				},
				Spec: corev1.NodeSpec{},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "node2",
					Labels: map[string]string{},
				},
				Spec: corev1.NodeSpec{},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "node3",
					Labels: map[string]string{},
				},
				Spec: corev1.NodeSpec{},
			},
		}
		taint = &corev1.Taint{
			Key:    "k8x.elx.cloud/test",
			Value:  "tainted",
			Effect: "NoSchedule",
		}
		nodeGroups = []v1alpha1.NodeGroup{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "nodegroup1",
				},
				Spec: v1alpha1.NodeGroupSpec{
					Members: []string{"node1"},
					Labels: map[string]string{
						"nodegroup1": "value1",
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "nodegroup2",
				},
				Spec: v1alpha1.NodeGroupSpec{
					Members: []string{"node2"},
					Labels: map[string]string{
						"nodegroup2": "value2",
					},
					Taints: []corev1.Taint{*taint},
				},
			},
		}
	)

	BeforeEach(func() {
		for _, node := range nodes {
			Expect(k8sClient.Create(context.Background(), &node)).Should(Succeed())
			n := &corev1.Node{}
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: node.Name}, n)
				if err != nil {
					return false
				}
				return true
			}, timeout, interval).Should(BeTrue())
		}
		for _, nodeGroup := range nodeGroups {
			Expect(k8sClient.Create(context.Background(), &nodeGroup)).Should(Succeed())
			ng := &v1alpha1.NodeGroup{}
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: nodeGroup.Name}, ng)
				if err != nil {
					return false
				}
				return true
			}, timeout, interval).Should(BeTrue())
		}
	})

	AfterEach(func() {
		for _, node := range nodes {
			Expect(k8sClient.Delete(context.Background(), &node)).Should(Succeed())
		}
		for _, nodeGroup := range nodeGroups {
			k8sClient.Delete(context.Background(), &nodeGroup)
		}
	})

	Context("NodeGroups", func() {
		It("should add a finalizer to NodeGroups", func() {
			ng := &v1alpha1.NodeGroup{}
			for _, nodeGroup := range nodeGroups {
				Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: nodeGroup.Name}, ng)).Should(Succeed())
				Expect(ng.Finalizers).Should(HaveLen(1))
			}
		})

		It("should add labels to member nodes", func() {
			n := &corev1.Node{}
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: "node1"}, n)
				if err != nil {
					return false
				}
				return n.GetLabels()["nodegroup1"] == "value1"
			}, timeout, interval).Should(BeTrue())
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: "node2"}, n)
				if err != nil {
					return false
				}
				return n.GetLabels()["nodegroup2"] == "value2"
			}, timeout, interval).Should(BeTrue())
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: "node3"}, n)).Should(Succeed())
			Expect(n.GetLabels()).To(BeNil())
		})
		It("should add taints to member nodes", func() {
			n := &corev1.Node{}
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: "node1"}, n)
				if err != nil {
					return false
				}
				for _, t := range n.Spec.Taints {
					if t.MatchTaint(taint) {
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeFalse())
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: "node2"}, n)
				if err != nil {
					return false
				}
				for _, t := range n.Spec.Taints {
					if t.MatchTaint(taint) {
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue())
		})
		It("should remove labels and taints during finalization", func() {
			n := &corev1.Node{}
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: "node2"}, n)
				if err != nil {
					return false
				}
				for _, t := range n.Spec.Taints {
					if t.MatchTaint(taint) {
						return n.GetLabels()["nodegroup2"] == "value2"
					}
				}
				return n.GetLabels()["nodegroup2"] == "value2"
			})
			ng := &v1alpha1.NodeGroup{}
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: "nodegroup2"}, ng)).To(Succeed())
			Expect(k8sClient.Delete(context.Background(), ng)).To(Succeed())
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: "node2"}, n)
				if err != nil {
					return false
				}
				for _, t := range n.Spec.Taints {
					if t.MatchTaint(taint) {
						return false
					}
				}
				if _, ok := n.GetLabels()["nodegroup2"]; ok {
					return false
				}
				return true
			})
		})
		It("should maintain labels and taints on nodes", func() {
			node := &corev1.Node{}
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: "node2"}, node)).To(Succeed())
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: "node2"}, node)
				if err != nil {
					return false
				}
				if _, ok := node.GetLabels()["nodegroup2"]; !ok {
					return false
				}
				for _, t := range node.Spec.Taints {
					if t.MatchTaint(taint) {
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue())
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: "node2"}, node)).To(Succeed())
			node.Spec.Taints = []corev1.Taint{}
			Expect(k8sClient.Update(context.Background(), node)).To(Succeed())
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: "node2"}, node)).To(Succeed())
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: "node2"}, node)
				if err != nil {
					return false
				}
				if _, ok := node.GetLabels()["nodegroup2"]; !ok {
					return false
				}
				for _, t := range node.Spec.Taints {
					if t.MatchTaint(taint) {
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue())
		})
		It("should maintain labels and taints when nodes are recreated", func() {
			node := &corev1.Node{}
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: "node2"}, node)).To(Succeed())
			Expect(k8sClient.Delete(context.Background(), node)).To(Succeed())
			Expect(k8sClient.Create(context.Background(), &nodes[1])).To(Succeed())
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: "node2"}, node)
				if err != nil {
					return false
				}
				if _, ok := node.GetLabels()["nodegroup2"]; !ok {
					return false
				}
				for _, t := range node.Spec.Taints {
					if t.MatchTaint(taint) {
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue())
		})
	})
})
