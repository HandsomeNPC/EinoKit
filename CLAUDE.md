# CLAUDE.md — EinoKit

EinoKit 是一个 AI 脚手架项目:后端基于 [Eino](https://github.com/cloudwego/eino)(CloudWeGo 的 LLM 应用框架,含 ADK),前端基于 Vue 3 + TDesign,通过 AG-UI 协议与后端流式交互。目标是用"买奶茶"等示例 Demo 端到端打通 Thinking / Tool Call / Human-in-the-Loop / A2UI 卡片 / Callback 进度 / 订单落库 等能力,作为后续业务 Agent 的起点。

## 仓库结构

```
EinoKit/
├── server/      # Go 后端 (Eino + ADK + AG-UI SDK)
│   ├── cmd/main.go
│   └── internal/
│       ├── agents/      # Agent 注册表与 demo agent
│       ├── agui/        # AG-UI 事件发射器与历史渲染
│       ├── handler/     # gin handler (health / thread)
│       ├── memory/      # 会话与消息存储 (GORM)
│       ├── msgops/      # AgenticMessage 归一化/抽取
│       ├── turnloop/    # ADK TurnLoop 封装、中断映射
│       └── tools/       # 示例工具 (order_milk_tea)
├── web/         # Vue 3 + TS + Vite + TDesign 前端
└── CLAUDE.md
```

## 开发命令

### 前端 (`web/`)
```powershell
cd web
npm install
npm run dev       # vite dev,默认 http://localhost:5173
npm run build     # vue-tsc -b && vite build
npm run preview
```

### 后端 (`server/`)
```powershell
cd server
go mod tidy
go run ./cmd           # 默认 :8080
go test ./...
go build ./...
```

## HTTP 接口约定

后端路由是**裸路径**,不带 `/api` 前缀。前端统一请求 `/api/*`,开发期由
`web/vite.config.ts` 的代理 rewrite 掉前缀,生产由网关承担。

会话级接口统一 **POST + 请求体里的 `threadId`**,不用 URL 路径参数——
这样 SSE endpoint 是静态常量,前端切会话只改 body,不必 `updateEndpoint`
(改 endpoint 还要处理引擎只在 init 求值一次配置带来的竞态)。

| 方法 | 路径 | 说明 |
|------|------|------|
| GET  | `/health` | 健康检查 |
| POST | `/threads` | 创建会话 |
| GET  | `/threads` | 列出会话 |
| POST | `/threads/delete` | 删除会话 |
| POST | `/threads/chat` | 对话(SSE,AG-UI 事件流) |
| POST | `/threads/render` | 渲染会话历史 |
| POST | `/threads/abort` | 终止当前运行 |
| POST | `/threads/docs` | 上传文档(multipart,`threadId` 走表单字段) |

### 中断恢复(Human-in-the-Loop)

**所有**中断都走同一条通道,与具体是哪个工具无关:

- 后端在 `RUN_FINISHED.outcome.interrupts[]` 里给出 `{id, toolCallId, message}`,
  **同时**把这份快照落库到 `thread_info.pending_interrupt`。
  `toolCallId` 由 `turnloop.toolCallIDOf` 从 ADK 中断地址的 `tool` 段解析
  (取 `SubID`,因为 compose 把工具名放 `ID`、call ID 放 `SubID`)。
- 前端按 `toolCallId` 把中断关联回 tool call 块,复用该工具已注册的渲染组件。
  刷新页面后 `/threads/render` 会带回 `interrupts`,审批卡片照样能恢复。
- 恢复时发 `POST /threads/chat` 带 `{threadId, resume:[{status, payload}]}`。
  **`interruptId` 不由前端传**——后端从自己的快照里取(`buildChatItem`),
  客户端只表达"批准还是拒绝"。传入值一概忽略,避免过期或伪造的 ID。

两处状态的权威关系:Redis 里的 **checkpoint 是唯一权威**(它才让 ADK 真的能
resume),`thread_info.pending_interrupt` 只是展示/回填用的副本。
`loadPendingInterrupts` 先查 checkpoint,不在就一律当作无待确认中断——
否则 checkpoint 过期(TTL)或 Redis 被清后,前端会显示一张点了就报错的卡片。
所有"非中断"的收尾路径(正常结束、出错、`/abort`)都会清掉这份副本。

新增需要审批的工具时,只需注册一个渲染组件,前后端的中断传递链路都不用改。

## 技术栈基线

- **后端**:Go + Eino (`github.com/cloudwego/eino` 含 `/adk` 子包) + AG-UI Go SDK(走正常 go.mod 依赖,无 `replace`)。模块路径 `github.com/HandsomeNPC/EinoKit`。Store:PostgreSQL(GORM,见 `internal/memory`);checkpoint 走 Redis(`internal/checkpoint`)。
- **前端**:Vue 3 `<script setup>` + TypeScript + Vite + TDesign Vue Next (`tdesign-vue-next`) + `@tdesign-vue-next/chat`。AG-UI 客户端走 ChatEngine + AGUIAdapter。
- **模型**:按 Claude Code 环境提示,构建 AI 应用默认用最新最强 Claude 模型(Opus/Sonnet/Haiku)。后端 Eino 的 Model 调用按业务自选 provider,不在脚手架强绑定。

### Eino 使用指引:优先调用 skill

涉及 Eino 后端开发时,**先用对应的 `/eino-*` skill**(项目 `.claude/skills/` 下,已加载),不要凭记忆写 API——Eino 迭代快,记忆易过期。按场景选:

| 场景 | Skill | 何时用 |
|------|-------|--------|
| 框架总览/概念/入门、不确定用哪个 skill | `/eino-guide` | 通用问题、架构理解、跨概念导航 |
| 构建 Agent、ReAct、中间件、Runner、interrupt/resume、TurnLoop、agent-as-tool | `/eino-agent` | ADK Agent 相关 |
| Chain/Graph/Workflow 编排、流式、checkpoint/state、Runnable | `/eino-compose` | 多步流水线与编排 |
| ChatModel/Embedding/Retriever/Indexer/Tool/Document/Prompt/Callback 选型与接入 | `/eino-component` | 组件选择与配置(含 eino-ext 各 provider 实现) |

四个 skill 都带 `reference/` 子目录的细分文档(如 `eino-component/reference/model/claude.md`、`eino-agent/reference/human-in-the-loop.md`),skill 内部会按需引用。不要手写 Eino 的导入路径或 API 签名,先查 skill。

## 代码风格

- Go:`gofmt` + `goimports`;包名短小,导出函数带 doc 注释。
- TS/Vue:`<script setup lang="ts">`,组合式 API;不混用 Options API。用 `vue-tsc` 保证类型构建通过。
- 注释密度匹配周边代码,不堆砌显而易见的注释。
- 引用代码用 `file_path:line_number` 格式,可点击。

## Git

- 主分支 `master`(远端只有 `origin/master`,没有 `main`),开发在 `master` 或特性分支。提交前不要 `--no-verify` 跳 hook。
- 远端:`codeup.aliyun.com/.../Platform/EinoKit.git`。
- `.idea/`、`node_modules/`、`dist/` 不入库。
