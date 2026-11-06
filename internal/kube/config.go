package kube

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"slices"
	"sync"

	"github.com/stephenzhang0713/kubepilot/internal/investigation"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// Manager holds the active client in memory; an investigation keeps the client
// it started with even when a different configuration is subsequently selected.
type Manager struct {
	mu        sync.RWMutex
	collector *Collector
	context   string
	source    string
	revision  uint64
}
type ConfigStatus struct {
	Configured bool   `json:"configured"`
	Context    string `json:"context,omitempty"`
	Source     string `json:"source,omitempty"`
	Revision   uint64 `json:"revision"`
}

func NewManager() *Manager {
	m := &Manager{}
	_ = m.ConfigureLocal("")
	return m
}
func (m *Manager) Status() ConfigStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return ConfigStatus{Configured: m.collector != nil, Context: m.context, Source: m.source, Revision: m.revision}
}
func (m *Manager) Collect(ctx context.Context, namespace, pod string) (investigation.Evidence, error) {
	m.mu.RLock()
	collector := m.collector
	m.mu.RUnlock()
	if collector == nil {
		return investigation.Evidence{}, errors.New("no Kubernetes configuration; configure it in the web console")
	}
	return collector.Collect(ctx, namespace, pod)
}
func (m *Manager) Configure(data []byte) error {
	config, err := clientcmd.Load(data)
	if err != nil {
		return errors.New("invalid kubeconfig YAML")
	}
	if config.CurrentContext == "" || config.Contexts[config.CurrentContext] == nil {
		return errors.New("kubeconfig must have a valid current-context")
	}
	// Uploaded configurations cannot invoke host programs or read host files.
	for _, auth := range config.AuthInfos {
		if auth == nil {
			return errors.New("invalid kubeconfig user")
		}
		if auth.Exec != nil || auth.AuthProvider != nil || auth.TokenFile != "" || auth.ClientCertificate != "" || auth.ClientKey != "" {
			return errors.New("uploaded kubeconfig must use embedded credentials; exec plugins and local credential files are not supported")
		}
	}
	for _, cluster := range config.Clusters {
		if cluster == nil {
			return errors.New("invalid kubeconfig cluster")
		}
		if cluster.CertificateAuthority != "" {
			return errors.New("uploaded kubeconfig must embed certificate-authority-data")
		}
	}
	cfg, err := clientcmd.NewNonInteractiveClientConfig(*config, config.CurrentContext, &clientcmd.ConfigOverrides{}, nil).ClientConfig()
	if err != nil {
		return errors.New("invalid or incomplete kubeconfig")
	}
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return errors.New("could not create Kubernetes client from kubeconfig")
	}
	m.mu.Lock()
	m.collector = &Collector{Client: client}
	m.context = config.CurrentContext
	m.source = "web"
	m.revision++
	m.mu.Unlock()
	return nil
}
func (m *Manager) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/kubeconfig", m.get)
	mux.HandleFunc("GET /api/cluster/overview", m.overview)
	mux.HandleFunc("PUT /api/kubeconfig", m.put)
	mux.HandleFunc("GET /api/kubeconfig/contexts", m.contexts)
	mux.HandleFunc("PUT /api/kubeconfig/local", m.putLocal)
}
func (m *Manager) get(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.MarshalWrite(w, m.Status())
}
func (m *Manager) put(w http.ResponseWriter, r *http.Request) {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
		return
	}
	var input struct {
		Kubeconfig string `json:"kubeconfig"`
	}
	if err := json.UnmarshalRead(http.MaxBytesReader(w, r.Body, 1<<20), &input, json.RejectUnknownMembers(true)); err != nil || input.Kubeconfig == "" {
		http.Error(w, "invalid request or kubeconfig exceeds 1 MiB", http.StatusBadRequest)
		return
	}
	if err := m.Configure([]byte(input.Kubeconfig)); err != nil {
		http.Error(w, fmt.Sprint(err), http.StatusBadRequest)
		return
	}
	m.get(w, r)
}

// localConfig uses only the server's standard kubeconfig loading rules.
func localConfig() (clientcmd.ClientConfig, []string, string, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	loader := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{})
	raw, err := loader.RawConfig()
	if err != nil {
		return nil, nil, "", errors.New("cannot read local kubeconfig; check KUBECONFIG and file permissions")
	}
	names := make([]string, 0, len(raw.Contexts))
	for name := range raw.Contexts {
		names = append(names, name)
	}
	slices.Sort(names)
	if len(names) == 0 {
		return nil, nil, "", errors.New("no contexts found in local kubeconfig")
	}
	return loader, names, raw.CurrentContext, nil
}
func (m *Manager) ConfigureLocal(selected string) error {
	_, names, current, err := localConfig()
	if err != nil {
		return err
	}
	if selected == "" {
		selected = current
	}
	if !slices.Contains(names, selected) {
		return errors.New("select a valid context from the local kubeconfig")
	}
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	loader := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{CurrentContext: selected})
	cfg, err := loader.ClientConfig()
	if err != nil {
		return errors.New("cannot load selected context; check kubeconfig and referenced files")
	}
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return errors.New("cannot create client for selected context; check kubeconfig and certificates")
	}
	m.mu.Lock()
	m.collector = &Collector{Client: client}
	m.context = selected
	m.source = "local"
	m.revision++
	m.mu.Unlock()
	return nil
}
func (m *Manager) contexts(w http.ResponseWriter, _ *http.Request) {
	_, names, current, err := localConfig()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.MarshalWrite(w, struct {
		Contexts []string `json:"contexts"`
		Current  string   `json:"current"`
	}{names, current})
}
func (m *Manager) putLocal(w http.ResponseWriter, r *http.Request) {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
		return
	}
	var input struct {
		Context string `json:"context"`
	}
	if err := json.UnmarshalRead(http.MaxBytesReader(w, r.Body, 4096), &input, json.RejectUnknownMembers(true)); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if err := m.ConfigureLocal(input.Context); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	m.get(w, r)
}

func (m *Manager) Revision() uint64 { m.mu.RLock(); defer m.mu.RUnlock(); return m.revision }
