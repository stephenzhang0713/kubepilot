# KubePilot

KubePilot 是本地运行的 Kubernetes 故障定位助手：先检查集群资源并展示发现，再收集证据、使用模型分析，并在终端里追问。界面基于 Bubble Tea v2 和 Lip Gloss v2。

[English](README.md)

## 快速开始

需要 Go 1.27、可访问的 Kubernetes 集群、本地 kubeconfig，以及支持工具调用的 OpenAI 兼容模型。例如启动 Ollama 后运行 `ollama pull qwen2.5:7b`。

```sh
go build -o kubepilot ./cmd/kubepilot
./kubepilot
```

按 `5` 打开设置，配置模型和 kubeconfig。默认读取本地 kubeconfig 当前 context；按 `c` 或在设置中选择 context，按 `r` 刷新集群检查。更改 kubeconfig 路径后需要重启，再选择新配置中的 context。

## 故障定位

- `1` 集群概览：查看只读检查的资源、异常和未知状态。未知、截断或采集失败不代表健康；按 `n` 开始调查。
- `2` 调查：按 `n` 指定集群或 Pod，采集证据并请求模型分析；选择事件并按 `Enter` 查看结果。
- `3` 助手：先完成一次调查，再按 `Enter` 输入问题。也可在调查结果中按 `a` 打开助手。助手基于该次调查的快照和当前会话对话回答，不会自动读取新数据或执行操作。需要新证据时重新调查。
- `4` 修复：调查有支持的建议时，可在结果中选择并准备计划。进入计划后审核，按 `a` 和 `y` 确认；`x` 拒绝，`v` 检查滚动更新。写操作默认关闭，可在设置中启用。重启 Deployment 可能影响服务；提交不代表恢复成功。
- `5` 设置：方向键选择，`Enter` 编辑或切换，编辑时 `Ctrl+U` 清空、`Enter` 保存、`Esc` 取消。context 切换立即生效，其他设置保存后重启生效。

界面底部显示当前快捷键。调查、对话、修复计划和巡检结果仅保存在进程内，退出后消失；程序不再使用数据库或审计文件，也不接入 Prometheus 和 Alertmanager。旧版本生成的文件不会自动删除。

调查证据包括资源信息、事件和日志，会发送给配置的模型。模型输出是建议，不是已验证的根因；事件可能是历史记录，采集也可能不完整。修复计划有时效且只能执行一次，系统命名空间被排除，Kubernetes 身份需要适当的 RBAC 权限。

## 设置与隐私

设置保存在系统用户配置目录的 `kubepilot/config.json`（macOS 通常为 `~/Library/Application Support/kubepilot/config.json`）。模型密钥在界面中隐藏，但以明文保存在该文件中；文件创建权限为 `0600`，请保护账户和备份。首次运行时兼容旧的模型、kubeconfig、巡检和修复环境变量作为默认值；保存的设置优先。

## 开发

```sh
go test ./...
go build ./cmd/kubepilot
```
