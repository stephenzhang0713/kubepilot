// Package kube collects bounded, read-only Kubernetes evidence.
package kube

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/stephenzhang0713/kubepilot/internal/investigation"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

type Collector struct{ Client kubernetes.Interface }

func New() (*Collector, error) {
	path := os.Getenv("KUBECONFIG")
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(home, ".kube", "config")
	}
	cfg, err := clientcmd.BuildConfigFromFlags("", path)
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig: %w", err)
	}
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &Collector{Client: client}, nil
}
func (c *Collector) Collect(ctx context.Context, namespace, pod string) (investigation.Evidence, error) {
	var result investigation.Evidence
	p, err := c.Client.CoreV1().Pods(namespace).Get(ctx, pod, metav1.GetOptions{})
	if err != nil {
		return result, fmt.Errorf("get pod: %w", err)
	}
	// Only status and selected metadata are exposed to avoid leaking pod environment variables.
	data, _ := json.MarshalIndent(struct {
		Name   string           `json:"name"`
		Status corev1.PodStatus `json:"status"`
	}{p.Name, p.Status}, "", "  ")
	result.Pod = string(data)
	events, err := c.Client.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{FieldSelector: fields.OneTermEqualSelector("involvedObject.uid", string(p.UID)).String(), Limit: 100})
	if err != nil {
		result.Warnings = append(result.Warnings, "events unavailable: "+err.Error())
	} else {
		data, _ = json.MarshalIndent(events.Items, "", "  ")
		result.Events = string(data)
	}
	var logs []string
	for _, container := range p.Spec.Containers {
		tail := int64(100)
		limit := int64(16384)
		stream, err := c.Client.CoreV1().Pods(namespace).GetLogs(pod, &corev1.PodLogOptions{Container: container.Name, TailLines: &tail, LimitBytes: &limit}).Stream(ctx)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("logs for %s unavailable: %v", container.Name, err))
			continue
		}
		b, readErr := io.ReadAll(io.LimitReader(stream, limit))
		_ = stream.Close()
		if readErr != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("logs for %s: %v", container.Name, readErr))
		}
		logs = append(logs, container.Name+":\n"+string(b))
	}
	result.Logs = strings.Join(logs, "\n\n")
	return result, nil
}
