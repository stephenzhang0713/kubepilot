# KubePilot

[English](README.md) | 简体中文

KubePilot 是一个本地优先的 Kubernetes SRE 调查原型：通过 Web 查看集群概览，或针对集群组件、指定 Pod 创建调查，由 CloudWeGo Eino ReAct Agent 调用证据工具并生成分析。Agent 可以提出受限的 Deployment 滚动重启建议，经人工批准后执行；写操作默认关闭。

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

## 修复后的滚动更新验证

批准并提交 Deployment 滚动重启后，可在修复管理中点击“检查滚动更新”。KubePilot 会重新读取目标 Deployment，核对集群连接、UID、本次重启标记、提交时的 generation、控制器观察到的 generation 和副本状态。只有预期副本数大于零且与审批时一致，更新、就绪、可用和总副本数均满足预期时，才显示滚动更新已验证。验证为一次即时检查，可再次手动检查。

这只能证明 Deployment 控制器报告的滚动更新状态，不能证明业务恢复或原始故障已解决；也不提供自动回滚。完整的事件、修复计划及验证结果保存在后端内存，重启后丢失；操作里程碑会写入本地日志，见下文。

## 操作轨迹

侧栏“操作轨迹”展示事件创建、调查结束、修复计划、批准、提交结果和滚动更新检查。默认保存到后端工作目录的 `data/audit.jsonl`；可用 `KUBEPILOT_AUDIT_FILE` 指定路径。Docker Compose 使用 `audit-data` 数据卷。该文件包含目标名称和资源状态，请限制访问并做好备份；不要由多个后端进程同时写入同一个文件。

审批记录必须成功写入并同步到磁盘，重启操作才会提交。如果提交后记录结果失败，请检查目标 Deployment，切勿盲目重试。日志损坏或存在不完整记录时后端拒绝启动，需要先由管理员检查处理。该日志不包含完整调查证据或审批人身份，也不具备防篡改或多用户认证功能，不应视为合规审计系统。

## 本地集群

对于 Minikube 或 Rancher Desktop，先启动集群，使用 `kubectl config get-contexts` 和 `kubectl get nodes` 检查。在同一台机器上启动后端后，点击 **Connect cluster → Local kubeconfig**，选择对应的 context。后端遵循 client-go 的标准配置加载规则：优先使用 `KUBECONFIG`，未设置时使用 `~/.kube/config`。要使用其他文件，请在启动后端**之前**设置 `KUBECONFIG=/path/to/config`；多个文件可按系统路径分隔符配置。运行后端的用户需要能读取所引用的证书和凭据。本地 kubeconfig 中的认证插件可能在连接时于后端机器上执行，请仅使用可信配置。

对于 k3s，同样选择一个后端用户可读取的 kubeconfig 和 context。若 k3s 运行在虚拟机或其他主机上，kubeconfig 中的 API 地址必须能从后端访问，并与服务端证书匹配。不要公开或提交集群凭据。选择 context 只完成客户端配置，需刷新概览才能验证连接与权限。

## Docker Compose

安装并启动 Docker 服务后运行 `docker compose up --build`，打开 http://127.0.0.1:3000，通过 **Connect cluster** 配置 kubeconfig。Compose 默认通过 `host.docker.internal:11434` 连接 Mac 上的 Ollama；可以通过 `MODEL_BASE_URL`、`MODEL_NAME` 和 `MODEL_API_KEY` 环境变量修改模型设置。默认 Compose 不挂载宿主机 kubeconfig，本机 context 选项只能看到 API 容器内的文件；宿主机的 `localhost` 地址和证书路径也不会自动可用。Minikube 和 Rancher Desktop 推荐在宿主机运行后端。使用容器时需提供容器可达的 API 地址、适当挂载 kubeconfig 和引用文件，并在容器中安装所需认证插件。

**网页 kubeconfig：** 上传的配置仅保存在服务端内存中，重启后清除，API 不会返回凭据。配置必须内嵌证书和凭据；不支持外部文件引用或认证插件。保存只验证配置格式，不测试集群连通性。若需要本机认证插件或外部凭据文件，请在本机运行后端并使用 `KUBECONFIG`。API 无身份认证，请仅在可信本机使用，不要公开暴露。

## API

