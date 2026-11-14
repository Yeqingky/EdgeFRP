# FRP 日志模块设计

范围: EdgeTunnel (frpc/frps) 与 EdgeNode (frps 进程管理、日志汇聚)。

## 1. 目标

- frps 由 EdgeNode 以子进程方式启动, 日志通过 stdout/stderr 直接采集, 不新增日志端口。
- frp 日志支持文本和 JSON Lines 两种格式, JSON 作为机器读取格式。
- 远程 frpc 支持可选的日志上报, 复用已认证的 frps 控制连接。
- EdgeNode 将 frps 和 frpc 日志异步转发到现有 `remotelogs` 通道。
- 日志路径不得阻塞代理数据面、控制面或 frps 退出流程。

## 2. 非目标

- 不在 EdgeNode 内实现 frps 协议栈。
- 不通过 tail 日志文件采集 frps 日志。
- 不新增公网 TCP 日志端口。
- 不实现日志查询 API 或长期日志存储; 存储仍由 EdgeAPI 负责。
- 不保证日志零丢失; 日志是调试数据, 允许在队列满时丢弃。

## 3. 总体架构

```mermaid
flowchart LR
  frps[frps 子进程] -->|stdout / stderr| node[EdgeNode]
  frpc[远程 frpc] -->|认证日志帧| frps
  node -->|有界队列| remote[remotelogs]
  remote --> api[EdgeAPI]
```

- frps 由 EdgeNode 启动, EdgeNode 读取其 stdout 和 stderr。
- frpc 日志通过控制连接上报到 frps, frps 再写入自身日志输出, 由 EdgeNode 统一采集。
- EdgeNode 不直接连接远程 frpc。

## 4. 模块划分

### 4.1 frp 日志核心

位置: `pkg/util/log`, `pkg/config/v1/common.go`, `cmd/frps/root.go`, `cmd/frpc/sub/root.go`。

职责:

- 保持现有 `console` 和文件输出行为。
- 增加 `log.format` 配置: `text` 或 `json`。
- 增加可关闭的日志输出生命周期, 供 frpc 上报器挂载和卸载。
- 提供结构化日志字段, 避免在 EdgeNode 侧用正则解析文本。

建议接口:

```go
type Sink interface {
    WriteLog(p []byte, level log.Level, when time.Time) (int, error)
    Close() error
}

func InitLogger(cfg LogConfig, extra ...Sink) error
func CloseLogger() error
```

实现约束:

- 控制台和文件继续使用 `github.com/fatedier/golib/log` 的 writer。
- JSON 格式使用 `encoding/json`, 每行一个对象。
- `disablePrintColor` 对 JSON 无意义, 但保留兼容。
- 文本格式保持现有时间、级别、caller、消息布局。
- frpc 上报器作为额外 Sink 挂载, 不替换 console/file。

### 4.2 EdgeNode frps supervisor

位置: EdgeNode 新增 `internal/apps/frps` 或同等包。

职责:

- 按配置启动 frps 子进程, 传入生成的 JSON 配置文件。
- 强制 `log.to = "console"`, `log.format = "json"`, `disablePrintColor = true`。
- 读取 stdout 和 stderr; stderr 合并到同一读取路径。
- 将日志行转发到有界队列, 由独立 goroutine 写入 `remotelogs`。
- 管理进程组, 支持优雅停止、异常退出检测和退避重启。
- 为每个 frps 实例生成实例 ID, 作为日志字段和 `remotelogs` 标签。

约束:

- 读取 goroutine 永远不能因为下游队列满而阻塞。
- 单行日志设置最大长度, 超出部分截断并计数。
- 队列满时丢弃并记录丢弃计数, 不阻塞 frps。
- 进程退出后关闭 pipe, 回收子进程, 避免僵尸进程。
- 多个 frps 实例使用独立队列或带实例 ID 的共享队列。

### 4.3 frpc 日志上报器

位置: `pkg/util/log` 的 Sink 实现, 加上 frpc 控制层。

职责:

