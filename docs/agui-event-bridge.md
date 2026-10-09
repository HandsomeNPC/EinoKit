# AG-UI 事件桥接设计（OnAgentEvents + 中断存储）

本文记录 `server/internal/turnloop/on_agent_events.go` 的设计依据与待做改动。

文中结论分两类，请注意区分：

- **[实测]** —— 在本机读源码或抓真实 SSE 流验证过，可以直接当事实用。
- **[待验证]** —— 推断，尚未跑起来确认。

> 口径说明：TDesign 侧的行为**不能**凭文档或记忆推断。`@tdesign-vue-next/chat` 只是转发
> `@tdesign/ai-chat-engine`，真正的 adapter 源码嵌在后者的 sourcemap `sourcesContent[]` 里。
> 下面带文件行号的引用均出自那份原始 TS，而非反编译产物。

---

## 1. 最关键的事实：TDesign 没有"中断"这个概念

在 `@tdesign/ai-chat-engine`、`@tdesign/web-components-chat`、`@tdesign-vue-next/chat`
三个包里 grep `interrupt` / `outcome` / `needsApproval`：**零命中**。[实测]

`RunFinishedEventSchema`（`adapters/agui/types/events.ts:340-345`）只有：

```ts
{ type: 'RUN_FINISHED', threadId: string, runId: string, result?: ChatJSONValue }
```

没有 `outcome` 字段。`handleOtherEvent` 对 `RUN_FINISHED` 直接 `return null`。

**所以审批卡片不是由中断事件渲染的，而是由"没发某个事件"渲染的。**

渲染器的判定（`pro-components/chat/chat-engine/components/toolcall/render.tsx:103-126`）：

```
if (toolCall.result)                                        → complete
else if (eventType === TOOL_CALL_END || TOOL_CALL_RESULT)   → complete（但 result 为 undefined）
else                                                        → executing   ← 表单在这里渲染
```

### 结论：中断时要发 `TOOL_CALL_END`，唯独不发 `TOOL_CALL_RESULT`

这一条推翻了一个很自然的直觉（"中断时别发 END，否则卡片会变成已完成"）。

实测官方 HITL 示例（旅游规划 Agent，文档中标注 Human-in-the-Loop 的那个），
第一轮真实 SSE： [实测]

```
RUN_STARTED
TEXT_MESSAGE_START / CONTENT / END     mid=msg_1791542656811
TOOL_CALL_START   tcid=call_preferences_1791542656811  name=collect_user_preferences
TOOL_CALL_ARGS    delta="{\"destination\":\"北京\"}"
TOOL_CALL_END     tcid=call_preferences_1791542656811   ← 发了，表单照样正常渲染
RUN_FINISHED
```

对照第二轮里一个**真正完成**的工具： [实测]

```
TOOL_CALL_START → TOOL_CALL_ARGS → TOOL_CALL_END → TOOL_CALL_RESULT   ← 四件套才算完成
```

可见语义是：`TOOL_CALL_END` = "参数流结束"，`TOOL_CALL_RESULT` = "工具真正完成"。

好处：`END` 可以在模型的 tool-call 流结束时自然发出，不需要"延后到拿到结果"的缓冲逻辑。

> 注意 `web/src/components/tools/DemoToolCard.vue:4` 是 `v-if="status === 'complete' && result"`。
> 中断时走上面第二支，`status` 是 `'complete'` 但 `result` 为 `undefined`，所以落到 `v-else`
> 的表单分支。**这是官方契约，不是巧合**，但自定义工具组件若只判 `status` 就会出错。

---

## 2. adapter 的硬门与 `reset()`

两道门会**静默**丢弃事件（无任何日志）： [实测]

```ts
// adapters/agui/event-mapper.ts:567
handleToolCallArgs:   if (!this.toolCallMap[event.toolCallId]) return null;
// adapters/agui/event-mapper.ts:635
handleToolCallResult: if (!this.toolCallMap[event.toolCallId]) return null;
```

而 `stream-handlers/agui-stream-handler.ts:88` 的 `onRunStart` 会调
`aguiAdapter.reset()`，它清空 `toolCallMap`（`event-mapper.ts:160-162`）。

### 推论：想在 resume 流里更新"中断前那个 toolCallId"的卡片，就不能发 `RUN_STARTED`

否则 map 被清 → `TOOL_CALL_RESULT` 撞门 → 卡片永远翻不到"下单成功"。

