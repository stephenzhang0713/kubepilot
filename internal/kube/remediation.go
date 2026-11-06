package kube

import (
	"context"
	"errors"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
)

type DeploymentSnapshot struct {
	Context         string `json:"context"`
	Namespace       string `json:"namespace"`
	Name            string `json:"name"`
	UID             string `json:"uid"`
	ResourceVersion string `json:"resourceVersion"`
	Replicas        int32  `json:"replicas"`
	Revision        uint64 `json:"clusterRevision"`
}

func (m *Manager) PrepareRestart(ctx context.Context, revision uint64, namespace, name string) (DeploymentSnapshot, error) {
	if len(validation.IsDNS1123Label(namespace)) > 0 || len(validation.IsDNS1123Subdomain(name)) > 0 {
		return DeploymentSnapshot{}, errors.New("invalid namespace or Deployment name")
	}
	if namespace == "kube-system" || namespace == "kube-public" || namespace == "kube-node-lease" {
		return DeploymentSnapshot{}, errors.New("system namespaces are excluded from restart operations")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.collector == nil || revision == 0 || m.revision != revision {
		return DeploymentSnapshot{}, errors.New("cluster connection changed; investigate again")
	}
	d, err := m.collector.Client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return DeploymentSnapshot{}, fmt.Errorf("read Deployment: %w", err)
	}
	if d.UID == "" || d.ResourceVersion == "" {
		return DeploymentSnapshot{}, errors.New("Deployment identity is unavailable")
	}
	replicas := int32(1)
	if d.Spec.Replicas != nil {
		replicas = *d.Spec.Replicas
	}
	return DeploymentSnapshot{Context: m.context, Namespace: namespace, Name: name, UID: string(d.UID), ResourceVersion: d.ResourceVersion, Replicas: replicas, Revision: revision}, nil
}
func (m *Manager) ExecuteRestart(ctx context.Context, s DeploymentSnapshot) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.collector == nil || m.revision != s.Revision {
		return errors.New("cluster connection changed; create a new plan")
	}
	api := m.collector.Client.AppsV1().Deployments(s.Namespace)
	d, err := api.Get(ctx, s.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("read Deployment: %w", err)
	}
	if string(d.UID) != s.UID || d.ResourceVersion != s.ResourceVersion {
		return errors.New("Deployment changed since review; create a new plan")
	}
	if d.Spec.Template.Annotations == nil {
		d.Spec.Template.Annotations = map[string]string{}
	}
	d.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = time.Now().UTC().Format(time.RFC3339Nano)
	if _, err = api.Update(ctx, d, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("update Deployment: %w", err)
	}
	return nil
}
