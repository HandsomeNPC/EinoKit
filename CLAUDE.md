# CLAUDE.md — EinoKit

EinoKit 是一个 AI 脚手架项目:后端基于 [Eino](https://github.com/cloudwego/eino)(CloudWeGo 的 LLM 应用框架,含 ADK),前端基于 Vue 3 + TDesign,通过 AG-UI 协议与后端流式交互。目标是用"买奶茶"等示例 Demo 端到端打通 Thinking / Tool Call / Human-in-the-Loop / A2UI 卡片 / Callback 进度 / 订单落库 等能力,作为后续业务 Agent 的起点。

## 仓库结构

```
EinoKit/
├── server/      # Go 后端 (Eino + ADK + AG-UI SDK),目前为空,待初始化 go mod
├── web/         # Vue 3 + TS + Vite 前端,待引入 TDesign
└── CLAUDE.md
```

> 当前为空脚手架,无任何提交。下面的命令与约定是**目标基线**,初始化时按此执行。

## 开发命令

### 前端 (`web/`)
```powershell
cd web
npm install        # 安装依赖(需补装:tdesign-vue-next、tdesign-icons-vue-next)
npm run dev       # vite dev,默认 http://localhost:5173
npm run build     # vue-tsc -b && vite build
npm run preview
```

### 后端 (`server/`)
```powershell
cd server
go mod init github.com/Platform/EinoKit      # 初始化(路径以实际为准)
go mod tidy
go run ./cmd/server                            # 或 make run,默认 :8080
go test ./...
```

## 关键约定(用户明确要求,勿违背)

### 1. 后端接口**不带 `/api` 前缀**
所有 HTTP 路由直接挂在根组下:`/auth/*`、`/agent/session/*`、`/agent/run`、`/agent/resume`、`/healthz`、`/readyz`。**不要** `r.Group("/api")`。

- 前端 `web/src/api/*.ts` 的调用路径与后端一一对应(`/auth/*`、`/agent/*`)。
- `web/vite.config.ts` 的 dev proxy 是 `/auth` 与 `/agent` → `http://localhost:8080`(不是 `/api`)。
- 若未来要改回 `/api` 前缀,需同步改:后端路由 + 前端 api/*.ts + vite proxy + App.vue 的 `chatServiceConfig.endpoint`。

### 2. AG-UI / Eino ADK 集成的非显然坑(都是踩过的,勿重复)

- **ADK interrupt 必须注册 gob 类型**:`compose.Interrupt(ctx, info)` 的 info 经 `encoding/gob` 序列化进 checkpoint,自定义 info 结构体(含嵌套如 `[]MyDTO`)必须在 `init()` 里 `gob.Register(MyInfo{})`。否则报 `failed to save checkpoint: gob: type not registered for interface`,Run 以 RUN_ERROR 结束(看起来像"客户端断连",其实是 checkpoint 序列化失败)。
- **REASONING_MESSAGE_* 必须用独立 messageId**,不能复用 TEXT_MESSAGE 的 msgID:同 id 的 REASONING_CONTENT 会让前端 ChatEngine AGUIAdapter 异常关闭 SSE,后端后续写全 `context cancelled`。后端 Emitter 给 reasoning 用独立 `reasoningID`。
- **Emitter 必须用 `sync.Mutex` 串行化 `write()`**:ADK Callback 在 Runner 的 goroutine 里触发,与 Run() 主循环并发写同一 SSEWriter,不加锁会帧损坏 → 客户端断连。
- **两类 InferTool 的中断机制不通用**:`utils.InferOptionableTool` 走 `compose.NewInterruptAndRerunErr`;`utils.InferTool` 走 `compose.Interrupt` + `compose.GetResumeContext[T](ctx)` 取确认值。选错会不工作。
- **ADK Runner 在 `github.com/cloudwego/eino/adk` 子包**(eino v0.9.20+),不是 eino-ext/adk,无需额外依赖。CheckPointStore 接口仅 Get/Set,自写 `sync.Map` 实现即可。`EnableStreaming:false` 可简化事件映射(每步完整 message,免流式 tool-call delta 解析)。
- **resume 走独立 SSE fetch**(走 `/agent/resume`),**不**经 ChatEngine.sendUserMessage。前端用 fetch + ReadableStream 解析 AG-UI 帧:CUSTOM(a2ui) → A2UI surface / 直接渲染;TEXT → `chatEngine.sendAIMessage({content, sendRequest:false})` 追加。
- **无 PostgreSQL 时** `internal/store` 自动降级到本地 SQLite(`server/einoit.db`,已 gitignore),便于本地起跑。

### 3. 前端 ChatEngine 约定
- ChatEngine 的 content 块 data 经 immer 冻结(只读),**不能** `block.data.result = x` 直接赋值(会 throw "Cannot assign to read only property")。要更新块状态须**不可变替换**整个 content 数组:`m.content = m.content.map(b => match ? {...b, data: {...b.data, result, eventType}} : b)`。
- ToolCallBlock 状态按 `data.eventType` 推进(START/ARGS=调用中, END=执行中, RESULT=完成),**不要只看 `result`**——interrupt 的工具无 result,会卡"调用中"。
- TDesign chat 系列组件注意:`t-chat-thinking` / `t-chat-markdown` / `t-chat-loading` 是 Vue 组件,渲染成内部 tag 而非 `<t-chat-xxx>` 原标签,`querySelector` 找不到;`t-chat-thinking` 的 `content` prop 是 TNode,传 String 会 warn 且不渲染,用默认 slot `<t-chat-thinking>{{text}}</t-chat-thinking>`;omi `t-chat-md-content` 在 slot 内不渲染文本,text/markdown 块用原生 `<div class="msg-text">{{ b.data }}</div>` 或 Vue `ChatMarkdown`。`ChatItem` 的 `#content` slot 可用(`renderTNodeJSX("content",{slotFirst:true})`),可放自建块组件。

## 技术栈基线

- **后端**:Go + Eino (`github.com/cloudwego/eino` 含 `/adk` 子包) + AG-UI SDK。AG-UI Go SDK 用 go.mod `replace` 指向本地 `E:/WorkSpace/ag-ui-main/sdks/community/go`(本地开发期,提交前确认是否还需 replace)。Store:PostgreSQL 生产 / SQLite 本地降级。
- **前端**:Vue 3 `<script setup>` + TypeScript + Vite + TDesign Vue Next (`tdesign-vue-next`)。AG-UI 客户端走 ChatEngine + AGUIAdapter。
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

- 主分支 `main`,开发在 `master` 或特性分支。提交前不要 `--no-verify` 跳 hook。
- 远端:`codeup.aliyun.com/.../Platform/EinoKit.git`。
- `server/einoit.db`、`.idea/`、`node_modules/`、`dist/` 不入库。