省掉 `RUN_STARTED` 是安全的：`adapters/agui/index.ts:270-286` 是个裸 switch，
没有状态机，没有任何东西依赖它。省掉后 `processor/index.ts:134` 的跨消息合并
（只对 `toolcall-` 前缀生效）会把 result 并回原来那张卡片。

> `server/internal/handler/thread/chat.go:73` 当前在 resume 路径上调了
> `Emitter.RunStarted()`，按上面的分析这是个 bug。见 §6 待办。

### 官方示例是怎么绕开的

实测第二轮开头确实有 `RUN_STARTED`，但它紧接着只对**新**工具发 `TOOL_CALL_START`，
`collect_user_preferences` 那张卡**永远不会**收到 `TOOL_CALL_RESULT` —— 终态是前端
`render.tsx:61-65` 乐观写入的用户自己的 payload。

于是我们有两个选择：

| 方案 | 做法 | 代价 |
|---|---|---|
| A（官方同款） | resume 流**不发**该 toolCallId 的 `TOOL_CALL_RESULT`，订单结果靠模型文本复述 | 卡片停在用户提交的表单快照，不显示后端订单号 |
| **B（本项目采用）** | resume 流**不发** `RUN_STARTED`，然后发 `TOOL_CALL_RESULT` | 卡片能翻到"✓ 下单成功 + 订单号"，但偏离官方范式 |

选 B，因为 `DemoToolCard.vue:4-15` 明确写了 `result.orderId` 的成功态 UI，设计意图是 B。

**实现时必须注释清楚"此处故意不发 RUN_STARTED"**，否则后人很容易"顺手补上"而引入
一个极难排查的 bug（事件被静默丢弃，没有任何日志）。

---

## 3. 事件映射表

| Eino 事件 | 发出的 AG-UI 事件 |
|---|---|
| assistant 流 · reasoning block | `REASONING_START` → `REASONING_MESSAGE_START` → `*_CONTENT` → `*_MESSAGE_END` → `REASONING_END` |
| assistant 流 · 文本 | `TEXT_MESSAGE_START` / `CONTENT` / `END` |
| assistant 流 · tool call | `TOOL_CALL_START` → `TOOL_CALL_ARGS`* → `TOOL_CALL_END` |
| 工具结果 | `TOOL_CALL_RESULT` |
| `Action.Interrupted` | 该 tool call **不发** `RESULT`；发 `RUN_FINISHED` + interrupt outcome；返回哨兵 error |
| `Err` | `RUN_ERROR` |
| 迭代器耗尽 | `RUN_FINISHED`（success） |
| resume 轮 | **不发 `RUN_STARTED`**，直接发中断前 toolCallId 的 `TOOL_CALL_RESULT` |

`outcome.interrupts[]` 前端不解析，但它会原样到达 `onComplete` 的 raw chunk
（`RUN_FINISHED` 走的是 `JSON.parse` 原始对象，**先于** zod 校验，见
`adapters/agui/index.ts:219`），且仍需落库供 `/render` 回填 —— 所以照发。

### 容易踩的细节

- **`REASONING_START` 不开块**（`event-mapper.ts:298-302`），它只暗存 title。
  只发 `START` + `CONTENT` 则**什么都不渲染**，必须发完整五件套，且带稳定 `messageId`。
- **`TOOL_CALL_ARGS` 要发原始 JSON 片段**，不要每次发完整对象。
  `mergeStringContent`（`adapters/agui/utils.ts:25-40`）对两边都 parse 成功且都是 object 时
  做**展开合并**而非拼接。Eino 每个 chunk 的 `tc.Args` 本身就是片段（parse 会失败 →
  退化为字符串拼接），所以逐 chunk 转发是对的。单发一次完整 JSON 也安全。
- **`tc.ID` 可能晚于第一个 args 片段到达**，需按 `Index` 缓冲，等 id 已知再 flush `START`。
- `TOOL_CALL_RESULT` 的 `messageId` 是 **required**（即便 adapter 内部不用它），
  缺了整个事件会被 zod 丢弃。
- `TEXT_MESSAGE_CONTENT.delta` 为空字符串会被 zod 拒绝（`events.ts:158` 的 refine）。
  `Emitter.TextContent` 已经有空值短路，保持即可。

---

## 4. 中断数据怎么存

### 决定性事实：interrupt ID 是随机 UUID

