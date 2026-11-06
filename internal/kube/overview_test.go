package kube

import (
	"context"
	"errors"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

func TestOverviewSurfacesProblemsAndPermissionGaps(t *testing.T) {
	client := fake.NewSimpleClientset(
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-a"}, Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionFalse}}}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "coredns", Namespace: "kube-system"}, Status: corev1.PodStatus{Phase: corev1.PodPending}},
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default"}, Spec: appsv1.DeploymentSpec{Replicas: new(int32(2))}, Status: appsv1.DeploymentStatus{AvailableReplicas: 1}},
	)
	client.PrependReactor("list", "daemonsets", func(clienttesting.Action) (bool, runtime.Object, error) { return true, nil, errors.New("forbidden") })
	snapshot := (&Collector{Client: client}).Overview(context.Background())
	byName := map[string]Section{}
	for _, s := range snapshot.Sections {
		byName[s.Name] = s
	}
	for _, name := range []string{"nodes", "system pods", "deployments"} {
		if byName[name].State != "attention" || len(byName[name].Findings) == 0 {
			t.Errorf("%s: %+v", name, byName[name])
		}
	}
	if got := byName["daemonsets"]; got.State != "unknown" || !strings.Contains(got.Error, "forbidden") {
		t.Fatalf("permission gap: %+v", got)
	}
	if byName["statefulsets"].State != "ok" {
		t.Fatalf("statefulsets: %+v", byName["statefulsets"])
	}
}
func TestTruncatedSectionNotMarkedHealthy(t *testing.T) {
	s := section("nodes")
	s.limit("next")
	if s.State != "unknown" || !s.Truncated {
		t.Fatalf("unexpected state: %+v", s)
	}
}
