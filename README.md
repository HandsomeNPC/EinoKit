# EinoKit

> 基于 [Eino](https://github.com/cloudwego/eino) + Vue 3 + TDesign 的 Agentic 系统开发脚手架。

EinoKit 用一个"买奶茶"示例 Demo 端到端打通 Agent 的核心能力,作为后续业务 Agent 的起点:

- 🧠 **Thinking** — 模型推理思考过程流式展示
- 🔧 **Tool Call** — 工具调用与结果回填
- 🧑‍💻 **Human-in-the-Loop** — 中断 / 审批 / 恢复,刷新页面也能还原
- 🎴 **A2UI 卡片** — 工具调用按类型渲染自定义交互卡片
- 📊 **Callback 进度** — 运行过程实时进度反馈
- 💾 **订单落库** — 会话与消息持久化(GORM + PostgreSQL)

## 架构

```
EinoKit/
├── server/      # Go 后端 (Eino + ADK + AG-UI SDK)
│   └── internal/
│       ├── agents/      # Agent 注册表与 demo agent
│       ├── agui/        # AG-UI 事件发射器与历史渲染
│       ├── handler/     # gin handler (health / thread)
│       ├── memory/       # 会话与消息存储 (GORM)
│       ├── msgops/       # AgenticMessage 归一化/抽取
│       ├── turnloop/     # ADK TurnLoop 封装、中断映射
│       └── tools/        # 示例工具 (order_milk_tea)
└── web/         # Vue 3 + TS + Vite + TDesign 前端
```

前后端通过 **AG-UI 协议** 流式交互:前端发起 SSE 对话,后端 Eino Agent 运行并把事件流回传。

## 技术栈

- **后端**:Go + Eino(`github.com/cloudwego/eino` 含 `/adk`)+ AG-UI Go SDK;持久化走 PostgreSQL(GORM),checkpoint 走 Redis。
- **前端**:Vue 3 `<script setup>` + TypeScript + Vite + TDesign Vue Next + `@tdesign-vue-next/chat`;AG-UI 客户端走 ChatEngine + AGUIAdapter。
- **模型**:后端 Eino 的 Model 调用按业务自选 provider,脚手架不强绑定。

## 快速开始

### 后端

```powershell
cd server
go mod tidy
go run ./cmd           # 默认 :8080
```

### 前端

```powershell
cd web
npm install
npm run dev            # http://localhost:5173
```

开发期 `web/vite.config.ts` 会把 `/api/*` 代理到后端并 rewrite 掉前缀。

## HTTP 接口

| 方法 | 路径 | 说明 |
|------|------|------|
| GET  | `/health` | 健康检查 |
| POST | `/threads` | 创建会话 |
| GET  | `/threads` | 列出会话 |
| POST | `/threads/delete` | 删除会话 |
| POST | `/threads/chat` | 对话(SSE,AG-UI 事件流) |
| POST | `/threads/render` | 渲染会话历史 |
| POST | `/threads/abort` | 终止当前运行 |
| POST | `/threads/docs` | 上传文档 |

会话级接口统一 **POST + 请求体里的 `threadId`**,不用 URL 路径参数,SSE endpoint 是静态常量,前端切会话只改 body。

## 设计要点

- **常驻 loop + 抢占**:每个会话对应一个跨请求存活的 `adk.TurnLoop`,新消息到达时在安全点抢占当前轮开新一轮,上文不丢。
- **统一中断通道**:所有工具的中断走同一条通道,`RUN_FINISHED.outcome.interrupts[]` 同时落库到 `thread_info.pending_interrupt`;恢复时只表达"批准/拒绝",`interruptId` 由后端从快照取。新增需审批工具只需注册渲染组件,链路不用改。
- **checkpoint 权威**:Redis 里的 checkpoint 是唯一权威,`thread_info.pending_interrupt` 只是展示副本;checkpoint 过期则前端不显示中断卡片,避免点了就报错。

详见 [CLAUDE.md](./CLAUDE.md)。

## License

MIT
