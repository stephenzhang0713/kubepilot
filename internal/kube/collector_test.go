package kube

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func TestCollectReadsPodEventsAndLogs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/namespaces/default/pods/app":
			fmt.Fprint(w, `{"metadata":{"name":"app","uid":"pod-uid"},"spec":{"containers":[{"name":"app","image":"example"}]},"status":{"phase":"Running"}}`)
		case "/api/v1/namespaces/default/events":
			if !strings.Contains(r.URL.Query().Get("fieldSelector"), "pod-uid") {
				t.Error("events not scoped to pod UID")
			}
			fmt.Fprint(w, `{"items":[{"message":"Back-off restarting"}]}`)
		case "/api/v1/namespaces/default/pods/app/log":
			if r.URL.Query().Get("tailLines") != "100" {
				t.Error("logs not bounded")
			}
			fmt.Fprint(w, "connection refused")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	result, err := (&Collector{Client: client}).Collect(t.Context(), "default", "app")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Pod, "Running") || !strings.Contains(result.Events, "Back-off") || !strings.Contains(result.Logs, "connection refused") {
		t.Fatalf("missing evidence: %+v", result)
	}
}
