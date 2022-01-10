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

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	k8sv1alpha1 "github.com/elastx/elx-nodegroup-controller/api/v1alpha1"
)

const (
	membersField = ".spec.members"
	finalizer    = "k8s.elx.cloud/finalizer"
)

type NodeGroupReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=k8s.elx.cloud,resources=nodegroups,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=k8s.elx.cloud,resources=nodegroups/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=k8s.elx.cloud,resources=nodegroups/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch;update;patch

func stringIn(s string, a []string) bool {
	for _, t := range a {
		if s == t {
			return true
		}
	}
	return false
}

func taintPos(taint *corev1.Taint, taints []corev1.Taint) int {
	for i, t := range taints {
		if t.MatchTaint(taint) {
			return i
		}
	}
	return -1
}

func hasTaint(taint *corev1.Taint, node *corev1.Node) bool {
	return taintPos(taint, node.Spec.Taints) > -1
}

func finalizeNodeLabels(node *corev1.Node, labels map[string]string) bool {
	needsUpdate := false
	currentLabels := node.GetLabels()
	for l, v := range labels {
		cv, ok := currentLabels[l]
		if ok && cv == v {
			delete(node.Labels, l)
			needsUpdate = true
		}
	}
	return needsUpdate
}

func reconcileNodeLabels(node *corev1.Node, labels map[string]string) bool {
	log := log.FromContext(context.Background())
	needsUpdate := false
	currentLabels := node.GetLabels()
	for l, v := range labels {
		log.V(1).Info("considering label", "node", node.Name, "label", l)
		cv, ok := currentLabels[l]
		if !ok {
			log.V(1).Info("adding label", "node", node.Name, "label", l, "value", v)
			if currentLabels == nil {
				currentLabels = map[string]string{l: v}
			} else {
				currentLabels[l] = v
			}
			node.SetLabels(currentLabels)
			needsUpdate = true
		} else {
			if v != cv {
				log.V(1).Info("updating label", "node", node.Name, "label", l, "value", v)
				node.Labels[l] = v
				needsUpdate = true
			}
		}
	}
	return needsUpdate
}

func finalizeNodeTaints(node *corev1.Node, taints []corev1.Taint) bool {
	needsUpdate := false
	for _, t := range taints {
		if pos := taintPos(&t, node.Spec.Taints); pos > -1 {
			node.Spec.Taints[pos] = node.Spec.Taints[len(node.Spec.Taints)-1]
			node.Spec.Taints = node.Spec.Taints[:len(node.Spec.Taints)-1]
			needsUpdate = true
		}
	}
	return needsUpdate
}

func reconcileNodeTaints(node *corev1.Node, taints []corev1.Taint) bool {
	needsUpdate := false
	for _, t := range taints {
		if !hasTaint(&t, node) {
			node.Spec.Taints = append(node.Spec.Taints, t)
			needsUpdate = true
		}
	}
	return needsUpdate
}

func (r *NodeGroupReconciler) findNodeGroupsForMember(node client.Object) []reconcile.Request {
	log := log.FromContext(context.Background())
	nodeGroups := &k8sv1alpha1.NodeGroupList{}
	listOpts := &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(membersField, node.GetName()),
	}
	err := r.List(context.Background(), nodeGroups, listOpts)
	if err != nil {
		return []reconcile.Request{}
	}

	requests := make([]reconcile.Request, len(nodeGroups.Items))
	for i, item := range nodeGroups.Items {
		requests[i] = reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name: item.GetName(),
			},
		}
	}
	log.Info("reconcile requests returned", "requests", requests)
	return requests
}

func (r *NodeGroupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	log.V(1).Info("reconciling node group", "nodeGroup", req.NamespacedName)
	var nodeGroup k8sv1alpha1.NodeGroup
	if err := r.Get(ctx, req.NamespacedName, &nodeGroup); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		log.Error(err, "unable to fetch nodeGroup", "nodeGroup", req.NamespacedName)

		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	node := &corev1.Node{}

	if nodeGroup.ObjectMeta.DeletionTimestamp.IsZero() {
		if !stringIn(finalizer, nodeGroup.GetFinalizers()) {
			log.Info("nodeGroup is missing finalizer")
			controllerutil.AddFinalizer(&nodeGroup, finalizer)
			if err := r.Update(ctx, &nodeGroup); err != nil {
				log.Info("error updating nodeGroup", "error", err)
				return ctrl.Result{}, err
			}
		}
	} else {
		if stringIn(finalizer, nodeGroup.GetFinalizers()) {
			for _, n := range nodeGroup.Spec.Members {
				err := r.Get(ctx, types.NamespacedName{Name: n}, node)
				if err != nil {
					return ctrl.Result{}, err
				}
				if needsUpdate := finalizeNodeLabels(node, nodeGroup.Spec.Labels); needsUpdate {
					if err := r.Update(ctx, node); err != nil {
						log.Error(err, "unable to update Node", "node", n)
					}
					return ctrl.Result{Requeue: true}, nil
				}
				if needsUpdate := finalizeNodeTaints(node, nodeGroup.Spec.Taints); needsUpdate {
					if err := r.Update(ctx, node); err != nil {
						log.Error(err, "unable to update Node", "node", n)
					}
					return ctrl.Result{Requeue: true}, nil
				}
			}
			controllerutil.RemoveFinalizer(&nodeGroup, finalizer)
			if err := r.Update(ctx, &nodeGroup); err != nil {
				log.Info("error updating nodeGroup", "error", err)
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	for _, n := range nodeGroup.Spec.Members {
		err := r.Get(ctx, types.NamespacedName{Name: n}, node)
		if err != nil {
			return ctrl.Result{}, err
		}
		log.V(1).Info("reconciling node", "node", n)
		if needsUpdate := reconcileNodeLabels(node, nodeGroup.Spec.Labels); needsUpdate {
			if err := r.Update(ctx, node); err != nil {
				log.Error(err, "unable to update Node", "node", n)
			}
			return ctrl.Result{Requeue: true}, nil
		}
		if needsUpdate := reconcileNodeTaints(node, nodeGroup.Spec.Taints); needsUpdate {
			if err := r.Update(ctx, node); err != nil {
				log.Error(err, "unable to update Node", "node", n)
			}
			return ctrl.Result{Requeue: true}, nil
		}
	}

	return ctrl.Result{}, nil
}

func (r *NodeGroupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &k8sv1alpha1.NodeGroup{}, membersField, func(rawObj client.Object) []string {
		nodeGroup := rawObj.(*k8sv1alpha1.NodeGroup)
		if nodeGroup.Spec.Members == nil {
			return nil
		}
		return append([]string{}, nodeGroup.Spec.Members...)
	}); err != nil {
		return err
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&k8sv1alpha1.NodeGroup{}).
		Watches(&source.Kind{Type: &corev1.Node{}},
			handler.EnqueueRequestsFromMapFunc(r.findNodeGroupsForMember),
			builder.WithPredicates(predicate.Or(predicate.LabelChangedPredicate{}, predicate.GenerationChangedPredicate{}))).
		Complete(r)
}
