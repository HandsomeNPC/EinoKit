package turnloop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"

	"github.com/HandsomeNPC/EinoKit/internal/agui"
	"github.com/HandsomeNPC/EinoKit/internal/memory/thread"
	"github.com/HandsomeNPC/EinoKit/internal/msgops"
)

var errInterrupted = errors.New("agent interrupted for approval")

// makeOnAgentEvents 返回 OnAgentEvents 回调。
func makeOnAgentEvents(threadID string) func(ctx context.Context, tc *adk.TurnContext[*ChatItem, *schema.AgenticMessage], evIter *adk.AsyncIterator[*adk.TypedAgentEvent[*schema.AgenticMessage]]) error {
	return func(ctx context.Context, tc *adk.TurnContext[*ChatItem, *schema.AgenticMessage], evIter *adk.AsyncIterator[*adk.TypedAgentEvent[*schema.AgenticMessage]]) error {
		// 本轮归属哪次 HTTP 请求：决定事件往哪条 SSE 流写、消息用哪个 runID
		// 落库。loop 是常驻的，这些不能在构造期绑定。
		turn := turnOf(tc.Consumed)
		if turn == nil {
			// 条目不带 Turn —— 只可能是从 checkpoint 恢复出来的旧条目，
			// 其 SSE 连接早断了。照常消费事件把消息落库，但不写 UI。
			log.Printf("warn: 本轮没有关联请求，仅落库不推送 thread=%s", threadID)
		}
		runID := turnRunID(turn)
		emitter := turnEmitter(turn)

		// 本轮结束时放行对应请求（被抢占的情况在下面单独处理）。
		defer func() {
			if turn != nil && !turn.Preempted() {
				turn.completeOne()
			}
		}()

		// writerBroken 在 SSE 写入失败(浏览器关页/断网)时置位。置位后
		// 停止向 UI 写,但继续消费事件、累积消息,以保住后台任务。
		// 没有 emitter（孤儿轮次）时直接视为已断。
		writerBroken := emitter == nil
		markBroken := func() {
			if !writerBroken && emitter.Err() != nil {
				writerBroken = true
				log.Printf("warn: SSE broken, continuing for accumulation thread=%s run=%s: %v", threadID, runID, emitter.Err())
			}
		}

		// pending 攒本轮要落库的中间消息,按事件到达顺序排列。。
		var pending []*schema.AgenticMessage

		for {
			event, ok := evIter.Next()
			if !ok {
				// 事件流正常耗尽(迭代器关闭)。
				break
			}

			// 0) 被后来的请求抢占:框架已在安全点取消 agent。本轮的已产出内容
			// 照常落库,但不发 RUN_FINISHED —— 收尾归抢占者那一轮。
			select {
			case <-tc.Preempted:
				flushPending(threadID, runID, &pending)
				if turn != nil {
					turn.markPreempted()
				}
				log.Printf("[turnloop] 本轮被新消息抢占 thread=%s run=%s", threadID, runID)
				return nil
			default:
			}

			// 1) 错误事件:发 RUN_ERROR,flush 已累积的消息,返回 err。
			if event.Err != nil {
				if !writerBroken {
					emitter.RunError(event.Err.Error())
					markBroken()
				}
				flushPending(threadID, runID, &pending)
				clearPendingInterrupt(threadID)
				return event.Err
			}

			// 2) 中断事件:发 RUN_FINISHED(interrupt),flush(必须把触发中断的
			// tool_call 落库,否则 resume 后历史缺这条),返回 errInterrupted。
			if event.Action != nil && event.Action.Interrupted != nil {
				interrupts := toAGUIInterrupts(event.Action.Interrupted.InterruptContexts)
				if !writerBroken {
					emitter.RunFinishedInterrupt(interrupts)
					markBroken()
				}
				flushPending(threadID, runID, &pending)
				// 落库中断详情:前端刷新后靠它恢复审批卡片,resume 时后端也
				// 靠它查出 interruptId,不信任客户端传入的值。
				savePendingInterrupt(threadID, interrupts)
				return errInterrupted
			}

			// 3) 消息输出:assistant 文本/工具调用,或 tool 结果。只累积,不立即落库。
			if event.Output != nil && event.Output.MessageOutput != nil {
				mo := event.Output.MessageOutput

				if msgops.VariantIsToolResult(mo) {
					// 工具结果:DrainToolResult 抽出 content/callID/name。
					content, callID, name := msgops.DrainToolResult(mo)
					if !writerBroken {
						msgID := events.GenerateMessageID()
						emitter.ToolResult(msgID, callID, content)
						markBroken()
					}
					pending = append(pending, msgops.NewToolResult[*schema.AgenticMessage](callID, name, content))
					// tool result 之后可能紧跟 Exit,落到下面统一处理。
				} else if mo.IsStreaming && mo.MessageStream != nil {
					// 流式 assistant 输出:边收 chunk 边发 TEXT/TOOL_CALL 事件,结束后合并累积。
					handleStreamingMessage(threadID, runID, emitter, mo, markBroken, &pending)
				} else if !msgops.IsNil(mo.Message) {
					// 非流式 assistant 消息:整条文本/工具调用一次性发出 + 累积。
					handleRegularMessage(emitter, mo.Message, markBroken, &pending)
				}
			}

			// 4) 退出动作:agent 本轮完成。break 后由统一收尾发 RUN_FINISHED(success)。
			if event.Action != nil && event.Action.Exit {
				break
			}
		}
		flushPending(threadID, runID, &pending)
		// 跑到这里说明本轮没有中断(正常 Exit 或事件流耗尽),清掉上一轮可能
		// 残留的快照——resume 成功收尾走的正是这条路径。
		clearPendingInterrupt(threadID)
		// 抢占可能恰好在最后一个事件后到达,此时本轮其实已经跑完,
		// 照常发 RUN_FINISHED,但请求的放行仍归 markPreempted。
		if !writerBroken {
			emitter.RunFinishedSuccess()
			markBroken()
		}
		return nil
	}
}