`internal/core/interrupt.go:105,116` —— `ID: uuid.NewString()`。 [实测]

它与工具名、tool call ID **无任何可推导关系**；每次中断（含二次中断）都是新 UUID。
resume 时 `ResumeParams.Targets` 的 key 就是这个 UUID（`adk/runner.go:84`），
靠 checkpoint 里的 `InterruptID2Address` 映射回地址（`adk/interrupt.go:219`）。

### 为什么 `thread_info.pending_interrupt` 不能删

因为 checkpoint **读不出来**：`serialization`（`adk/interrupt.go:210`）是
**小写未导出**的 gob 结构。 [实测]

checkpoint 能回答的只有"存在/不存在"（`store.Get` 的 `existed`）。
里面的 UUID 和 `Info` 文案取不出来。而：

- 前端 `web/src/api/types.ts:47` 要 `interrupts: AGUIInterrupt[]`（含 `id`/`message`/`toolCallId`）；
- **更要紧**：resume 时后端要从快照取 `interruptId` 填 `Targets`。CLAUDE.md 约定
  ID 不由前端传（防过期/伪造），删掉这个字段就只能让前端传，违背该约定。

曾考虑的替代方案：把"待审批"由历史推导（扫 `thread_message`，找有 `toolCalls[].id`
但无对应 `FunctionToolResult` 的那条）。可行，但**UUID 仍然无处可存** —— 绕回原点，
且丢失 message 文案。**不采用。**

### 三层职责（现有设计正确）

| 层 | 存什么 | 为何不可合并 |
|---|---|---|
| Redis checkpoint | `InterruptID2Address` + `InterruptID2State` | ADK 自己写的 gob 内部结构，**只有它能 resume** |
| `thread_info.pending_interrupt` | `[]types.Interrupt` | 纯展示/回填副本。有 TTL 风险，故必须以 checkpoint 为准 |
| `thread_message` | tool call + tool result | 审批卡片的**壳**从这里恢复（`convertHistoryMessages` 按 `toolCalls[].id` ↔ `tool.toolCallId` 匹配） |

第三层容易被忽略：刷新后卡片本身由 `/render` 的历史渲染，`interrupts[]` 只负责
告知"这张卡还在等审批"。两者缺一不可。

### 真正多余的是 `PendingInterruptItem` 这个类型

三方对比：

| 定义处 | 形态 |
|---|---|
| `memory/thread/thread.go:17-21` | `PendingInterruptItem{InterruptID, InterruptInfo, ToolCallID}` |
| `memory/thread/store.go:79` 注释 | 「payload 为 AG-UI `[]types.Interrupt` 的 JSON 数组」 |
| `web/src/api/types.ts:60-69` | `AGUIInterrupt{id, reason, message, toolCallId, …}` |

注释与前端都是 `types.Interrupt`，只有 Go 类型是自定义的，字段名还对不上
（`InterruptInfo` ↔ `message`，`InterruptID` ↔ `id`）。

换成 `[]types.Interrupt` 后一份数据三处零转换复用：`RunFinishedInterrupt(items)`
直接传、`/render` 原样输出、前端类型天然匹配。

### 构造映射

```go
types.Interrupt{
    ID:         ic.ID,                       // uuid
    Reason:     "tool_call",
    Message:    fmt.Sprintf("%v", ic.Info),  // demo_tool 传的是 string
    ToolCallID: toolCallIDOf(ic.Address),    // tool 段的 SubID
}
```

`toolCallIDOf`：遍历 `InterruptCtx.Address` 找 `Type == "tool"` 的段取 **`SubID`**。
依据 `compose/tool_node.go:1009` → `appendToolAddressSegment(ctx, task.name, task.callID)`，
签名是 `(ctx, segID, subID)`，故 `ID` = 工具名、`SubID` = call ID。 [实测]

优先取 `IsRootCause` 的 context，取不到退回 `[0]`。

### 两条铁律

1. **覆盖写，绝不追加。** `tools/demo_tool.go:70-73` 会二次中断并产生**全新 UUID**；
   若追加，前端会同时看到死 ID 和活 ID，点哪个都可能报错。每轮中断整组覆盖。
2. **所有收尾路径都要清。** 正常结束、出错、`/abort` 三条路。
   `ClearPendingInterrupt`（`store.go:99-102`）设计成"行不存在也返回 nil"的幂等语义，
   正适合此用法。

