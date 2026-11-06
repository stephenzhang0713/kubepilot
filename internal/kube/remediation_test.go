package kube

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

func TestRestartBoundToClusterAndObjectVersion(t *testing.T) {
	client := fake.NewSimpleClientset(&appsv1.Deployment{Name: "api", Namespace: "default", UID: types.UID("uid-1"), ResourceVersion: "10"})
	m := &Manager{collector: &Collector{Client: client}, context: "test", revision: 3}
	snapshot, err := m.PrepareRestart(t.Context(), 3, "default", "api")
	if err != nil {
		t.Fatal(err)
	}
	if len(client.Actions()) != 1 {
		t.Fatal("preparing mutated cluster")
	}
	if _, err := m.ExecuteRestart(t.Context(), snapshot); err != nil {
		t.Fatal(err)
	}
	d, err := client.AppsV1().Deployments("default").Get(t.Context(), "api", metav1.GetOptions{})
	if err != nil || d.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] == "" {
		t.Fatalf("missing rollout marker: %+v %v", d, err)
	}
	m.revision++
	if _, err := m.ExecuteRestart(t.Context(), snapshot); err == nil {
		t.Fatal("cluster switch was not rejected")
	}
	m.revision--
	snapshot.ResourceVersion = "old"
	if _, err := m.ExecuteRestart(t.Context(), snapshot); err == nil {
		t.Fatal("stale version was not rejected")
	}
	if _, err := m.PrepareRestart(t.Context(), 3, "kube-system", "api"); err == nil {
		t.Fatal("system namespace allowed")
	}
}

func TestVerifyRestartRequiresFreshMatchingRollout(t *testing.T) {
	replicas := int32(2)
	d := &appsv1.Deployment{Name: "api", Namespace: "default", UID: types.UID("uid-1"), ResourceVersion: "12", Generation: 4, Spec: appsv1.DeploymentSpec{Replicas: &replicas}}
	d.Spec.Template.Annotations = map[string]string{"kubepilot.io/restart-token": "plan-1"}
	d.Status = appsv1.DeploymentStatus{ObservedGeneration: 4, Replicas: 2, UpdatedReplicas: 2, ReadyReplicas: 2, AvailableReplicas: 2}
	client := fake.NewSimpleClientset(d)
	m := &Manager{collector: &Collector{Client: client}, revision: 3}
	s := DeploymentSnapshot{Namespace: "default", Name: "api", UID: "uid-1", Replicas: 2, Revision: 3, RestartToken: "plan-1", RestartGeneration: 4}
	evidence, err := m.VerifyRestart(t.Context(), s)
	if err != nil || !evidence.Verified || evidence.ObservedAt.IsZero() {
		t.Fatalf("healthy rollout: %+v %v", evidence, err)
	}
	d.Status.UpdatedReplicas = 1
	if _, err := client.AppsV1().Deployments("default").Update(t.Context(), d, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	evidence, err = m.VerifyRestart(t.Context(), s)
	if err != nil || evidence.Verified {
		t.Fatalf("incomplete rollout: %+v %v", evidence, err)
	}
	s.RestartToken = "different"
	if _, err := m.VerifyRestart(t.Context(), s); err == nil {
		t.Fatal("accepted another restart")
	}
	s.RestartToken = "plan-1"
	s.RestartGeneration = 3
	evidence, err = m.VerifyRestart(t.Context(), s)
	if err != nil || evidence.Verified {
		t.Fatal("accepted a changed generation")
	}
	s.UID = "other"
	if _, err := m.VerifyRestart(t.Context(), s); err == nil {
		t.Fatal("accepted a different Deployment")
	}
}