// turnRunID / turnEmitter 兜住 turn 为 nil 的孤儿轮次（条目从 checkpoint
// 恢复、原请求早已结束）。
func turnRunID(t *Turn) string {
	if t == nil {
		return "orphan-" + uuid.New().String()
	}
	return t.RunID()
}

func turnEmitter(t *Turn) *agui.Emitter {
	if t == nil {
		return nil
	}
	return t.Emitter()
}

// savePendingInterrupt 把本轮产生的中断落库,供 /render 恢复审批卡片与
// resume 时反查 interruptId。失败只记日志:中断本身已经通过 SSE 发给前端,
// 落库失败最多让"刷新后恢复"这一个增强功能失效,不该让整轮对话失败。
func savePendingInterrupt(threadID string, interrupts []types.Interrupt) {
	if len(interrupts) == 0 {
		return
	}
	payload, err := json.Marshal(interrupts)
	if err != nil {
		log.Printf("warn: marshal pending interrupt failed thread=%s: %v", threadID, err)
		return
	}
	if err := thread.SavePendingInterrupt(threadID, payload); err != nil {
		log.Printf("warn: persist pending interrupt failed thread=%s: %v", threadID, err)
	}
}

// clearPendingInterrupt 清掉会话的中断快照。在所有"非中断"的收尾路径调用,
// 保证快照不会比它描述的那次中断活得更久。
func clearPendingInterrupt(threadID string) {
	if err := thread.ClearPendingInterrupt(threadID); err != nil {
		log.Printf("warn: clear pending interrupt failed thread=%s: %v", threadID, err)
	}
}

