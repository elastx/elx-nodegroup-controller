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
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	k8sv1alpha2 "github.com/elastx/elx-nodegroup-controller/api/v1alpha2"
)

const (
	membersField           = ".spec.members"
	finalizer              = "k8s.elx.cloud/finalizer"
	startupTaintAnnotation = "k8s.elx.cloud/startup-taints-applied"
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
	for l := range labels {
		if _, ok := currentLabels[l]; ok {
			delete(node.Labels, l)
			needsUpdate = true
		}
	}
	return needsUpdate
}

func reconcileNodeLabels(ctx context.Context, node *corev1.Node, labels map[string]string) bool {
	log := log.FromContext(ctx)
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

func reconcileNodeTaints(ctx context.Context, node *corev1.Node, taints []corev1.Taint) bool {
	log := log.FromContext(ctx)
	needsUpdate := false
	for _, t := range taints {
		if !hasTaint(&t, node) {
			log.V(1).Info("adding taint", "node", node.Name, "taint", t)
			node.Spec.Taints = append(node.Spec.Taints, t)
			needsUpdate = true
		}
	}
	return needsUpdate
}

func startupTaintKey(ngName string, taint corev1.Taint) string {
	return ngName + "/" + taint.Key + ":" + string(taint.Effect)
}

func getAppliedStartupTaints(node *corev1.Node) map[string]struct{} {
	result := make(map[string]struct{})
	annotations := node.GetAnnotations()
	if annotations == nil {
		return result
	}
	val, ok := annotations[startupTaintAnnotation]
	if !ok || val == "" {
		return result
	}
	for _, entry := range strings.Split(val, ",") {
		result[entry] = struct{}{}
	}
	return result
}

func setAppliedStartupTaints(node *corev1.Node, applied map[string]struct{}) {
	annotations := node.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	if len(applied) == 0 {
		delete(annotations, startupTaintAnnotation)
	} else {
		keys := make([]string, 0, len(applied))
		for k := range applied {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		annotations[startupTaintAnnotation] = strings.Join(keys, ",")
	}
	node.SetAnnotations(annotations)
}

func reconcileStartupTaints(ctx context.Context, node *corev1.Node, ngName string, taints []corev1.Taint) bool {
	log := log.FromContext(ctx)
	needsUpdate := false
	applied := getAppliedStartupTaints(node)
	for _, t := range taints {
		key := startupTaintKey(ngName, t)
		if _, alreadyApplied := applied[key]; alreadyApplied {
			continue
		}
		if !hasTaint(&t, node) {
			log.V(1).Info("adding startup taint", "node", node.Name, "taint", t)
			node.Spec.Taints = append(node.Spec.Taints, t)
		}
		applied[key] = struct{}{}
		needsUpdate = true
	}
	if needsUpdate {
		setAppliedStartupTaints(node, applied)
	}
	return needsUpdate
}

func finalizeStartupTaints(node *corev1.Node, ngName string, taints []corev1.Taint) bool {
	needsUpdate := false
	for _, t := range taints {
		if pos := taintPos(&t, node.Spec.Taints); pos > -1 {
			node.Spec.Taints[pos] = node.Spec.Taints[len(node.Spec.Taints)-1]
			node.Spec.Taints = node.Spec.Taints[:len(node.Spec.Taints)-1]
			needsUpdate = true
		}
	}
	applied := getAppliedStartupTaints(node)
	for _, t := range taints {
		key := startupTaintKey(ngName, t)
		if _, ok := applied[key]; ok {
			delete(applied, key)
			needsUpdate = true
		}
	}
	if needsUpdate {
		setAppliedStartupTaints(node, applied)
	}
	return needsUpdate
}

func (r *NodeGroupReconciler) findNodeGroupsForMember(ctx context.Context, node client.Object) []reconcile.Request {
	log := log.FromContext(ctx)
	nodeGroups := &k8sv1alpha2.NodeGroupList{}
	listOpts := &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(membersField, node.GetName()),
	}
	if err := r.List(ctx, nodeGroups, listOpts); err != nil {
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
	log.V(1).Info("reconcile requests returned", "requests", requests)
	return requests
}

func (r *NodeGroupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	log.V(1).Info("reconciling node group", "nodeGroup", req.NamespacedName)
	var nodeGroup k8sv1alpha2.NodeGroup
	if err := r.Get(ctx, req.NamespacedName, &nodeGroup); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		log.Error(err, "unable to fetch nodeGroup", "nodeGroup", req.NamespacedName)

		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	seen := make(map[string]struct{}, len(nodeGroup.Spec.Members))
	for _, m := range nodeGroup.Spec.Members {
		seen[m] = struct{}{}
	}
	matchingStrings := append([]string{}, nodeGroup.Spec.Members...)

	nodes := &corev1.NodeList{}
	if err := r.List(ctx, nodes); err != nil {
		log.Error(err, "unable to list Nodes")
		return ctrl.Result{}, err
	}

	for _, node := range nodes.Items {
		nodeNameParts := strings.Split(node.Name, "-")
		for _, part := range nodeNameParts {
			if stringIn(part, nodeGroup.Spec.NodeGroupNames) {
				if _, exists := seen[node.Name]; !exists {
					seen[node.Name] = struct{}{}
					matchingStrings = append(matchingStrings, node.Name)
				}
				break
			}
		}
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
			for _, n := range matchingStrings {
				err := r.Get(ctx, types.NamespacedName{Name: n}, node)
				if apierrors.IsNotFound(err) {
					continue
				}
				if err != nil {
					return ctrl.Result{}, err
				}
				if needsUpdate := finalizeNodeLabels(node, nodeGroup.Spec.Labels); needsUpdate {
					if err := r.Update(ctx, node); err != nil {
						log.Error(err, "unable to update Node", "node", n)
						return ctrl.Result{}, err
					}
					return ctrl.Result{Requeue: true}, nil
				}
				if needsUpdate := finalizeNodeTaints(node, nodeGroup.Spec.Taints); needsUpdate {
					if err := r.Update(ctx, node); err != nil {
						log.Error(err, "unable to update Node", "node", n)
						return ctrl.Result{}, err
					}
					return ctrl.Result{Requeue: true}, nil
				}
				if needsUpdate := finalizeStartupTaints(node, nodeGroup.Name, nodeGroup.Spec.StartupTaints); needsUpdate {
					if err := r.Update(ctx, node); err != nil {
						log.Error(err, "unable to update Node", "node", n)
						return ctrl.Result{}, err
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

	for _, n := range matchingStrings {
		err := r.Get(ctx, types.NamespacedName{Name: n}, node)
		if apierrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return ctrl.Result{}, err
		}
		log.V(1).Info("reconciling node", "node", n)
		if needsUpdate := reconcileNodeLabels(ctx, node, nodeGroup.Spec.Labels); needsUpdate {
			if err := r.Update(ctx, node); err != nil {
				log.Error(err, "unable to update Node", "node", n)
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}
		if needsUpdate := reconcileNodeTaints(ctx, node, nodeGroup.Spec.Taints); needsUpdate {
			if err := r.Update(ctx, node); err != nil {
				log.Error(err, "unable to update Node", "node", n)
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}
		if needsUpdate := reconcileStartupTaints(ctx, node, nodeGroup.Name, nodeGroup.Spec.StartupTaints); needsUpdate {
			if err := r.Update(ctx, node); err != nil {
				log.Error(err, "unable to update Node", "node", n)
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}
	}

	return ctrl.Result{}, nil
}

func (r *NodeGroupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &k8sv1alpha2.NodeGroup{}, membersField, func(rawObj client.Object) []string {
		nodeGroup := rawObj.(*k8sv1alpha2.NodeGroup)
		if nodeGroup.Spec.Members == nil {
			return nil
		}
		return append([]string{}, nodeGroup.Spec.Members...)
	}); err != nil {
		return err
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&k8sv1alpha2.NodeGroup{}).
		Watches(&corev1.Node{},
			handler.EnqueueRequestsFromMapFunc(r.findNodeGroupsForMember)).
		Complete(r)
}
