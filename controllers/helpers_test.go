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
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Unit tests for the pure helper functions in nodegroup_controller.go.
// These do not require a running Kubernetes API server.

func TestStringIn(t *testing.T) {
	tests := []struct {
		name     string
		s        string
		a        []string
		expected bool
	}{
		{"found at start", "a", []string{"a", "b", "c"}, true},
		{"found in middle", "b", []string{"a", "b", "c"}, true},
		{"found at end", "c", []string{"a", "b", "c"}, true},
		{"not found", "d", []string{"a", "b", "c"}, false},
		{"empty slice", "a", []string{}, false},
		{"empty string found", "", []string{"", "a"}, true},
		{"empty string not found", "", []string{"a", "b"}, false},
		{"single element match", "only", []string{"only"}, true},
		{"single element no match", "x", []string{"only"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stringIn(tt.s, tt.a); got != tt.expected {
				t.Errorf("stringIn(%q, %v) = %v, want %v", tt.s, tt.a, got, tt.expected)
			}
		})
	}
}

func TestTaintPos(t *testing.T) {
	taint1 := corev1.Taint{Key: "key1", Value: "val1", Effect: corev1.TaintEffectNoSchedule}
	taint2 := corev1.Taint{Key: "key2", Value: "val2", Effect: corev1.TaintEffectNoExecute}
	taint3 := corev1.Taint{Key: "key3", Value: "val3", Effect: corev1.TaintEffectPreferNoSchedule}

	tests := []struct {
		name     string
		taint    *corev1.Taint
		taints   []corev1.Taint
		expected int
	}{
		{"found at index 0", &taint1, []corev1.Taint{taint1, taint2}, 0},
		{"found at index 1", &taint2, []corev1.Taint{taint1, taint2}, 1},
		{"found at index 2", &taint3, []corev1.Taint{taint1, taint2, taint3}, 2},
		{"not found", &taint3, []corev1.Taint{taint1, taint2}, -1},
		{"empty taints list", &taint1, []corev1.Taint{}, -1},
		{"different effect, no match", &corev1.Taint{Key: "key1", Value: "val1", Effect: corev1.TaintEffectNoExecute}, []corev1.Taint{taint1}, -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := taintPos(tt.taint, tt.taints); got != tt.expected {
				t.Errorf("taintPos() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestHasTaint(t *testing.T) {
	present := corev1.Taint{Key: "k", Value: "v", Effect: corev1.TaintEffectNoSchedule}
	absent := corev1.Taint{Key: "other", Value: "v", Effect: corev1.TaintEffectNoSchedule}

	nodeWith := &corev1.Node{
		Spec: corev1.NodeSpec{Taints: []corev1.Taint{present}},
	}
	nodeWithout := &corev1.Node{}

	if !hasTaint(&present, nodeWith) {
		t.Error("hasTaint returned false for a node that has the taint")
	}
	if hasTaint(&absent, nodeWith) {
		t.Error("hasTaint returned true for a taint the node does not have")
	}
	if hasTaint(&present, nodeWithout) {
		t.Error("hasTaint returned true for a node with no taints")
	}
}

func TestFinalizeNodeLabels(t *testing.T) {
	tests := []struct {
		name           string
		nodeLabels     map[string]string
		specLabels     map[string]string
		expectedUpdate bool
		expectedLabels map[string]string
	}{
		{
			name:           "removes label when value matches spec",
			nodeLabels:     map[string]string{"managed": "yes", "other": "kept"},
			specLabels:     map[string]string{"managed": "yes"},
			expectedUpdate: true,
			expectedLabels: map[string]string{"other": "kept"},
		},
		{
			name:           "does not remove label when value has been changed since NodeGroup applied it",
			nodeLabels:     map[string]string{"managed": "modified"},
			specLabels:     map[string]string{"managed": "yes"},
			expectedUpdate: false,
			expectedLabels: map[string]string{"managed": "modified"},
		},
		{
			name:           "does not remove label that is absent from node",
			nodeLabels:     map[string]string{"other": "value"},
			specLabels:     map[string]string{"managed": "yes"},
			expectedUpdate: false,
			expectedLabels: map[string]string{"other": "value"},
		},
		{
			name:           "removes multiple matching labels",
			nodeLabels:     map[string]string{"a": "1", "b": "2", "c": "3"},
			specLabels:     map[string]string{"a": "1", "b": "2"},
			expectedUpdate: true,
			expectedLabels: map[string]string{"c": "3"},
		},
		{
			name:           "handles nil node labels gracefully",
			nodeLabels:     nil,
			specLabels:     map[string]string{"managed": "yes"},
			expectedUpdate: false,
			expectedLabels: nil,
		},
		{
			name:           "handles empty spec labels",
			nodeLabels:     map[string]string{"key": "value"},
			specLabels:     map[string]string{},
			expectedUpdate: false,
			expectedLabels: map[string]string{"key": "value"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Labels: tt.nodeLabels},
			}
			got := finalizeNodeLabels(node, tt.specLabels)
			if got != tt.expectedUpdate {
				t.Errorf("finalizeNodeLabels() needsUpdate = %v, want %v", got, tt.expectedUpdate)
			}
			if len(node.Labels) != len(tt.expectedLabels) {
				t.Errorf("after finalize: labels = %v, want %v", node.Labels, tt.expectedLabels)
				return
			}
			for k, v := range tt.expectedLabels {
				if node.Labels[k] != v {
					t.Errorf("after finalize: label[%s] = %q, want %q", k, node.Labels[k], v)
				}
			}
		})
	}
}

func TestReconcileNodeLabels(t *testing.T) {
	tests := []struct {
		name           string
		nodeLabels     map[string]string
		specLabels     map[string]string
		expectedUpdate bool
		expectedLabels map[string]string
	}{
		{
			name:           "adds new label to existing label map",
			nodeLabels:     map[string]string{"existing": "yes"},
			specLabels:     map[string]string{"new": "label"},
			expectedUpdate: true,
			expectedLabels: map[string]string{"existing": "yes", "new": "label"},
		},
		{
			name:           "adds label when node has nil labels map",
			nodeLabels:     nil,
			specLabels:     map[string]string{"key": "value"},
			expectedUpdate: true,
			expectedLabels: map[string]string{"key": "value"},
		},
		{
			name:           "updates label with wrong value",
			nodeLabels:     map[string]string{"key": "oldvalue"},
			specLabels:     map[string]string{"key": "newvalue"},
			expectedUpdate: true,
			expectedLabels: map[string]string{"key": "newvalue"},
		},
		{
			name:           "no update when label already has correct value",
			nodeLabels:     map[string]string{"key": "value"},
			specLabels:     map[string]string{"key": "value"},
			expectedUpdate: false,
			expectedLabels: map[string]string{"key": "value"},
		},
		{
			name:           "no update when spec labels is empty",
			nodeLabels:     map[string]string{"key": "value"},
			specLabels:     map[string]string{},
			expectedUpdate: false,
			expectedLabels: map[string]string{"key": "value"},
		},
		{
			name:           "adds multiple labels",
			nodeLabels:     map[string]string{},
			specLabels:     map[string]string{"a": "1", "b": "2"},
			expectedUpdate: true,
			expectedLabels: map[string]string{"a": "1", "b": "2"},
		},
		{
			name:           "does not remove labels not in spec",
			nodeLabels:     map[string]string{"a": "1", "b": "2"},
			specLabels:     map[string]string{"a": "1"},
			expectedUpdate: false,
			expectedLabels: map[string]string{"a": "1", "b": "2"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "test-node", Labels: tt.nodeLabels},
			}
			got := reconcileNodeLabels(node, tt.specLabels)
			if got != tt.expectedUpdate {
				t.Errorf("reconcileNodeLabels() needsUpdate = %v, want %v", got, tt.expectedUpdate)
			}
			for k, v := range tt.expectedLabels {
				if node.Labels[k] != v {
					t.Errorf("label[%s] = %q, want %q", k, node.Labels[k], v)
				}
			}
		})
	}
}

