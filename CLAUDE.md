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

## 技术栈基线

- **后端**:Go + Eino (`github.com/cloudwego/eino` 含 `/adk` 子包) + AG-UI SDK。AG-UI Go SDK 用 go.mod `replace` 指向本地 `E:/WorkSpace/ag-ui-main/sdks/community/go`(本地开发期,提交前确认是否还需 replace)。Store:PostgreSQL 生产 / 本地原型用 JSONL 文件(`internal/msgops` + `mem` 包)。
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