- 新增可选配置, 默认关闭, 不影响本地 console/file。
- 登录成功后挂载上报器, 连接断开时卸载。
- 日志通过控制连接发送, 使用新的消息类型, 不在公网新增日志端口。
- frps 收到日志后写入自身日志输出, 附带客户端标识。
- 控制连接不支持该消息时自动关闭上报并输出一条警告。

约束:

- 独立有界队列和单写 goroutine, 不占用控制消息的同步写路径。
- 队列满时优先丢弃 `debug`/`trace`, 再丢 `info`; 保留 `warn`/`error`。
- 单条日志限制大小, 超长截断。
- 每个客户端限速, 防止刷日志拖垮 frps。
- 登录前的日志只写本地, 不做补发。
- 不对日志内容做隐式多行拆分; 消息内的换行必须转义。

## 5. 日志模型

JSON Lines 字段:

```json
{
  "time": "2026-11-14T23:40:00+08:00",
  "level": "info",
  "source": "frps",
  "instance": "edge-node-1",
  "clientID": "abc123",
  "runID": "def456",
  "proxy": "ssh",
  "caller": "server/service.go:389",
  "message": "login to server success"
}
```

字段说明:

| 字段 | 必填 | 说明 |
|---|---|---|
| `time` | 是 | RFC3339Nano |
| `level` | 是 | `trace`/`debug`/`info`/`warn`/`error` |
| `source` | 是 | `frps` 或 `frpc` |
| `instance` | 否 | frps 实例 ID |
| `clientID` | 否 | frpc 客户端标识 |
| `runID` | 否 | frpc 当前会话 |
| `proxy` | 否 | 关联代理名 |
| `caller` | 否 | 源码位置 |
| `message` | 是 | 日志正文 |

EdgeNode 解析失败时按 `info` 处理, 保留原始行, 不阻塞后续日志。

## 6. 配置

frps:

```json
{
  "log": {
    "to": "console",
    "level": "info",
    "format": "json",
    "disablePrintColor": true
  }
}
```

frpc 新增字段:

```json
{
  "log": {
    "to": "console",
    "level": "info",
    "format": "text",
    "report": {
      "enable": true,
      "maxQueue": 1024,
      "maxBytes": 8192,
      "maxPerSecond": 50
    }
  }
}
```

默认值:

| 字段 | 默认值 |
|---|---|
| `log.format` | `text` |
| `log.report.enable` | `false` |
| `log.report.maxQueue` | `1024` |
| `log.report.maxBytes` | `8192` |
| `log.report.maxPerSecond` | `50` |

## 7. 背压与失败处理

- frps stdout 写入是同步的; EdgeNode 必须持续读取, 否则 frps 会阻塞在日志写入。
- EdgeNode 队列满时丢弃, 不向 frps 施加反压。
- frpc 上报队列满时按级别丢弃, 不阻塞代理控制帧。
- frps 退出或 pipe 断开时, EdgeNode 停止该实例的读取 goroutine。
- EdgeNode 重启后不尝试接管旧 frps 进程; 由 supervisor 决定停止并重启。
- `remotelogs` 上传失败只记录本地错误, 不影响 frps/frpc 运行。

## 8. 安全

- 日志上报复用 frpc 到 frps 的认证会话, 不新增匿名入口。
- frps 不对公网暴露日志读取接口。
- 如果未来支持独立 frps 的 Unix socket, 仅使用本地 Unix socket, 权限 `0600`。
- 日志写入前执行脱敏:
  - `token`、`secretKey`、`password`、`Authorization`。
  - 代理插件配置中的凭据。
- 限制单条日志大小和每客户端速率。
- EdgeNode 只信任 frps 子进程的 stdout, 不解析任意远端输入为控制指令。

## 9. TODO

### Phase 1: frp 日志核心

- [ ] 在 `LogConfig` 增加 `format`, 默认 `text`, 校验只允许 `text`/`json`。
- [ ] 在 `pkg/util/log` 增加 JSON 编码器和多 Sink 支持。
- [ ] 增加 `CloseLogger`, 确保 frpc 上报器可卸载。
- [ ] 保持 `console` 和文件轮转行为不变。
- [ ] 更新 `conf/frpc_full_example.json`、`conf/frps_full_example.json`。
- [ ] 增加单元测试: 文本格式、JSON 格式、级别过滤、Sink 关闭。