// flushPending 把 pending 整批归一化后一次性追加到 (thread_id, run_id) 行。
// 调用后清空 pending,使后续 flush 成为 no-op(防止重复落库)。提取为包级
// 函数后,makeOnAgentEvents 闭包不再持有 flush 闭包,各返回点直接传 &pending。
func flushPending(threadID, runID string, pending *[]*schema.AgenticMessage) {
	if len(*pending) == 0 {
		return
	}
	payloads := make([]string, 0, len(*pending))
	for _, msg := range *pending {
		normalized := msgops.NormalizeForSession(msg)
		payload, mErr := json.Marshal(normalized)
		if mErr != nil {
			log.Printf("warn: marshal intermediate message failed thread=%s run=%s: %v", threadID, runID, mErr)
			continue
		}
		payloads = append(payloads, string(payload))
	}
	*pending = nil
	if len(payloads) == 0 {
		return
	}
	if aErr := thread.AppendMessages(threadID, runID, payloads); aErr != nil {
		log.Printf("warn: persist intermediate batch failed thread=%s run=%s: %v", threadID, runID, aErr)
	}
}

// handleStreamingMessage 消费一条流式 assistant 输出。逐 chunk 发
// TEXT_MESSAGE_START/CONTENT/END 与 TOOL_CALL_START/ARGS/END,流结束后
// 用 msgops.ConcatChunks 合并为完整消息并累积(SSE 断了也照常合并 + 累积)。
func handleStreamingMessage(
	threadID, runID string,
	emitter *agui.Emitter,
	mo *adk.TypedMessageVariant[*schema.AgenticMessage],
	markBroken func(),
	pending *[]*schema.AgenticMessage,
) {
	textMsgID := ""
	textStarted := false
	// 按 tool call 的 Index 跟踪 ID/是否已 START,跨 chunk 累积 args。
	tcIDByIdx := map[int]string{}
	tcStartedByIdx := map[int]bool{}
	var chunks []*schema.AgenticMessage

	for {
		chunk, err := mo.MessageStream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			log.Printf("warn: stream recv error thread=%s run=%s: %v", threadID, runID, err)
			break
		}
		chunks = append(chunks, chunk)

		// 文本 delta
		if text := msgops.AssistantDeltaText(chunk); text != "" {
			if !markBrokenOrSkip(emitter, markBroken) {
				if !textStarted {
					textMsgID = emitter.TextStart()
					textStarted = true
					markBroken()
				}
				if !markBrokenOrSkip(emitter, markBroken) {
					emitter.TextContent(textMsgID, text)
					markBroken()
				}
			}
		}

		// 工具调用 delta(按 Index 关联;ID/Name 通常在首个 chunk 一起到)
		for _, tc := range msgops.ToolCalls(chunk) {
			idx := tc.Index
			if tc.ID != "" && tcIDByIdx[idx] == "" {
				tcIDByIdx[idx] = tc.ID
			}
			id := tcIDByIdx[idx]
			if !tcStartedByIdx[idx] && id != "" && tc.Name != "" {
				if !markBrokenOrSkip(emitter, markBroken) {
					emitter.ToolStartWithID(id, tc.Name)
					markBroken()
				}
				tcStartedByIdx[idx] = true
			}
			if tc.Args != "" && id != "" {
				if !markBrokenOrSkip(emitter, markBroken) {
					emitter.ToolArgs(id, tc.Args)
					markBroken()
				}
			}
		}
	}

	// 收尾:TEXT_END + 各 TOOL_CALL_END(SSE 断了就跳过,合并累积照常)
	if textStarted && !markBrokenOrSkip(emitter, markBroken) {
		emitter.TextEnd(textMsgID)
		markBroken()
	}
	for idx, started := range tcStartedByIdx {
		if started && !markBrokenOrSkip(emitter, markBroken) {
			emitter.ToolEnd(tcIDByIdx[idx])
			markBroken()
		}
	}

	// 合并所有 chunk 为一条完整消息并累积(保留 reasoning signature 等)。
	if len(chunks) > 0 {
		if merged, mErr := msgops.ConcatChunks(chunks); mErr == nil && msgops.HasContent(merged) {
			*pending = append(*pending, merged)
		} else if mErr != nil {
			log.Printf("warn: concat stream chunks failed thread=%s run=%s: %v", threadID, runID, mErr)
		}
	}
}

