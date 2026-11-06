package kube

import (
	"bytes"
	json "encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const validConfig = `apiVersion: v1
kind: Config
clusters:
- name: test
  cluster:
    server: https://example.test
contexts:
- name: test-context
  context:
    cluster: test
    user: test-user
current-context: test-context
users:
- name: test-user
  user:
    token: secret-token
`

func TestConfigureRoutes(t *testing.T) {
	m := &Manager{}
	mux := http.NewServeMux()
	m.Routes(mux)
	save := func(config, contentType string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"kubeconfig": config})
		req := httptest.NewRequest(http.MethodPut, "/api/kubeconfig", bytes.NewReader(body))
		req.Header.Set("Content-Type", contentType)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		return w
	}
	if w := save(validConfig, "application/json"); w.Code != 200 || strings.Contains(w.Body.String(), "secret-token") {
		t.Fatalf("save: %d %s", w.Code, w.Body.String())
	}
	if w := save("not yaml", "application/json"); w.Code != 400 {
		t.Fatalf("invalid config: %d", w.Code)
	}
	if w := save(validConfig, "text/plain"); w.Code != 415 {
		t.Fatalf("content type: %d", w.Code)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/kubeconfig", nil))
	if !strings.Contains(w.Body.String(), "test-context") || strings.Contains(w.Body.String(), "secret-token") {
		t.Fatalf("status: %s", w.Body.String())
	}
	if m.Status().Context != "test-context" {
		t.Fatal("invalid submission changed existing configuration")
	}
}
func TestRejectExternalCredentials(t *testing.T) {
	for _, replacement := range []string{"exec:\n      apiVersion: client.authentication.k8s.io/v1\n      command: example", "tokenFile: /etc/passwd", "client-key: /tmp/key"} {
		t.Run(replacement, func(t *testing.T) {
			m := &Manager{}
			if err := m.Configure([]byte(strings.Replace(validConfig, "token: secret-token", replacement, 1))); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
}
func TestNoConfiguredCluster(t *testing.T) {
	m := &Manager{}
	if _, err := m.Collect(t.Context(), "default", "pod"); err == nil {
		t.Fatal("expected error")
	}
}

func TestConfiguredClientUsedForCollection(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret-token" {
			t.Error("configured credentials not used")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/namespaces/default/pods/app":
			_, _ = w.Write([]byte(`{"metadata":{"name":"app","uid":"uid"},"spec":{"containers":[]},"status":{"phase":"Running"}}`))
		case "/api/v1/namespaces/default/events":
			_, _ = w.Write([]byte(`{"items":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	m := &Manager{}
	if err := m.Configure([]byte(strings.Replace(validConfig, "server: https://example.test", "server: "+server.URL+"\n    insecure-skip-tls-verify: true", 1))); err != nil {
		t.Fatal(err)
	}
	evidence, err := m.Collect(t.Context(), "default", "app")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(evidence.Pod, "Running") {
		t.Fatalf("unexpected evidence: %+v", evidence)
	}
}
