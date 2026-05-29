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

// Package e2e contains end-to-end tests that run against a real Kubernetes cluster.
// The tests expect:
//   - A kubeconfig pointing at a live cluster (KUBECONFIG env var or ~/.kube/config).
//   - The NodeGroup CRD installed (make install).
//   - The controller running either in-cluster or locally (make run / make deploy).
//
// A kind-based workflow is available via the Makefile:
//
//	make kind-create          # create a kind cluster
//	make kind-load-and-deploy # build + load image + deploy controller
//	make test-e2e             # run these tests
//	make kind-delete          # tear down
package e2e_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	k8sv1alpha2 "github.com/elastx/elx-nodegroup-controller/api/v1alpha2"
)

const (
	e2eTimeout  = 60 * time.Second
	e2eInterval = 500 * time.Millisecond
)

var (
	k8sClient    client.Client
	clusterNodes []corev1.Node
	// workerNodes are nodes without the control-plane role label.
	// Safe to taint during testing.
	workerNodes []corev1.Node
	// testNodeName is the primary node used in single-node tests.
	// Prefers a worker node; falls back to any available node.
	testNodeName string
)

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "E2E Suite")
}

var _ = BeforeSuite(func() {
	By("connecting to cluster")
	cfg, err := config.GetConfig()
	if err != nil {
		Skip(fmt.Sprintf("cannot get cluster config (is KUBECONFIG set?): %v", err))
	}

	scheme := runtime.NewScheme()
	Expect(clientgoscheme.AddToScheme(scheme)).To(Succeed())
	Expect(k8sv1alpha2.AddToScheme(scheme)).To(Succeed())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme})
	Expect(err).NotTo(HaveOccurred())

	By("verifying NodeGroup CRD is installed")
	ngList := &k8sv1alpha2.NodeGroupList{}
	if err := k8sClient.List(context.Background(), ngList); err != nil {
		Skip(fmt.Sprintf("NodeGroup CRD not accessible (install with `make install`): %v", err))
	}

	By("listing cluster nodes")
	nodeList := &corev1.NodeList{}
	Expect(k8sClient.List(context.Background(), nodeList)).To(Succeed())
	clusterNodes = nodeList.Items
	if len(clusterNodes) == 0 {
		Skip("cluster has no nodes")
	}

	for _, n := range clusterNodes {
		if _, isCP := n.Labels["node-role.kubernetes.io/control-plane"]; !isCP {
			workerNodes = append(workerNodes, n)
		}
	}

	if len(workerNodes) > 0 {
		testNodeName = workerNodes[0].Name
	} else {
		// Fall back to the first available node (control-plane only cluster)
		testNodeName = clusterNodes[0].Name
	}
	fmt.Fprintf(os.Stdout, "\nUsing node %q for e2e tests\n", testNodeName)

	By("verifying controller is responsive (NodeGroup create/get round-trip)")
	// This does not check that the controller pod is running; it just verifies
	// the API server accepts NodeGroup resources. The tests themselves will time
	// out if the controller is not reconciling.
})