### 不建议单独建表

它是**副本而非源数据**，且同一时刻一个 thread 最多一组待确认中断（checkpoint 就一份）。
无需索引/分页/历史版本，jsonb 数组已能表达并行多中断。单独建表只多一次 join 和一致性负担。

**一个例外值得将来做**：审批审计日志（谁、何时、批准/拒绝了什么、payload 是什么）。
那是 append-only 的合规需求，与"当前待确认状态"是两种不同的数据，应独立建表 ——
但别与 `pending_interrupt` 混在一起。

**审批结果不要存进 `pending_interrupt`**，它应作为工具结果进 `thread_message`，
这样历史天然完整，`/render` 也能渲染"✓ 下单成功"的终态。

---

## 5. 事件该在哪里发：骨架归 OnAgentEvents，血肉归 Tool

曾讨论"是否全部在 Tool 内部用 Emitter 手动发事件"。结论是**分两类**。

### 必须在 Tool 内部发：`ACTIVITY_*` / `STATE_*`

Eino 的事件流**根本没有**对应物 —— 它只给 assistant 消息流、工具结果、Action。
而这些事件表达的是"工具执行到第 3 步了"这种中途状态，在 `OnAgentEvents` 里拿不到
（工具还在跑，`FunctionToolResult` 尚未产生）。

实测官方综合示例第二轮印证：`show_planning_steps` 在 `TOOL_CALL_ARGS` 之后、
`TOOL_CALL_END` 之前插了 **6 个 `STATE_DELTA`**。 [实测]
该序列只能从工具内部发出，没有替代路径。

### 不要搬进 Tool：`TOOL_CALL_START` / `ARGS` / `END`

1. **流式效果会丢。** 工具被调用时参数已完整反序列化（`demoToolInvoke(ctx, args *demoArgs)`），
   只能发一次完整 JSON。模型逐 token 吐参数的过程发生在工具执行**之前**，
   只有 `OnAgentEvents` 的 assistant 流能看到。
2. **中断的工具永远发不出 `START`。** `tools/demo_tool.go:61-62` 首次调用即中断返回。
   放在该行之后永不执行；放在之前则恢复时**再发一次** `START`，而
   `handleToolCallStart` 用 `strategy: 'append'`（`utils.ts:151-157`）—— 会**多渲染一张卡片**。
3. **并发写 SSE 会撕裂数据帧。** `compose/tool_node.go:1100-1115` 的 `parallelRunToolCall`
   对多个 tool call 真的开 goroutine。[实测] 而 `SSEWriter` **无写锁**
   （`pkg/encoding/sse/writer.go`，第 204 行的 mutex 属于另一个结构体）[实测]，
   `Emitter.write` 也没有锁 → 两个 `data: {...}\n\n` 交错写入同一个 `c.Writer`，
   产出无法解析的帧。`OnAgentEvents` 是单 goroutine 顺序消费，天然无此问题。
4. **工具会被迫依赖 Emitter。** `Demo` 现在是 `utils.InferTool` 推导的纯函数，
   可单测、可复用；要发事件就得把 emitter 透过 ctx 传入，从此耦合 AG-UI/gin。

### 分工表

| 事件 | 发出位置 |
|---|---|
| `TEXT_*` / `REASONING_*` | `OnAgentEvents` |
| `TOOL_CALL_START` / `ARGS` / `END` / `RESULT` | `OnAgentEvents` |
| `ACTIVITY_*` / `STATE_*` | **Tool 内部** |
| `RUN_*` | `OnAgentEvents` |

### 为此需要补的基础设施

1. **给 `Emitter.write` 加 `sync.Mutex`。** 既然允许多 goroutine 写，这是必需的，
   否则就是数据竞争。三行改动。
2. **用 `GenInputResult.RunCtx` 透传 emitter。** （`adk/turn_loop.go:636`）
   在 `GenInput` 里把本轮 emitter 塞进 `RunCtx`，工具内用 `agui.FromContext(ctx)` 取。
   这顺带解决了 §6 的 emitter 绑定问题，因为 `RunCtx` 每轮重建。
3. 工具内取 toolCallId 用 `compose.GetToolCallID(ctx)`（`compose/tool_node.go:1477`）[实测]，
   使 `STATE_SNAPSHOT` 的 key 能与前端 `subscribeKey: (props) => props.args?.taskId` 对齐。

