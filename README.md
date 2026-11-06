# KubePilot

English | [简体中文](README_zh-CN.md)

KubePilot is a local-first Kubernetes SRE investigation prototype. Inspect a cluster-wide snapshot or investigate either cluster components or a specific Pod. A CloudWeGo Eino ReAct agent calls an evidence tool and generates an assessment from the collected data. The agent can propose a limited Deployment rolling restart for explicit human approval; execution is disabled by default.

## Prerequisites

- Go 1.27, Node.js 22+, and npm.
- An accessible Kubernetes cluster and a kubeconfig. Pod investigations need `get` on `pods` and `pods/log` and `list` on `events` in the target namespace. Cluster snapshots and investigations need cluster-wide `list` access to nodes, Pods, events, Deployments, DaemonSets and StatefulSets. Restricted access is reported per section as unknown.
- A service that supports **tool calling** through the OpenAI Chat Completions API. By default, KubePilot connects to Ollama at `http://localhost:11434/v1` using `qwen2.5:7b`. Install and start Ollama, then run `ollama pull qwen2.5:7b`. Choose a model that supports tool calling. It can run locally on a Mac without an NVIDIA GPU.

## Run locally (recommended)

First, use `kubectl get pod -n <namespace>` to check the current context and target Pod. Then run the backend from the project root:

```sh
# Optional: configure an OpenAI-compatible model service
export MODEL_BASE_URL=http://localhost:11434/v1
export MODEL_NAME=qwen2.5:7b
export MODEL_API_KEY=ollama
# KUBECONFIG defaults to ~/.kube/config
go run ./cmd/kubepilot
```

In a separate terminal, start the web interface:

```sh
cd web
npm ci
npm run dev
```

