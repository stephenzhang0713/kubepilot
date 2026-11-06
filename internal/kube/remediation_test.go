package kube

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

func TestRestartBoundToClusterAndObjectVersion(t *testing.T) {
	client := fake.NewSimpleClientset(&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default", UID: types.UID("uid-1"), ResourceVersion: "10"}})
	m := &Manager{collector: &Collector{Client: client}, context: "test", revision: 3}
	snapshot, err := m.PrepareRestart(t.Context(), 3, "default", "api")
	if err != nil {
		t.Fatal(err)
	}
	if len(client.Actions()) != 1 {
		t.Fatal("preparing mutated cluster")
	}
	if err := m.ExecuteRestart(t.Context(), snapshot); err != nil {
		t.Fatal(err)
	}
	d, err := client.AppsV1().Deployments("default").Get(t.Context(), "api", metav1.GetOptions{})
	if err != nil || d.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] == "" {
		t.Fatalf("missing rollout marker: %+v %v", d, err)
	}
	m.revision++
	if err := m.ExecuteRestart(t.Context(), snapshot); err == nil {
		t.Fatal("cluster switch was not rejected")
	}
	m.revision--
	snapshot.ResourceVersion = "old"
	if err := m.ExecuteRestart(t.Context(), snapshot); err == nil {
		t.Fatal("stale version was not rejected")
	}
	if _, err := m.PrepareRestart(t.Context(), 3, "kube-system", "api"); err == nil {
		t.Fatal("system namespace allowed")
	}
}