---

## 6. 实施记录

§1–§5 的设计已落地。本节记录实现形态与仍未验证的部分。

### 已完成

**存储层**
- `memory/thread/thread.go`：`PendingInterrupt` 改为 `[]types.Interrupt`，
  删除 `PendingInterruptItem`，字段注释写明"它是副本，checkpoint 才是权威"。
- `memory/thread/store.go`：`SavePendingInterrupt` 改收 `[]types.Interrupt`，
  序列化交给列上的 `serializer:json`（规避 `[]byte` 可能被双重编码成 base64）。

**基础设施**
- `agui/emitter.go`：`write` / `ToolArgsAllowEmpty` 加 `sync.Mutex`；
  `Err()` / `EncErr()` 一并加锁。顺带修了 `ToolArgsAllowEmpty` 的传输错误分支
  原先不记录 `e.err` 导致连接断开后不熔断的问题。
- `agui/context.go`（新增）：`WithEmitter` / `FromContext`，供工具内部发
  `STATE_*` / `ACTIVITY_*`。

**Turn 管道**（解决两个阻塞项）
- `turnloop/turn.go`（新增）：`Turn{Seq, Emitter, ThreadID, done}`，
  `Finish()` 幂等，`turnOf(items)` 取 `Seq` 最大者。
- `turnloop/chat_item.go`：加**非导出** `turn` 字段 —— gob 忽略非导出字段，
  于是从 checkpoint 恢复的条目不会带着已关闭的 SSE 流回来（有测试固定此约定）。
- `turnloop/turnloop.go`：`ThreadContext` 只留 `ThreadID`，emitter 不再闭包进构造期。
- `turnloop/gen_input.go` / `gen_resume.go`：通过 `GenInputResult.RunCtx` 注入
  本轮 emitter。
- `handler/thread/chat.go`：重写。handler 阻塞在 `turn.Done()`，
  同时 select 客户端断开与 10 分钟兜底超时。

**事件桥接**
- `turnloop/on_agent_events.go`：按 §3 映射表实现。`turnStream` 持有单轮状态；
  `TOOL_CALL_END` 与 `RESULT` 一起在工具结果到达时才发（中断路径天然两个都不发）；
  tool call 参数按 `Index` 聚合、等 ID 到齐才发 `START`；reasoning 发完整五件套。
- `turnloop/interrupt.go`（新增）：`toolCallIDOf`（取 tool 段 `SubID`）、
  `interruptsOf`、`loadPendingInterrupts`（先查 checkpoint）、
  `PendingInterrupts` / `IsRunning` 两个导出查询入口。
- `msgops/extract.go`：新增 `ReasoningText`（只取明文，不外传 Signature）。

**已修的 bug**
- `chat.go`：resume 路径不再发 `RUN_STARTED`（§2）。
- `chat.go`：Push 返回 `false`（loop 已死）时摘登记重建，而非徒劳重试；
  新增 `watchLoopExit` 在 loop 退出后异步清理登记。
- `turnloop_holder.go`：新增 `Delete` / `DeleteIf`。
- `abort.go`：清 `pending_interrupt` + 摘 loop 登记；无运行中 loop 时也清理。
- `tools/demo_tool.go`：**原先忽略 `ResumeEntry.Status`，用户点"取消"也会下单成功**。
  现在 `ResumeStatusCancelled` 返回 `Cancelled: true`。
  （这个洞此前不可达，是 Turn 管道让 status 可靠投递后才暴露的。）
- `web/src/components/ChatPanel.vue`：删除会话的 `filter` 条件写反了
  （`=== threadId` 只留下被删项、清空其余），改为 `!==`。

### 坑：`[]types.Interrupt` 不能直接交给 GORM 的 `Update`

```go
// ✗ 静默失败：列里存成单个 JSON object，或根本没写进去
Update("pending_interrupt", items)

// ✓ 显式 marshal
payload, _ := json.Marshal(items)
Update("pending_interrupt", string(payload))
```

`Update(column, value)` 走的是**列路径**，字段上的 `serializer:json` 不参与；
而 GORM 的参数绑定会把切片当变参列表展开，单元素切片被拆成其首个元素。
结果列里是 `{...}` 而不是 `[{...}]`，**写入时不报错**，直到读取阶段才炸：

```
json: cannot unmarshal object into Go value of type []types.Interrupt
```