Open the Vite URL shown in the terminal (default: http://127.0.0.1:5173). Use **Connect cluster → Local kubeconfig** to select a context, or upload an embedded kubeconfig, then refresh the cluster snapshot or select cluster or Pod scope and run an investigation. If a local kubeconfig was loaded at startup, you can also use it directly. The API listens on `127.0.0.1:8080` by default. If the local configuration is unavailable, check `kubectl config current-context` or configure it in the browser.

## Verify a restart rollout

After approving and submitting a Deployment restart, use **Check rollout** in repair management to fetch fresh Deployment status. Verification checks the cluster connection, UID, restart marker, submitted generation, observed generation, and replica counts. It requires a positive replica count unchanged since approval, with updated, ready, available, and total replicas matching the desired count. This is a point-in-time check and can be repeated.

This verifies only the Deployment controller's rollout status, not application health or resolution of the original incident. There is no automatic rollback. Full incidents, plans, and verification results are held in backend memory and lost on restart; operation milestones are recorded locally as described below.

## Activity trace

The activity trace records incident creation, investigation completion, repair plans, approvals, submission outcomes, and rollout checks. The default file is `data/audit.jsonl` relative to the backend working directory; set `KUBEPILOT_AUDIT_FILE` to change it. Docker Compose stores it in the `audit-data` volume. The file contains target names and resource status: restrict access and back it up. Do not share one journal between backend processes.

An approval must be written and synced before a restart is submitted. If recording the result fails after submission, inspect the target Deployment before any retry. The backend refuses to start on a corrupt or incomplete journal until an administrator inspects it. The journal does not contain full investigation evidence or user identities, and is not tamper-proof or a substitute for authenticated compliance auditing.

## Local clusters

For Minikube or Rancher Desktop, start the cluster and check `kubectl config get-contexts` and `kubectl get nodes`. Start the backend on the same machine, open **Connect cluster → Local kubeconfig**, and select the desired context. The backend uses the standard client-go loading rules: `KUBECONFIG` if set, otherwise `~/.kube/config`. Set `KUBECONFIG=/path/to/config` **before** starting the backend to use a different file; multiple files can be separated with the platform path separator. The backend account needs read access to referenced certificates and credentials. Local kubeconfigs may use authentication plugins, which run on the backend machine when needed; use only trusted configs.

For k3s, configure a kubeconfig readable by the backend user and select its context in the same way. If k3s runs in a VM or another host, its API server address must be reachable from the backend, and its certificate must match that address. Do not expose or commit cluster credentials. Saving a context configures the client; refresh the snapshot to test access and permissions.

## Docker Compose

Install and start Docker, then run `docker compose up --build` and open http://127.0.0.1:3000. Use **Connect cluster** to provide a kubeconfig. Compose connects to Ollama on the Mac through `host.docker.internal:11434` by default. Set `MODEL_BASE_URL`, `MODEL_NAME`, and `MODEL_API_KEY` as environment variables to change the model settings. The default Compose setup does not mount the host kubeconfig; the local-context option only sees files inside the API container. Host `localhost` addresses and host certificate paths are not automatically available there. For Minikube and Rancher Desktop, local backend startup is recommended. A container deployment needs a reachable API address, a suitable mounted kubeconfig and referenced files, and any required auth plugin installed in the container.

**Web kubeconfig:** Uploaded configurations are kept in server memory until restart and are not returned by the API. They must use embedded certificates and credentials; external file references and authentication plugins are rejected. Saving validates the configuration but does not test connectivity. For host-based authentication plugins or external credential files, run the backend locally with `KUBECONFIG` instead. The API has no authentication: keep it on a trusted local machine and do not expose it publicly.

## API

- `GET /healthz`
- `GET /api/audit`: list local operation milestones, newest first.
- `GET /api/settings`: current model name and repair enablement (no API key).
- `GET /api/remediations`, `POST /api/remediations`, `POST /api/remediations/{id}/execute`: list, prepare, and explicitly approve a single-use repair plan.
- `GET /api/cluster/overview`: a read-only snapshot of nodes, kube-system Pods, Deployments, DaemonSets, StatefulSets and warning events. Each category inspects at most 100 resources; permission failures and truncation are reported.
- `GET /api/kubeconfig/contexts`, `PUT /api/kubeconfig/local`: list and select a context from the backend’s standard kubeconfig.
- `GET /api/kubeconfig`, `PUT /api/kubeconfig`: inspect configuration status or submit `{"kubeconfig":"..."}`; responses never include credentials.
- `POST /api/incidents`: `{"title":"Crash loop","scope":"pod","namespace":"default","pod":"app"}` or `{"title":"Cluster health","scope":"cluster"}`
- `GET /api/incidents` and `GET /api/incidents/{id}`
- `POST /api/incidents/{id}/investigations`: runs an investigation synchronously for up to two minutes; the response `status` is `completed` or `failed`.
- `GET /api/incidents/{id}/investigations` and `GET /api/investigations/{id}`

Incidents and investigations are stored in process memory and are lost on restart. Collected cluster or Pod evidence, including events and potentially logs, is sent to the configured model during an investigation. Logs may contain sensitive information; confirm that data can be sent before using an external model. The API has no authentication and is intended for local use; do not expose it directly to the public internet. Agent output is advice, not a verified root cause.

## Approved repair operations

The agent can propose a rolling restart of a Deployment identified in a cluster investigation. Select **Review plan**, then review its cluster context, namespace, name, UID, resource version, replica count and proposed reason in **Repair management**. If the model does not make a supported proposal, no repair is available. The proposal is not itself a change.

Write operations are disabled by default. To enable them, set `KUBEPILOT_ENABLE_REMEDIATION=true` before starting the backend or Compose. The user must explicitly approve each plan. The Kubernetes identity needs `get` and `update` on the target Deployment. System namespaces are excluded. A plan expires after 15 minutes, is single-use, and is rejected if the cluster connection or Deployment identity/version changes. The operation updates the Pod template restart annotation and may disrupt service. A **submitted** status means the API accepted the update; it does not verify the rollout or recovery. If the result is unconfirmed, check the Deployment before taking further action.

Full plans and execution state are stored in memory and disappear on restart. There is no user authentication. Do not expose the API to untrusted users or networks; enable write access only for a trusted local setup with appropriately limited Kubernetes RBAC. The model cannot directly execute cluster operations.

## Local activity journal

Milestones such as incident creation, investigation completion, repair approval and results are appended to `data/audit.jsonl` and shown in **Activity trace** across restarts. Set `KUBEPILOT_AUDIT_FILE` to change the path; Compose uses the `audit-data` volume by default. The backend must be able to write the file: it will not start if the journal cannot be opened, and a repair is blocked if its approval cannot be recorded. If recording a submitted result fails, inspect the Deployment before deciding what to do next.

The journal contains metadata such as timestamps, status, identifiers and repair targets, but not full evidence, logs or user identities. It is not tamper-proof and cannot restore investigations or repair plans. Protect and back up the file as appropriate. Stop the backend before rotating it: the process keeps the file open. A corrupt or incomplete final record prevents startup.

## Verification

```sh
go test ./...
cd web && npm ci && npm run build
```

Automated tests use simulated dependencies and do not replace validation against a real cluster and model. The result displays a warning if events or a container's logs cannot be collected. If the Pod cannot be read or the model call fails, the investigation status is `failed`.

A cluster snapshot checks Kubernetes resource status, not all possible failure modes. It does not directly test API server health endpoints, etcd, network, storage, metrics, or external dependencies. Warning events can be historical. A clean snapshot is not a guarantee of overall health.
