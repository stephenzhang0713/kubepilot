// Package prometheus collects bounded, read-only metric evidence.
package prometheus

import (
	"context"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/stephenzhang0713/kubepilot/internal/incidents"
)

type Client struct {
	base  *url.URL
	token string
	http  *http.Client
}

func New(rawURL, token string) (*Client, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("invalid Prometheus URL")
	}
	return &Client{base: u, token: token, http: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (c *Client) Collect(ctx context.Context, incident incidents.Incident) (string, error) {
	queries := []string{"up"}
	if incident.Scope == "pod" {
		selector := "namespace=" + strconv.Quote(incident.Namespace) + ",pod=" + strconv.Quote(incident.Pod)
		queries = []string{"kube_pod_container_status_restarts_total{" + selector + "}", "sum(rate(container_cpu_usage_seconds_total{" + selector + ",container!=\"\",container!=\"POD\"}[5m])) by (container)"}
	}
	end := time.Now().UTC().Truncate(time.Second)
	type result struct {
		Query    string         `json:"query"`
		Data     jsontext.Value `json:"data"`
		Warnings []string       `json:"warnings,omitempty"`
	}
	output := struct {
		Start   time.Time `json:"start"`
		End     time.Time `json:"end"`
		Results []result  `json:"results"`
	}{Start: end.Add(-15 * time.Minute), End: end}
	for _, query := range queries {
		u := c.base.Clone()
		u.Path = strings.TrimRight(u.Path, "/") + "/api/v1/query_range"
		params := u.Query()
		params.Set("query", query)
		params.Set("start", output.Start.Format(time.RFC3339))
		params.Set("end", end.Format(time.RFC3339))
		params.Set("step", "60s")
		params.Set("limit", "50")
		params.Set("timeout", "8s")
		u.RawQuery = params.Encode()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return "", err
		}
		if c.token != "" {
			req.Header.Set("Authorization", "Bearer "+c.token)
		}
		resp, err := c.http.Do(req)
		if err != nil {
			return "", fmt.Errorf("query failed: %w", err)
		}
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, 1024*1024+1))
		resp.Body.Close()
		if readErr != nil || len(data) > 1024*1024 {
			return "", fmt.Errorf("query response unavailable or too large")
		}
		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("query returned HTTP %d", resp.StatusCode)
		}
		var body struct {
			Status   string         `json:"status"`
			Data     jsontext.Value `json:"data"`
			Warnings []string       `json:"warnings"`
		}
		if err := json.Unmarshal(data, &body); err != nil || body.Status != "success" || len(body.Data) == 0 {
			return "", fmt.Errorf("invalid or unsuccessful query response")
		}
		output.Results = append(output.Results, result{Query: query, Data: body.Data, Warnings: body.Warnings})
	}
	b, err := json.Marshal(output)
	return string(b), err
}