更糟的是它炸在 `SelectList`（`/threads` 列表接口）上 —— 一条坏数据会让整个
会话列表 500，而症状看起来跟中断功能毫无关系。

实测对比（2026-10-09，GORM v1.31.2 + PostgreSQL）：

| 写法 | 结果 |
|---|---|
| `Update("col", items)` | 静默失败，列值未更新 |
| `Update("col", string(json))` | ✓ 正确存为数组 |
| `Select("col").Updates(&ThreadInfo{...})` | ✓ 正确存为数组（走字段路径，serializer 生效） |

采用第二种：不依赖 GORM 对字段路径的推导，意图更直白。

> 此前本文档把风险猜成"`[]byte` 被双重编码成 base64"，**猜错了**。
> 真实原因是切片展开，且和 `[]byte` 无关。

### 验证状态

| 项目 | 结果 |
|---|---|
| `go build ./...` | 通过 |
| `go vet ./...` | 通过 |
| `gofmt` | 无差异（仓库为 CRLF 行尾，`gofmt -l` 会整体列出，内容实际一致） |
| `go test ./...` | 通过（turnloop 6 个新测试 + msgops 既有测试） |
| `go test -race` | **未跑** —— 本机无 C 编译器（cgo 需要 `cl.exe`） |
| `vue-tsc` | 有 1 个**既有**错误（`ChatPanel.vue:29` 的 `AttachmentContent` 类型不匹配），
  已用 `git stash` 确认改动前就存在 |

新增测试固定的约定：`toolCallIDOf` 取 `SubID` 而非 `ID`、`turnOf` 取最新轮、
`Finish` 幂等（20 goroutine 并发）、nil `Turn` 各方法安全、
**`ChatItem` 的 `turn` 不被 gob 持久化**。

### 仍未验证（需要跑起来）

- [x] ~~`SavePendingInterrupt` 的 `serializer:json` 行为~~ ——
      已实测，见上方"坑"小节。**猜测的原因是错的，真实原因是切片展开。**
- [ ] **完整端到端未跑通**：已验证到"提问 → 工具调用 → 中断落库"
      （见下方实跑日志），中断之后的**审批 → 恢复 → 落库**尚未走通。
- [ ] 抢占路径：`tc.Preempted` 的检查点放在事件循环开头，被抢占的轮次能否
      确实跳过 `RUN_FINISHED`。
- [ ] `gen_resume` 里"按序号配对 pending 与前端回传决定"的假设 ——
      并行多工具中断时顺序是否稳定。单中断（当前 demo 唯一场景）不受影响。
- [ ] 刷新页面后 resume 的降级行为是否如 §7 所述。

### 已实跑验证的部分

2026-10-09 实跑日志确认下列环节正常：

```
[genInput] query="我想喝芋泥波波奶茶" messages=2
[events]   assistant 流: 正文 265 字, reasoning 0 字, tool call 0 个
[genInput] query="大杯，无糖，1杯" messages=4
[events]   assistant 流: 正文 86 字, reasoning 0 字, tool call 1 个
[events]   中断 id=5628... toolCallID=call_e613... msg="确认下单 1 杯芋泥波波（大杯 / 无糖），合计 18.00 元？"
[loop]     退出: turnloop: agent 等待人工确认
```

即：handler 阻塞到本轮收尾（请求耗时 2.47s / 2.58s 而非立即返回）、
多轮历史累积（messages 2 → 4）、tool call 解析、中断上下文与 `toolCallID` 提取
（`SubID` 取法正确）、`ErrInterrupted` 哨兵让 loop 带 ExitReason 退出以保留 checkpoint。

### 前端布局坑：`.t-chat` 的父级必须是 flex 容器

现象：聊天区没有滚动条，新消息也不自动滚到底部。

原因：`.t-chat` 自身是 `display:flex; flex-direction:column; flex:1 1 0%`，
靠**父级是 flex 容器**撑出确定高度；真正的滚动容器 `.t-chat__list` 才拿得到
`flex:1` + `overflow-y:auto`。父级若是普通 block，`.t-chat` 高度退化为内容高度，
于是 `scrollHeight === clientHeight` —— 既没有滚动条，组件内部的
`scrollToBottom`（`chat-list.tsx:168`）也变成空操作。

`autoScroll` 与 `defaultScrollTo` 的默认值本来就是 `true` / `'bottom'`，
**不需要显式传**，问题纯在 CSS。修法（`ChatPanel.vue`）：