func TestFinalizeNodeTaints(t *testing.T) {
	taint1 := corev1.Taint{Key: "key1", Value: "v1", Effect: corev1.TaintEffectNoSchedule}
	taint2 := corev1.Taint{Key: "key2", Value: "v2", Effect: corev1.TaintEffectNoExecute}
	taint3 := corev1.Taint{Key: "key3", Value: "v3", Effect: corev1.TaintEffectPreferNoSchedule}

	tests := []struct {
		name           string
		nodeTaints     []corev1.Taint
		specTaints     []corev1.Taint
		expectedUpdate bool
		remainingLen   int
	}{
		{
			name:           "removes single matching taint",
			nodeTaints:     []corev1.Taint{taint1, taint2},
			specTaints:     []corev1.Taint{taint1},
			expectedUpdate: true,
			remainingLen:   1,
		},
		{
			name:           "removes all matching taints",
			nodeTaints:     []corev1.Taint{taint1, taint2},
			specTaints:     []corev1.Taint{taint1, taint2},
			expectedUpdate: true,
			remainingLen:   0,
		},
		{
			name:           "does not remove taint not in spec",
			nodeTaints:     []corev1.Taint{taint1, taint2},
			specTaints:     []corev1.Taint{taint3},
			expectedUpdate: false,
			remainingLen:   2,
		},
		{
			name:           "no update when node has no taints",
			nodeTaints:     []corev1.Taint{},
			specTaints:     []corev1.Taint{taint1},
			expectedUpdate: false,
			remainingLen:   0,
		},
		{
			name:           "no update with empty spec taints",
			nodeTaints:     []corev1.Taint{taint1},
			specTaints:     []corev1.Taint{},
			expectedUpdate: false,
			remainingLen:   1,
		},
		{
			name:           "preserves non-spec taints when removing spec taint",
			nodeTaints:     []corev1.Taint{taint1, taint2, taint3},
			specTaints:     []corev1.Taint{taint2},
			expectedUpdate: true,
			remainingLen:   2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := &corev1.Node{
				Spec: corev1.NodeSpec{Taints: append([]corev1.Taint{}, tt.nodeTaints...)},
			}
			got := finalizeNodeTaints(node, tt.specTaints)
			if got != tt.expectedUpdate {
				t.Errorf("finalizeNodeTaints() needsUpdate = %v, want %v", got, tt.expectedUpdate)
			}
			if len(node.Spec.Taints) != tt.remainingLen {
				t.Errorf("remaining taints = %d, want %d (taints: %v)", len(node.Spec.Taints), tt.remainingLen, node.Spec.Taints)
			}
		})
	}
}

