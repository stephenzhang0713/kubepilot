# KubePilot

[English](README.md) | 简体中文

KubePilot 是一个本地优先的 Kubernetes SRE 调查原型：通过 Web 查看集群概览，或针对集群组件、指定 Pod 创建调查，由 CloudWeGo Eino ReAct Agent 调用证据工具并生成分析。此版本只读，不会执行修复。

## 前提

- Go 1.27、Node.js 22+ 和 npm。
- 可访问的 Kubernetes 集群和 kubeconfig；Pod 调查需要目标 namespace 的 `pods`、`pods/log` 的 `get` 和 `events` 的 `list` 权限。集群概览及集群调查需要对节点、Pod、事件、Deployment、DaemonSet 和 StatefulSet 的集群范围 `list` 权限；权限不足的检查项会显示为未知。
- 支持 OpenAI Chat Completions **工具调用**的服务。默认连接本机 Ollama：`http://localhost:11434/v1`，模型 `qwen2.5:7b`。先安装并启动 Ollama，运行 `ollama pull qwen2.5:7b`。模型应支持工具调用；Mac 可本地运行，无需 NVIDIA GPU。

## 本机启动（推荐）

先通过 `kubectl get pod -n <namespace>` 确认上下文和 Pod，再在项目根目录运行：

```sh
# 可选：设置其他兼容模型服务
export MODEL_BASE_URL=http://localhost:11434/v1
export MODEL_NAME=qwen2.5:7b
export MODEL_API_KEY=ollama
# KUBECONFIG 默认为 ~/.kube/config
go run ./cmd/kubepilot
```

另开终端：

```sh
cd web
npm ci
npm run dev
```

打开终端显示的 Vite 地址（默认 http://127.0.0.1:5173），点击 **Connect cluster → Local kubeconfig** 选择本机 context，或上传内嵌凭据的 kubeconfig，然后刷新集群概览，或选择集群、Pod 范围并运行调查。如果后端启动时已加载本地 kubeconfig，也可以直接使用。API 默认只监听 `127.0.0.1:8080`。如果本地配置不可用，请检查 `kubectl config current-context`，或在网页中配置。

## 本地集群

对于 Minikube 或 Rancher Desktop，先启动集群，使用 `kubectl config get-contexts` 和 `kubectl get nodes` 检查。在同一台机器上启动后端后，点击 **Connect cluster → Local kubeconfig**，选择对应的 context。后端遵循 client-go 的标准配置加载规则：优先使用 `KUBECONFIG`，未设置时使用 `~/.kube/config`。要使用其他文件，请在启动后端**之前**设置 `KUBECONFIG=/path/to/config`；多个文件可按系统路径分隔符配置。运行后端的用户需要能读取所引用的证书和凭据。本地 kubeconfig 中的认证插件可能在连接时于后端机器上执行，请仅使用可信配置。

对于 k3s，同样选择一个后端用户可读取的 kubeconfig 和 context。若 k3s 运行在虚拟机或其他主机上，kubeconfig 中的 API 地址必须能从后端访问，并与服务端证书匹配。不要公开或提交集群凭据。选择 context 只完成客户端配置，需刷新概览才能验证连接与权限。

## Docker Compose

安装并启动 Docker 服务后运行 `docker compose up --build`，打开 http://127.0.0.1:3000，通过 **Connect cluster** 配置 kubeconfig。Compose 默认通过 `host.docker.internal:11434` 连接 Mac 上的 Ollama；可以通过 `MODEL_BASE_URL`、`MODEL_NAME` 和 `MODEL_API_KEY` 环境变量修改模型设置。默认 Compose 不挂载宿主机 kubeconfig，本机 context 选项只能看到 API 容器内的文件；宿主机的 `localhost` 地址和证书路径也不会自动可用。Minikube 和 Rancher Desktop 推荐在宿主机运行后端。使用容器时需提供容器可达的 API 地址、适当挂载 kubeconfig 和引用文件，并在容器中安装所需认证插件。

**网页 kubeconfig：** 上传的配置仅保存在服务端内存中，重启后清除，API 不会返回凭据。配置必须内嵌证书和凭据；不支持外部文件引用或认证插件。保存只验证配置格式，不测试集群连通性。若需要本机认证插件或外部凭据文件，请在本机运行后端并使用 `KUBECONFIG`。API 无身份认证，请仅在可信本机使用，不要公开暴露。

## API

- `GET /healthz`
- `GET /api/cluster/overview`：只读采集节点、kube-system Pod、Deployment、DaemonSet、StatefulSet 和警告事件；每类最多检查 100 个资源，权限不足和截断会明确标注。
- `GET /api/kubeconfig/contexts`、`PUT /api/kubeconfig/local`：列出并选择后端标准 kubeconfig 中的 context。
- `GET /api/kubeconfig`、`PUT /api/kubeconfig`：查看配置状态或提交 `{"kubeconfig":"..."}`；响应不会包含凭据。
- `POST /api/incidents`：`{"title":"Crash loop","scope":"pod","namespace":"default","pod":"app"}`，或 `{"title":"Cluster health","scope":"cluster"}`
- `GET /api/incidents`、`GET /api/incidents/{id}`
- `POST /api/incidents/{id}/investigations`：同步运行调查，最多等待两分钟；响应中的 `status` 为 `completed` 或 `failed`。
- `GET /api/incidents/{id}/investigations`、`GET /api/investigations/{id}`

事件和调查保存在进程内存中，重启后丢失。调查过程中会向配置的模型提供采集到的集群或 Pod 证据，包括事件和可能包含的日志；日志可能包含敏感信息，使用外部模型前请自行确认数据可发送。API 没有身份认证，适合本机使用；不要直接暴露到公共网络。Agent 输出是建议，并非已经验证的根因。

## 验证

```sh
go test ./...
cd web && npm ci && npm run build
```

自动化测试使用模拟依赖，不替代对真实集群和模型的验证。采集事件或某个容器日志失败时，结果会显示警告；Pod 本身无法读取或模型调用失败时，调查状态为 `failed`。

集群概览基于 Kubernetes 资源状态，不能覆盖所有故障模式；当前不会直接检查 API Server 健康端点、etcd、网络、存储、指标或外部依赖。警告事件可能是历史记录。概览没有发现异常不代表整个集群健康。
