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
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	k8sv1alpha2 "github.com/elastx/elx-nodegroup-controller/api/v1alpha2"
)

// reconcilerScheme returns a scheme with all required types registered.
func reconcilerScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := k8sv1alpha2.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	return s
}

// TestReconcile_NodeGroupGetError covers the non-NotFound error path when
// fetching the NodeGroup at the start of Reconcile.
func TestReconcile_NodeGroupGetError(t *testing.T) {
	s := reconcilerScheme(t)

	injected := errors.New("injected get error")
	fc := fake.NewClientBuilder().WithScheme(s).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(_ context.Context, _ client.WithWatch, _ types.NamespacedName, _ client.Object, _ ...client.GetOption) error {
				return injected
			},
		}).Build()

	r := &NodeGroupReconciler{Client: fc, Scheme: s}
	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "does-not-matter"},
	})
	if !errors.Is(err, injected) {
		t.Errorf("expected injected error, got %v", err)
	}
}

// TestReconcile_NodeListError covers the error path when listing all nodes fails.
func TestReconcile_NodeListError(t *testing.T) {
	s := reconcilerScheme(t)

	ng := &k8sv1alpha2.NodeGroup{
		ObjectMeta: metav1.ObjectMeta{Name: "ng-list-err"},
		Spec: k8sv1alpha2.NodeGroupSpec{
			Members: []string{"node1"},
			Labels:  map[string]string{"k": "v"},
		},
	}

	injected := errors.New("injected list error")
	callCount := 0
	fc := fake.NewClientBuilder().WithScheme(s).WithObjects(ng).
		WithInterceptorFuncs(interceptor.Funcs{
			List: func(_ context.Context, _ client.WithWatch, list client.ObjectList, _ ...client.ListOption) error {
				// Only intercept Node lists so NodeGroup lists still work.
				if _, ok := list.(*corev1.NodeList); ok {
					callCount++
					return injected
				}
				return nil
			},
		}).Build()

	r := &NodeGroupReconciler{Client: fc, Scheme: s}
	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: ng.Name},
	})
	if !errors.Is(err, injected) {
		t.Errorf("expected injected error, got %v", err)
	}
}

// TestReconcile_FinalizerUpdateError covers the error path when updating the
// NodeGroup to add the finalizer fails.
func TestReconcile_FinalizerUpdateError(t *testing.T) {
	s := reconcilerScheme(t)

	ng := &k8sv1alpha2.NodeGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "ng-update-err",
			Finalizers: []string{},
		},
		Spec: k8sv1alpha2.NodeGroupSpec{},
	}

	injected := errors.New("injected update error")
	fc := fake.NewClientBuilder().WithScheme(s).WithObjects(ng).
		WithInterceptorFuncs(interceptor.Funcs{
			Update: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.UpdateOption) error {
				return injected
			},
		}).Build()

	r := &NodeGroupReconciler{Client: fc, Scheme: s}
	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: ng.Name},
	})
	if !errors.Is(err, injected) {
		t.Errorf("expected injected error, got %v", err)
	}
}

// TestFindNodeGroupsForMember_ListError covers the error path in
// findNodeGroupsForMember when listing NodeGroups fails.
func TestFindNodeGroupsForMember_ListError(t *testing.T) {
	s := reconcilerScheme(t)

	injected := errors.New("injected list error")
	fc := fake.NewClientBuilder().WithScheme(s).
		WithInterceptorFuncs(interceptor.Funcs{
			List: func(_ context.Context, _ client.WithWatch, _ client.ObjectList, _ ...client.ListOption) error {
				return injected
			},
		}).Build()

	r := &NodeGroupReconciler{Client: fc, Scheme: s}
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "any-node"}}

	requests := r.findNodeGroupsForMember(context.Background(), node)
	if len(requests) != 0 {
		t.Errorf("expected empty requests on error, got %d", len(requests))
	}
}

