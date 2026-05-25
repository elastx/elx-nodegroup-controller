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

package v1alpha2

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type NodeGroupSpec struct {
	//+optional
	//+kubebuilder:validation:MaxItems=500
	Members []string `json:"members,omitempty"`
	//+optional
	//+kubebuilder:validation:MaxItems=50
	NodeGroupNames []string `json:"nodeGroupNames,omitempty"`
	//+optional
	//+kubebuilder:validation:MaxProperties=64
	//+kubebuilder:validation:XValidation:rule="self.all(k, !k.startsWith('kubernetes.io/') && !k.startsWith('k8s.io/') && !k.startsWith('node.kubernetes.io/'))",message="label keys must not use reserved kubernetes.io/, k8s.io/, or node.kubernetes.io/ prefixes"
	Labels map[string]string `json:"labels,omitempty"`
	//+optional
	//+kubebuilder:validation:MaxItems=100
	//+kubebuilder:validation:XValidation:rule="self.all(t, t.effect in ['NoSchedule', 'PreferNoSchedule', 'NoExecute'])",message="taint effect must be one of: NoSchedule, PreferNoSchedule, NoExecute"
	Taints []corev1.Taint `json:"taints,omitempty"`
}

type NodeGroupStatus struct {
}

//+kubebuilder:object:root=true
//+kubebuilder:storageversion
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster

// NodeGroup is the Schema for the nodegroups API
type NodeGroup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NodeGroupSpec   `json:"spec,omitempty"`
	Status NodeGroupStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// NodeGroupList contains a list of NodeGroup
type NodeGroupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NodeGroup `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NodeGroup{}, &NodeGroupList{})
}
