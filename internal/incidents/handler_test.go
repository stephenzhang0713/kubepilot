package incidents

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIncidentLifecycle(t *testing.T) {
	mux := http.NewServeMux()
	New().Routes(mux)
	req := httptest.NewRequest("POST", "/api/incidents", strings.NewReader(`{"title":"CrashLoop","namespace":"default","pod":"app"}`))
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", res.Code, res.Body.String())
	}
	var item Incident
	if err := json.Unmarshal(res.Body.Bytes(), &item); err != nil {
		t.Fatal(err)
	}
	if item.ID == "" || item.Pod != "app" {
		t.Fatalf("bad incident: %+v", item)
	}
	res = httptest.NewRecorder()
	mux.ServeHTTP(res, httptest.NewRequest("GET", "/api/incidents/"+item.ID, nil))
	if res.Code != http.StatusOK {
		t.Fatalf("get: %d", res.Code)
	}
	res = httptest.NewRecorder()
	mux.ServeHTTP(res, httptest.NewRequest("POST", "/api/incidents", strings.NewReader(`{"title":"missing pod"}`)))
	if res.Code != http.StatusBadRequest {
		t.Fatalf("validation: %d", res.Code)
	}
}
