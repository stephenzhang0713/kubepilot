# KubePilot

KubePilot is a local Kubernetes troubleshooting assistant: inspect cluster resources, collect evidence, ask a model for analysis, and follow up in the terminal. The interface uses Bubble Tea v2 and Lip Gloss v2.

[简体中文](README_zh-CN.md)

## Quick start

You need Go 1.27, a reachable Kubernetes cluster and local kubeconfig, and an OpenAI-compatible model with tool calling. For example, start Ollama and run `ollama pull qwen2.5:7b`.

```sh
go build -o kubepilot ./cmd/kubepilot
./kubepilot
```

Press `5` to configure the model and kubeconfig. KubePilot initially uses the current local kubeconfig context. Press `c` or choose a context in Settings, then press `r` to refresh the cluster inspection. Restart after changing the kubeconfig path before selecting a context from the new file.

## Troubleshooting

- `1` Overview: inspect read-only resource checks, findings, and unknown states. Unknown, truncated, or failed checks do not establish health. Press `n` to start an investigation.
- `2` Investigations: press `n` to select a cluster or Pod and collect evidence for model analysis. Select an incident and press `Enter` to view its result.
- `3` Assistant: run an investigation first, then press `Enter` to ask a question. You can also press `a` in a result. The assistant uses that investigation's snapshot and the current conversation; it does not automatically collect new data or perform actions. Run another investigation for fresh evidence.
- `4` Repairs: when a result includes a supported proposal, select it and prepare a plan. Review the plan, press `a` and `y` to confirm, `x` to reject, or `v` to check rollout. Writes are disabled by default and can be enabled in Settings. Restarting a Deployment can disrupt service; submission does not prove recovery.
- `5` Settings: use arrow keys and `Enter` to edit or toggle; `Ctrl+U` clears, `Enter` saves, and `Esc` cancels. Context changes apply immediately; restart to apply other settings.

The footer shows current shortcuts. Investigations, conversations, repair plans and inspection results live only in memory and disappear on exit. The app no longer uses a database or audit file, or integrates with Prometheus or Alertmanager. Files created by older versions are not deleted automatically.

Investigation evidence includes resource details, events and logs and is sent to the configured model. Model output is advice, not a verified root cause; events can be historical and evidence can be incomplete. Repair plans expire and are single-use; system namespaces are excluded and the Kubernetes identity needs appropriate RBAC permissions.

## Settings and privacy

Settings are stored in `kubepilot/config.json` under the system user configuration directory (typically `~/Library/Application Support/kubepilot/config.json` on macOS). The model key is hidden in the interface but stored in plaintext in this file, created with `0600` permissions; protect your account and backups. Previous model, kubeconfig, inspection and remediation environment variables remain supported as initial defaults; saved settings take precedence.

## Development

```sh
go test ./...
go build ./cmd/kubepilot
```