func TestReconcileNodeTaints(t *testing.T) {
	taint1 := corev1.Taint{Key: "key1", Value: "v1", Effect: corev1.TaintEffectNoSchedule}
	taint2 := corev1.Taint{Key: "key2", Value: "v2", Effect: corev1.TaintEffectNoExecute}

	tests := []struct {
		name           string
		nodeTaints     []corev1.Taint
		specTaints     []corev1.Taint
		expectedUpdate bool
		expectedLen    int
	}{
		{
			name:           "adds a missing taint",
			nodeTaints:     []corev1.Taint{},
			specTaints:     []corev1.Taint{taint1},
			expectedUpdate: true,
			expectedLen:    1,
		},
		{
			name:           "adds multiple missing taints",
			nodeTaints:     []corev1.Taint{},
			specTaints:     []corev1.Taint{taint1, taint2},
			expectedUpdate: true,
			expectedLen:    2,
		},
		{
			name:           "no update when taint already present",
			nodeTaints:     []corev1.Taint{taint1},
			specTaints:     []corev1.Taint{taint1},
			expectedUpdate: false,
			expectedLen:    1,
		},
		{
			name:           "adds only the missing taint when one is already present",
			nodeTaints:     []corev1.Taint{taint1},
			specTaints:     []corev1.Taint{taint1, taint2},
			expectedUpdate: true,
			expectedLen:    2,
		},
		{
			name:           "no update with empty spec taints",
			nodeTaints:     []corev1.Taint{taint1},
			specTaints:     []corev1.Taint{},
			expectedUpdate: false,
			expectedLen:    1,
		},
		{
			name:           "no update when node has no taints and spec is empty",
			nodeTaints:     []corev1.Taint{},
			specTaints:     []corev1.Taint{},
			expectedUpdate: false,
			expectedLen:    0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "test-node"},
				Spec:       corev1.NodeSpec{Taints: append([]corev1.Taint{}, tt.nodeTaints...)},
			}
			got := reconcileNodeTaints(node, tt.specTaints)
			if got != tt.expectedUpdate {
				t.Errorf("reconcileNodeTaints() needsUpdate = %v, want %v", got, tt.expectedUpdate)
			}
			if len(node.Spec.Taints) != tt.expectedLen {
				t.Errorf("taint count = %d, want %d (taints: %v)", len(node.Spec.Taints), tt.expectedLen, node.Spec.Taints)
			}
		})
	}
}