// TestReconcile_FinalizationNodeLabelUpdateError covers the error path when
// updating a node to remove a managed label during finalization fails.
// The controller returns the error so controller-runtime applies rate-limited requeue.
func TestReconcile_FinalizationNodeLabelUpdateError(t *testing.T) {
	s := reconcilerScheme(t)

	now := metav1.Now()
	ng := &k8sv1alpha2.NodeGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "ng-fin-label-update-err",
			DeletionTimestamp: &now,
			Finalizers:        []string{finalizer},
			ResourceVersion:   "1",
		},
		Spec: k8sv1alpha2.NodeGroupSpec{
			Members: []string{"fin-node"},
			Labels:  map[string]string{"managed": "yes"},
		},
	}
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "fin-node",
			Labels:          map[string]string{"managed": "yes"},
			ResourceVersion: "1",
		},
	}

	injected := errors.New("injected node update error")
	fc := fake.NewClientBuilder().WithScheme(s).WithObjects(ng, node).
		WithInterceptorFuncs(interceptor.Funcs{
			Update: func(_ context.Context, _ client.WithWatch, obj client.Object, _ ...client.UpdateOption) error {
				if _, isNode := obj.(*corev1.Node); isNode {
					return injected
				}
				return nil
			},
		}).Build()

	r := &NodeGroupReconciler{Client: fc, Scheme: s}
	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: ng.Name},
	})
	if !errors.Is(err, injected) {
		t.Errorf("expected injected error, got %v", err)
	}
}

// TestReconcile_FinalizationNodeTaintUpdateError covers the error path when
// updating a node to remove a managed taint during finalization fails.
func TestReconcile_FinalizationNodeTaintUpdateError(t *testing.T) {
	s := reconcilerScheme(t)

	taint := corev1.Taint{Key: "test/taint", Value: "yes", Effect: corev1.TaintEffectNoSchedule}

	now := metav1.Now()
	ng := &k8sv1alpha2.NodeGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "ng-fin-taint-update-err",
			DeletionTimestamp: &now,
			Finalizers:        []string{finalizer},
			ResourceVersion:   "1",
		},
		Spec: k8sv1alpha2.NodeGroupSpec{
			Members: []string{"fin-taint-node"},
			// No labels — skip to taint finalization on first reconcile
			Taints: []corev1.Taint{taint},
		},
	}
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "fin-taint-node",
			ResourceVersion: "1",
		},
		Spec: corev1.NodeSpec{
			Taints: []corev1.Taint{taint},
		},
	}

	injected := errors.New("injected taint node update error")
	fc := fake.NewClientBuilder().WithScheme(s).WithObjects(ng, node).
		WithInterceptorFuncs(interceptor.Funcs{
			Update: func(_ context.Context, _ client.WithWatch, obj client.Object, _ ...client.UpdateOption) error {
				if _, isNode := obj.(*corev1.Node); isNode {
					return injected
				}
				return nil
			},
		}).Build()

	r := &NodeGroupReconciler{Client: fc, Scheme: s}
	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: ng.Name},
	})
	if !errors.Is(err, injected) {
		t.Errorf("expected injected error, got %v", err)
	}
}

// TestReconcile_FinalizationFinalierRemovalError covers the error path when
// updating the NodeGroup to remove the finalizer fails after node cleanup.
func TestReconcile_FinalizationFinalizerRemovalError(t *testing.T) {
	s := reconcilerScheme(t)

	now := metav1.Now()
	ng := &k8sv1alpha2.NodeGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "ng-fin-finalizer-err",
			DeletionTimestamp: &now,
			Finalizers:        []string{finalizer},
			ResourceVersion:   "1",
		},
		// No labels or taints — node cleanup is a no-op, goes straight to finalizer removal.
		Spec: k8sv1alpha2.NodeGroupSpec{
			Members: []string{"fin-clean-node"},
		},
	}
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "fin-clean-node",
			ResourceVersion: "1",
		},
	}

	injected := errors.New("injected nodegroup update error")
	fc := fake.NewClientBuilder().WithScheme(s).WithObjects(ng, node).
		WithInterceptorFuncs(interceptor.Funcs{
			Update: func(_ context.Context, _ client.WithWatch, obj client.Object, _ ...client.UpdateOption) error {
				if _, isNG := obj.(*k8sv1alpha2.NodeGroup); isNG {
					return injected
				}
				return nil
			},
		}).Build()

	r := &NodeGroupReconciler{Client: fc, Scheme: s}
	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: ng.Name},
	})
	if !errors.Is(err, injected) {
		t.Errorf("expected injected error, got %v", err)
	}
}
