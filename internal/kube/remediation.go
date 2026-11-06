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
	Context           string `json:"context"`
	Namespace         string `json:"namespace"`
	Name              string `json:"name"`
	UID               string `json:"uid"`
	ResourceVersion   string `json:"resourceVersion"`
	Replicas          int32  `json:"replicas"`
	Revision          uint64 `json:"clusterRevision"`
	RestartToken      string `json:"restartToken,omitempty"`
	RestartGeneration int64  `json:"restartGeneration,omitempty"`
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
func (m *Manager) ExecuteRestart(ctx context.Context, s DeploymentSnapshot) (int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.collector == nil || m.revision != s.Revision {
		return 0, errors.New("cluster connection changed; create a new plan")
	}
	api := m.collector.Client.AppsV1().Deployments(s.Namespace)
	d, err := api.Get(ctx, s.Name, metav1.GetOptions{})
	if err != nil {
		return 0, fmt.Errorf("read Deployment: %w", err)
	}
	if string(d.UID) != s.UID || d.ResourceVersion != s.ResourceVersion {
		return 0, errors.New("Deployment changed since review; create a new plan")
	}
	if d.Spec.Template.Annotations == nil {
		d.Spec.Template.Annotations = map[string]string{}
	}
	d.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = time.Now().UTC().Format(time.RFC3339Nano)
	d.Spec.Template.Annotations["kubepilot.io/restart-token"] = s.RestartToken
	updated, err := api.Update(ctx, d, metav1.UpdateOptions{})
	if err != nil {
		return 0, fmt.Errorf("update Deployment: %w", err)
	}
	return updated.Generation, nil
}

// RolloutEvidence is a fresh observation of the Deployment after a restart.
type RolloutEvidence struct {
	ObservedAt         time.Time `json:"observedAt"`
	ResourceVersion    string    `json:"resourceVersion"`
	Generation         int64     `json:"generation"`
	ObservedGeneration int64     `json:"observedGeneration"`
	Desired            int32     `json:"desired"`
	Updated            int32     `json:"updated"`
	Ready              int32     `json:"ready"`
	Available          int32     `json:"available"`
	Total              int32     `json:"total"`
	Verified           bool      `json:"verified"`
}

func (m *Manager) VerifyRestart(ctx context.Context, s DeploymentSnapshot) (RolloutEvidence, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.collector == nil || m.revision != s.Revision {
		return RolloutEvidence{}, errors.New("cluster connection changed; cannot verify this plan")
	}
	d, err := m.collector.Client.AppsV1().Deployments(s.Namespace).Get(ctx, s.Name, metav1.GetOptions{})
	if err != nil {
		return RolloutEvidence{}, fmt.Errorf("read Deployment: %w", err)
	}
	if string(d.UID) != s.UID {
		return RolloutEvidence{}, errors.New("Deployment identity changed; cannot verify this plan")
	}
	if s.RestartToken == "" || d.Spec.Template.Annotations["kubepilot.io/restart-token"] != s.RestartToken {
		return RolloutEvidence{}, errors.New("restart marker is missing or changed; cannot verify this plan")
	}
	desired := int32(1)
	if d.Spec.Replicas != nil {
		desired = *d.Spec.Replicas
	}
	e := RolloutEvidence{ObservedAt: time.Now().UTC(), ResourceVersion: d.ResourceVersion, Generation: d.Generation, ObservedGeneration: d.Status.ObservedGeneration, Desired: desired, Updated: d.Status.UpdatedReplicas, Ready: d.Status.ReadyReplicas, Available: d.Status.AvailableReplicas, Total: d.Status.Replicas}
	e.Verified = desired > 0 && desired == s.Replicas && s.RestartGeneration > 0 && d.Generation == s.RestartGeneration && e.ObservedGeneration >= e.Generation && e.Updated == desired && e.Ready == desired && e.Available == desired && e.Total == desired
	return e, nil
}
