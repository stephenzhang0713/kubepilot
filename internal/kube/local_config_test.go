package kube

import (
	"bytes"
	json "encoding/json/v2"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalContextWithReferencedCertificate(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret-token" {
			t.Error("expected configured credentials")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/namespaces/default/pods/app":
			fmt.Fprint(w, `{"metadata":{"name":"app","uid":"uid"},"spec":{"containers":[]},"status":{"phase":"Running"}}`)
		case "/api/v1/namespaces/default/events":
			fmt.Fprint(w, `{"items":[]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ca.crt"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	config := fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: local
  cluster:
    server: %s
    certificate-authority: ca.crt
users:
- name: user
  user:
    token: secret-token
contexts:
- name: minikube
  context:
    cluster: local
    user: user
- name: rancher-desktop
  context:
    cluster: local
    user: user
current-context: minikube
`, server.URL)
	file := filepath.Join(dir, "config")
	if err := os.WriteFile(file, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KUBECONFIG", file)
	m := NewManager()
	if got := NewManagerForContext("missing").Status(); got.Configured {
		t.Fatal("missing saved context must not fall back to a different cluster")
	}
	if got := NewManagerForContext("rancher-desktop").Status(); got.Context != "rancher-desktop" {
		t.Fatalf("saved context: %+v", got)
	}
	if got := m.Status(); got.Context != "minikube" || got.Source != "local" {
		t.Fatalf("initial status: %+v", got)
	}
	mux := http.NewServeMux()
	m.Routes(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/kubeconfig/contexts", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "rancher-desktop") || strings.Contains(w.Body.String(), "secret-token") {
		t.Fatalf("contexts: %d %s", w.Code, w.Body.String())
	}
	payload, _ := json.Marshal(map[string]string{"context": "rancher-desktop"})
	req := httptest.NewRequest(http.MethodPut, "/api/kubeconfig/local", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK || m.Status().Context != "rancher-desktop" {
		t.Fatalf("switch: %d %s", w.Code, w.Body.String())
	}
	evidence, err := m.Collect(t.Context(), "default", "app")
	if err != nil || !strings.Contains(evidence.Pod, "Running") {
		t.Fatalf("collect: %+v %v", evidence, err)
	}
	if err := m.ConfigureLocal("missing"); err == nil || m.Status().Context != "rancher-desktop" {
		t.Fatalf("failed switch changed current context: %v", err)
	}
}
func TestLocalContextAbsent(t *testing.T) {
	t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "missing"))
	m := NewManager()
	if m.Status().Configured {
		t.Fatal("unexpected configured cluster")
	}
	if err := m.ConfigureLocal(""); err == nil {
		t.Fatal("expected missing context error")
	}
}