- `GET /healthz`
- `GET /api/audit`：按最新优先读取本地操作里程碑。
- `GET /api/settings`：查看当前模型名称与修复开关，不返回 API 密钥。
- `GET /api/remediations`、`POST /api/remediations`、`POST /api/remediations/{id}/execute`：列出、准备并明确批准一次性修复计划。
- `GET /api/cluster/overview`：只读采集节点、kube-system Pod、Deployment、DaemonSet、StatefulSet 和警告事件；每类最多检查 100 个资源，权限不足和截断会明确标注。
- `GET /api/kubeconfig/contexts`、`PUT /api/kubeconfig/local`：列出并选择后端标准 kubeconfig 中的 context。
- `GET /api/kubeconfig`、`PUT /api/kubeconfig`：查看配置状态或提交 `{"kubeconfig":"..."}`；响应不会包含凭据。
- `POST /api/incidents`：`{"title":"Crash loop","scope":"pod","namespace":"default","pod":"app"}`，或 `{"title":"Cluster health","scope":"cluster"}`
- `GET /api/incidents`、`GET /api/incidents/{id}`
- `POST /api/incidents/{id}/investigations`：同步运行调查，最多等待两分钟；响应中的 `status` 为 `completed` 或 `failed`。
- `GET /api/incidents/{id}/investigations`、`GET /api/investigations/{id}`

事件和调查保存在进程内存中，重启后丢失。调查过程中会向配置的模型提供采集到的集群或 Pod 证据，包括事件和可能包含的日志；日志可能包含敏感信息，使用外部模型前请自行确认数据可发送。API 没有身份认证，适合本机使用；不要直接暴露到公共网络。Agent 输出是建议，并非已经验证的根因。

## 人工批准的修复操作

Agent 可以针对集群调查中发现异常的 Deployment 提出滚动重启建议。点击 **Review plan**，在 **Repair management** 中核对集群 context、命名空间、名称、UID、资源版本、副本数和建议原因。模型没有提出受支持的建议时，不会出现可执行的修复。提出建议本身不会修改集群。

写操作默认关闭。需要使用时，在启动后端或 Compose 前设置 `KUBEPILOT_ENABLE_REMEDIATION=true`。每个计划都必须由用户单独批准；Kubernetes 身份需要目标 Deployment 的 `get` 和 `update` 权限。系统命名空间被排除。计划 15 分钟过期且只能使用一次；集群连接、对象身份或版本变化后会被拒绝。执行会更新 Pod 模板的重启注解，可能影响服务可用性。**submitted** 仅表示 API 已接受更新，不代表滚动更新或服务恢复已验证；若结果为 unconfirmed，请先检查 Deployment，再决定是否继续。

完整计划和执行状态保存在内存中，重启后丢失。当前没有用户认证，不要把 API 暴露给不可信用户或网络；仅在可信本地环境启用写入，并通过 Kubernetes RBAC 限制权限。模型不能直接执行集群操作。

## 本地操作记录

事件创建、调查完成、修复审批和结果等里程碑会追加到 `data/audit.jsonl`，可在 **Activity trace** 中查看，重启后仍然保留。设置 `KUBEPILOT_AUDIT_FILE` 可更改路径；Compose 默认使用 `audit-data` 卷。后端必须能够写入该文件；无法打开日志时不会启动，无法记录审批时会阻止修复。如果提交后的结果无法写入，应先检查 Deployment 再决定下一步。

记录包含时间、状态、对象标识和修复目标等元数据，不保存完整证据或日志，也不记录用户身份；它不是防篡改审计系统，不能恢复调查或修复计划。请保护并按需备份该文件。轮转前须停止后端，因为进程会持续持有已打开的文件；损坏或末尾不完整的日志会阻止启动。

## 验证

```sh
go test ./...
cd web && npm ci && npm run build
```

自动化测试使用模拟依赖，不替代对真实集群和模型的验证。采集事件或某个容器日志失败时，结果会显示警告；Pod 本身无法读取或模型调用失败时，调查状态为 `failed`。

集群概览基于 Kubernetes 资源状态，不能覆盖所有故障模式；当前不会直接检查 API Server 健康端点、etcd、网络、存储、指标或外部依赖。警告事件可能是历史记录。概览没有发现异常不代表整个集群健康。
