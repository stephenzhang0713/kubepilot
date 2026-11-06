package kube

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/stephenzhang0713/kubepilot/internal/investigation"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Finding struct {
	Resource string `json:"resource"`
	Detail   string `json:"detail"`
}
type Section struct {
	Name      string    `json:"name"`
	State     string    `json:"state"`
	Checked   int       `json:"checked"`
	Findings  []Finding `json:"findings"`
	Error     string    `json:"error,omitempty"`
	Truncated bool      `json:"truncated,omitempty"`
}
type Overview struct {
	CollectedAt time.Time `json:"collectedAt"`
	Sections    []Section `json:"sections"`
}

func section(name string) Section { return Section{Name: name, State: "ok", Findings: []Finding{}} }
func (s *Section) fail(err error) { s.State = "unknown"; s.Error = err.Error() }
func (s *Section) issue(resource, detail string) {
	s.State = "attention"
	if len(s.Findings) < 30 {
		s.Findings = append(s.Findings, Finding{resource, detail})
	}
}
func (s *Section) limit(continuation string) {
	if continuation != "" {
		s.Truncated = true
		if s.State == "ok" {
			s.State = "unknown"
		}
	}
}
func (c *Collector) Overview(ctx context.Context) Overview {
	out := Overview{CollectedAt: time.Now().UTC(), Sections: []Section{}}
	opts := metav1.ListOptions{Limit: 100}
	nodes := section("nodes")
	nodeList, err := c.Client.CoreV1().Nodes().List(ctx, opts)
	if err != nil {
		nodes.fail(err)
	} else {
		nodes.Checked = len(nodeList.Items)
		if nodes.Checked == 0 {
			nodes.State = "unknown"
			nodes.Error = "no nodes returned"
		}
		nodes.limit(nodeList.Continue)
		for _, n := range nodeList.Items {
			ready := false
			for _, cond := range n.Status.Conditions {
				if cond.Type == corev1.NodeReady {
					ready = cond.Status == corev1.ConditionTrue
				}
				if (cond.Type == corev1.NodeMemoryPressure || cond.Type == corev1.NodeDiskPressure || cond.Type == corev1.NodePIDPressure) && cond.Status == corev1.ConditionTrue {
					nodes.issue(n.Name, string(cond.Type)+": "+cond.Message)
				}
			}
			if !ready {
				nodes.issue(n.Name, "Node Ready condition is not True")
			}
		}
	}
	out.Sections = append(out.Sections, nodes)
	system := section("system pods")
	pods, err := c.Client.CoreV1().Pods("kube-system").List(ctx, opts)
	if err != nil {
		system.fail(err)
	} else {
		system.Checked = len(pods.Items)
		if system.Checked == 0 {
			system.State = "unknown"
			system.Error = "no kube-system pods returned"
		}
		system.limit(pods.Continue)
		for _, p := range pods.Items {
			if p.Status.Phase == corev1.PodSucceeded {
				continue
			}
			ready := false
			for _, cond := range p.Status.Conditions {
				if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
					ready = true
				}
			}
			if !ready {
				system.issue(p.Namespace+"/"+p.Name, "Pod not ready ("+string(p.Status.Phase)+")")
			}
		}
	}
	out.Sections = append(out.Sections, system)
	deployments := section("deployments")
	deps, err := c.Client.AppsV1().Deployments("").List(ctx, opts)
	if err != nil {
		deployments.fail(err)
	} else {
		deployments.Checked = len(deps.Items)
		deployments.limit(deps.Continue)
		for _, d := range deps.Items {
			desired := int32(1)
			if d.Spec.Replicas != nil {
				desired = *d.Spec.Replicas
			}
			if d.Status.AvailableReplicas < desired {
				deployments.issue(d.Namespace+"/"+d.Name, fmt.Sprintf("%d/%d replicas available", d.Status.AvailableReplicas, desired))
			}
		}
	}
	out.Sections = append(out.Sections, deployments)
	daemons := section("daemonsets")
	ds, err := c.Client.AppsV1().DaemonSets("").List(ctx, opts)
	if err != nil {
		daemons.fail(err)
	} else {
		daemons.Checked = len(ds.Items)
		daemons.limit(ds.Continue)
		for _, d := range ds.Items {
			if d.Status.NumberReady < d.Status.DesiredNumberScheduled {
				daemons.issue(d.Namespace+"/"+d.Name, fmt.Sprintf("%d/%d ready", d.Status.NumberReady, d.Status.DesiredNumberScheduled))
			}
		}
	}
	out.Sections = append(out.Sections, daemons)
	stateful := section("statefulsets")
	sts, err := c.Client.AppsV1().StatefulSets("").List(ctx, opts)
	if err != nil {
		stateful.fail(err)
	} else {
		stateful.Checked = len(sts.Items)
		stateful.limit(sts.Continue)
		for _, s := range sts.Items {
			desired := int32(1)
			if s.Spec.Replicas != nil {
				desired = *s.Spec.Replicas
			}
			if s.Status.ReadyReplicas < desired {
				stateful.issue(s.Namespace+"/"+s.Name, fmt.Sprintf("%d/%d ready", s.Status.ReadyReplicas, desired))
			}
		}
	}
	out.Sections = append(out.Sections, stateful)
	events := section("warning events")
	ev, err := c.Client.CoreV1().Events("").List(ctx, metav1.ListOptions{Limit: 100, FieldSelector: "type=Warning"})
	if err != nil {
		events.fail(err)
	} else {
		events.Checked = len(ev.Items)
		events.limit(ev.Continue)
		for _, e := range ev.Items {
			events.issue(e.Namespace+"/"+e.InvolvedObject.Name, e.Reason+": "+e.Message)
		}
	}
	out.Sections = append(out.Sections, events)
	return out
}
func (m *Manager) Overview(ctx context.Context) (Overview, error) {
	m.mu.RLock()
	collector := m.collector
	m.mu.RUnlock()
	if collector == nil {
		return Overview{}, errors.New("no Kubernetes configuration; configure it in the web console")
	}
	return collector.Overview(ctx), nil
}
func (m *Manager) overview(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	result, err := m.Overview(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.MarshalWrite(w, result)
}

func (m *Manager) CollectCluster(ctx context.Context) (investigation.Evidence, error) {
	overview, err := m.Overview(ctx)
	if err != nil {
		return investigation.Evidence{}, err
	}
	data, err := json.Marshal(overview)
	if err != nil {
		return investigation.Evidence{}, err
	}
	evidence := investigation.Evidence{Cluster: string(data)}
	success := 0
	for _, s := range overview.Sections {
		if s.Error != "" {
			evidence.Warnings = append(evidence.Warnings, s.Name+": "+s.Error)
		} else {
			success++
		}
		if s.Truncated {
			evidence.Warnings = append(evidence.Warnings, s.Name+": only the first 100 resources were inspected")
		}
	}
	if success == 0 {
		return evidence, errors.New("no cluster resources could be inspected; check connectivity and permissions")
	}
	return evidence, nil
}