验收:

- frps 配置 `format=json` 时, stdout 每行是一个可解析的 JSON 对象。
- 默认配置输出与改动前一致。
- 文件轮转和 `maxDays` 行为不变。

### Phase 2: EdgeNode frps supervisor

- [ ] 增加 frps 实例配置: 二进制路径、配置文件路径、实例 ID、重启策略。
- [ ] 生成 frps JSON 配置时强制 `log.to=console`、`log.format=json`。
- [ ] 实现 stdout/stderr 管道读取、单行长度限制和行计数。
- [ ] 实现有界队列、丢弃计数和异步 `remotelogs` 转发。
- [ ] 实现进程组、优雅停止、异常退出重启和僵尸进程回收。
- [ ] 扩展 `remotelogs` 或新增接口, 支持结构化字段和指定级别。
- [ ] 增加单元测试: 正常日志、超大行、队列溢出、进程崩溃、停止流程。

验收:

- EdgeNode 启动和停止 frps 后无残留进程。
- frps 日志可在 EdgeAPI 节点日志中按 `FRPS` 标签查看。
- frps 高频输出时, 代理端口和控制面不受影响。
- 队列满时日志丢弃可见, 进程不阻塞。

### Phase 3: frpc 日志上报

- [ ] 定义 frpc 到 frps 的日志消息类型和 JSON 字段。
- [ ] 增加握手能力标识; 不支持时自动禁用上报。
- [ ] frpc 登录后挂载上报 Sink, 断开时卸载。
- [ ] 实现有界队列、按级别丢弃、单条大小限制和每客户端限速。
- [ ] frps 处理日志消息, 附加 `clientID`/`runID` 后写入自身日志输出。
- [ ] 增加脱敏逻辑和单元测试。
- [ ] 增加多客户端、洪泛、超长消息、断线重连测试。

验收:

- 远程 frpc 的日志可在 EdgeAPI 中看到, 并带有客户端标识。
- 日志洪泛不会增加控制消息延迟。
- 旧版本 frps 不会因为新日志消息崩溃; 上报自动关闭。
- 日志中不出现 token、密码等敏感值。

### Phase 4: 运维与文档

- [ ] 增加指标: 队列深度、丢弃数、截断数、上报失败数。
- [ ] 增加日志级别和上报开关的配置说明。
- [ ] 增加 e2e: 真实 frps/frpc + EdgeNode 日志落库。
- [ ] 更新 EdgeTunnel 和 EdgeNode 的相关文档。
- [ ] 做一次安全审查, 确认没有新的公网入口和敏感信息泄漏。

## 10. 测试计划

| 层级 | 内容 |
|---|---|
| 单元测试 | JSON 编码、Sink 生命周期、队列丢弃、限速、脱敏 |
| 进程测试 | frps 启动/停止/崩溃/重启、stdout/stderr 读取、僵尸回收 |
| 集成测试 | frpc 登录后上报、断线重连、旧版本兼容 |
| 压力测试 | 高频日志、超大日志、慢消费者、多实例 |
| e2e | EdgeNode -> frps -> remotelogs -> EdgeAPI |

## 11. 风险与开放问题

| 风险 | 处理 |
|---|---|
| 控制连接被日志阻塞 | 独立队列、单写 goroutine、严格限速 |
| frps stdout 阻塞 | EdgeNode 持续读取, 下游只入有界队列 |
| 日志洪泛 | 按级别丢弃、每客户端限速、默认关闭 debug |
| 协议兼容 | 握手能力标识, 不支持则禁用 |
| 敏感信息 | 统一脱敏, 安全审查 |
| EdgeNode 重启留下孤儿 frps | 进程组管理, 启动时清理或显式重启 |

开放问题:

- 远程 frpc 是否全部由 EdgeNode 管理; 若不是, Phase 3 才需要。
- EdgeAPI 需要保留多久的 frp 日志, 是否需要按代理维度查询。
- 是否需要为每个 frps 实例单独配置日志级别。
- `maxQueue`、`maxBytes`、`maxPerSecond` 的最终默认值需要压测后确定。
