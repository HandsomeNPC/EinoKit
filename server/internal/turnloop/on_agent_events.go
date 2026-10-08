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

	"github.com/HandsomeNPC/EinoKit/internal/agui"
	"github.com/HandsomeNPC/EinoKit/internal/memory/thread"
	"github.com/HandsomeNPC/EinoKit/internal/msgops"
)

var errInterrupted = errors.New("agent interrupted for approval")

// makeOnAgentEvents 返回 OnAgentEvents 回调。
func makeOnAgentEvents(threadID, runID string, emitter *agui.Emitter) func(ctx context.Context, tc *adk.TurnContext[*ChatItem, *schema.AgenticMessage], evIter *adk.AsyncIterator[*adk.TypedAgentEvent[*schema.AgenticMessage]]) error {
	return func(ctx context.Context, tc *adk.TurnContext[*ChatItem, *schema.AgenticMessage], evIter *adk.AsyncIterator[*adk.TypedAgentEvent[*schema.AgenticMessage]]) error {
		// 一次 HTTP 请求 = 一个 loop = 一轮对话，所以这里就把 loop 标记为停止：
		// 不带 cancel 选项的 Stop 不会打断本轮，agent 照常跑完，loop 只是不再开下一轮。
		// 必须显式 Stop——否则本轮结束后 loop 会阻塞在空队列的 Receive 上等新输入，
		// handler 的 Wait() 永远不返回，请求就挂死了。
		// 放在这里而不是 handler 里：此刻本轮已通过全部 isCommitted 检查并开始执行，
		// 在 handler 里 Push 后立刻 Stop 则可能让 loop 在取出条目前就退出，输入被丢掉。
		tc.Loop.Stop(adk.WithStopCause("single-turn run"))

		// writerBroken 在 SSE 写入失败(浏览器关页/断网)时置位。置位后
		// 停止向 UI 写,但继续消费事件、累积消息,以保住后台任务。
		writerBroken := false
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

			// 1) 错误事件:发 RUN_ERROR,flush 已累积的消息,返回 err。
			if event.Err != nil {
				if !writerBroken {
					emitter.RunError(event.Err.Error())
					markBroken()
				}
				flushPending(threadID, runID, &pending)
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
		if !writerBroken {
			emitter.RunFinishedSuccess()
			markBroken()
		}
		return nil
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
	return []types.Interrupt{{ID: pick.ID, Message: infoToString(pick.Info)}}
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