```css
.panel__body {
  flex: 1;
  min-height: 0;        /* 否则 flex 子项不收缩，长列表顶出输入框 */
  display: flex;        /* ← 关键 */
  flex-direction: column;
  padding: 16px;
}
```

原先这里是 `overflow: hidden` 且没有 `display:flex`，超出的消息被直接裁掉。

### 下一步建议

1. 起 PostgreSQL + Redis，跑一遍买奶茶全流程，对着浏览器 Network 面板核对事件序列。
2. 在 `Demo` 工具里加一条 `STATE_SNAPSHOT` + `STATE_DELTA`，验证 §5 的
   "血肉归 Tool"路径真的通（前端只需给 `DemoToolCard` 加 `subscribeKey`）。
3. `render.go` 的 `interrupts` 回填与前端 `RenderResponse` 对齐后，
   验证刷新页面能恢复审批卡片。

---

## 7. 已知可接受的降级

**刷新页面后 resume，`TOOL_CALL_RESULT` 会被丢弃。**

`/render` → `convertHistoryMessages` 把内容块直接写进 message store，
**从不填充** adapter 的 `toolCallMap`。于是刷新后的 resume 流里，
`TOOL_CALL_RESULT` 撞上 §2 的硬门被静默丢弃。

卡片看起来仍正常，因为 `render.tsx:61-65` 乐观地写入
`status:'complete', result: <用户自己的 payload>`，加上 agent instruction
会让模型用文本复述订单号。

重发一次 `TOOL_CALL_START` 能修好数据链路，但 `strategy: 'append'` 会渲染出
**第二张**卡片 —— 反而更糟。**接受此降级**，不绕着它做工程。

> 另一处相关细节：`convertHistoryMessages` 调 `createToolCallContent` 时
> **不传 status 参数**，故默认 `'pending'` 且 `eventType` 为 `undefined`。
> 在渲染器的 else 链里，这意味着 `result` 为空时落到 `'executing'` ——
> 所以从 `/render` 恢复的未答复 tool call 能**正确重现审批表单**。
> 这依赖 `eventType` 的缺席，**不要给它补上**。

---

## 附：参考位置速查

**TDesign adapter 原始 TS**（嵌于 sourcemap，非反编译）：

```
web/node_modules/@tdesign/ai-chat-engine/dist/index.mjs.map
  → JSON.parse(...).sourcesContent[N]
```

| N | 路径 |
|---|---|
| 212 | `adapters/agui/utils.ts` |
| 214 | `adapters/agui/types/schema.ts` |
| 215 | `adapters/agui/types/events.ts` |
| 218 | `adapters/agui/event-mapper.ts` |
| 219 | `adapters/agui/index.ts` |
| 220 | `stream-handlers/agui-stream-handler.ts` |
| 203 | `processor/index.ts` |
| 234 | `index.ts`（`ChatEngine`） |

**工具卡片渲染器**：

```
web/node_modules/@tdesign-vue-next/chat/es/_chunks/dep-416e8288.mjs.map
  → sourcesContent[1] = pro-components/chat/chat-engine/components/toolcall/render.tsx
```

**官方 HITL 示例**（可抓真实 SSE 流复核）：

- 文档：`https://tdesign.tencent.com/vue-next-chat/components/chat-engine`
  的 `agui-comprehensive` demo（源码在该 `td-doc-demo` 元素的 `data-javascript` 属性里）
- 端点：`POST https://1257786608-9i9j1kpa67.ap-guangzhou.tencentscf.com/sse/travel-planner`
  - 第一轮：`{"prompt":"请为我规划一个北京3日游行程"}`
  - 第二轮：`{"toolCallMessage":{"toolCallId":"<上轮的>","toolCallName":"collect_user_preferences","result":"{...}"}}`

> 官方示例的 HITL 回传用的是 `sendAIMessage` + `toolCallMessage`，**不是** `resumeRun`。
> 本项目用 `resumeRun` + `resume: []types.ResumeEntry`（`ChatPanel.vue:133`），
> 符合 AG-UI 的 `ResumeEntry` 规范，且"interruptId 不由前端传"的安全约定更好。
> **保留本项目做法。** 可选改进：让前端把 `toolCallId` 一并带回，
> 后端用它交叉校验快照，比纯靠"取唯一那条"更稳。