// handleRegularMessage 处理非流式 assistant 消息:整条文本 + 工具调用一次性发出,再累积。
func handleRegularMessage(
	emitter *agui.Emitter,
	msg *schema.AgenticMessage,
	markBroken func(),
	pending *[]*schema.AgenticMessage,
) {
	content := msgops.AssistantText(msg)
	toolCalls := msgops.ToolCalls(msg)

	if !markBrokenOrSkip(emitter, markBroken) {
		if content != "" {
			msgID := emitter.TextStart()
			markBroken()
			if !markBrokenOrSkip(emitter, markBroken) {
				emitter.TextContent(msgID, content)
				markBroken()
			}
			if !markBrokenOrSkip(emitter, markBroken) {
				emitter.TextEnd(msgID)
				markBroken()
			}
		}
		for _, tc := range toolCalls {
			if !markBrokenOrSkip(emitter, markBroken) {
				emitter.ToolStartWithID(tc.ID, tc.Name)
				markBroken()
			}
			if !markBrokenOrSkip(emitter, markBroken) {
				emitter.ToolArgs(tc.ID, tc.Args)
				markBroken()
			}
			if !markBrokenOrSkip(emitter, markBroken) {
				emitter.ToolEnd(tc.ID)
				markBroken()
			}
		}
	}

	if content != "" || len(toolCalls) > 0 || msgops.HasContent(msg) {
		*pending = append(*pending, msg)
	}
}

// markBrokenOrSkip 检查 SSE 是否已断:已断返回 true 让调用方跳过 emitter 写入,
// 没断则推进 markBroken(吸收一次 emitter 调用后的传输错误)再返回 false。
func markBrokenOrSkip(emitter *agui.Emitter, markBroken func()) bool {
	if emitter.Err() != nil {
		markBroken()
		return true
	}
	return false
}

// toAGUIInterrupts 把 ADK 的 InterruptContexts 转成 AG-UI 的 []types.Interrupt。
// 优先取 IsRootCause 的那一个;都没有就退化用第一个。客户端据此拿到 interruptID,
// 审批时原样回传,GenResume 用它组 ResumeParams.Targets。
//
// ToolCallID 从中断地址里的 tool 段取(见 toolCallIDOf):前端据此把中断关联回
// 对应的 tool call 块,从而复用该工具已注册的渲染组件,不需要前后端各自硬编码
// 某个工具名。Reason 固定 "tool_call" —— 当前所有中断都源自工具调用。
func toAGUIInterrupts(ictxs []*adk.InterruptCtx) []types.Interrupt {
	var pick *adk.InterruptCtx
	for _, ic := range ictxs {
		if ic != nil && ic.IsRootCause {
			pick = ic
			break
		}
	}
	if pick == nil && len(ictxs) > 0 {
		pick = ictxs[0]
	}
	if pick == nil {
		return nil
	}
	it := types.Interrupt{
		ID:      pick.ID,
		Message: infoToString(pick.Info),
	}
	if callID := toolCallIDOf(pick); callID != "" {
		it.Reason = "tool_call"
		it.ToolCallID = callID
	}
	return []types.Interrupt{it}
}

// toolCallIDOf 从中断地址里取触发它的 tool call ID。
//
// ADK 的 Address 形如 "agent:A;tool:order_milk_tea:call_abc123"。compose 的 ToolsNode
// 派发工具时调 appendToolAddressSegment(ctx, task.name, task.callID),所以 tool 段的
// ID 是工具名、SubID 才是 call ID(SubID 的存在正是为了区分同名并发调用)。
// 从后往前找:嵌套 agent-as-tool 场景下最靠近叶子的 tool 段才是真正发起调用的那个。
// 没有 tool 段或 SubID 为空时返回空串,调用方据此跳过关联。
func toolCallIDOf(ic *adk.InterruptCtx) string {
	for i := len(ic.Address) - 1; i >= 0; i-- {
		if ic.Address[i].Type == adk.AddressSegmentTool {
			return ic.Address[i].SubID
		}
	}
	return ""
}

// infoToString 把 InterruptCtx.Info(any)格式化为字符串,优先用 fmt.Stringer。
func infoToString(info any) string {
	if info == nil {
		return ""
	}
	if s, ok := info.(fmt.Stringer); ok {
		return s.String()
	}
	return fmt.Sprintf("%v", info)
}
