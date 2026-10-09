package thread

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/gin-gonic/gin"

	"github.com/HandsomeNPC/EinoKit/internal/agui"
	"github.com/HandsomeNPC/EinoKit/internal/memory/thread"
	"github.com/HandsomeNPC/EinoKit/internal/model/bo"
	"github.com/HandsomeNPC/EinoKit/internal/turnloop"
)

// turnTimeout 是单轮对话的兜底超时。超时只是放开 HTTP 连接，不终止后端 run
// （真正要停得走 /threads/abort）。
const turnTimeout = 10 * time.Minute

// Chat 会话对话。POST /threads/chat
func Chat(c *gin.Context) {
	var req bo.ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误: " + err.Error()})
		return
	}
	threadID := strings.TrimSpace(req.ThreadID)
	if threadID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "threadId 不能为空"})
		return
	}
	if _, err := thread.SelectByID(threadID); err != nil {
		if errors.Is(err, thread.ErrThreadNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "会话不存在"})
			return
		}
		log.Printf("[thread] 查询会话失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询会话失败"})
		return
	}

	runID := events.GenerateRunID()
	emitter := agui.NewEmitter(c, threadID, runID)
	tc := turnloop.ThreadContext{ThreadID: threadID}

	// turn 承载本轮的 emitter/runID 与结束信号，随 ChatItem 进入 loop。
	turn := turnloop.NewTurn(threadID, emitter)
	chatItem := turnloop.NewChatItem(&req, turn)

	writeSSEHeaders(c)

	isResume := req.IsResume()
	if isResume {
		// 不发 RUN_STARTED：TDesign 的 adapter 在 RUN_STARTED 时会 reset()，
		// 清空 toolCallMap，之后中断前那个 toolCallId 的 TOOL_CALL_RESULT 会撞上
		// "未知 toolCallId" 的校验而被静默丢弃，审批卡片永远翻不到完成态。
		// 详见 docs/agui-event-bridge.md §2。
		submit(tc, chatItem, true)
	} else {
		emitter.RunStarted()
		submit(tc, chatItem, false)
	}

	// Run() 是非阻塞的（adk 内部 go l.run(ctx)）。handler 必须在此阻塞到本轮收尾，
	// 否则 gin 会关掉 ResponseWriter，OnAgentEvents 后续所有写入都落空。
	select {
	case <-turn.Done():
	case <-c.Request.Context().Done():
		// 客户端断开。后端 run 仍在跑（要停得显式 /abort），这里只放开连接；
		// OnAgentEvents 会因 emitter 写失败而转为"只持久化不输出"。
		log.Printf("[chat] thread=%s run=%s 客户端断开", threadID, runID)
	case <-time.After(turnTimeout):
		log.Printf("[chat] thread=%s run=%s 单轮超时 %s", threadID, runID, turnTimeout)
	}

	if err := emitter.Err(); err != nil {
		log.Printf("[chat] thread=%s run=%s SSE 传输中断: %v", threadID, runID, err)
	}
	if err := emitter.EncErr(); err != nil {
		log.Printf("[chat] thread=%s run=%s 事件编码失败: %v", threadID, runID, err)
	}
}

// writeSSEHeaders 写 SSE 响应头。关掉 Nginx 缓冲，否则事件会被攒着不下发。
func writeSSEHeaders(c *gin.Context) {
	h := c.Writer.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	c.Writer.WriteHeader(http.StatusOK)
	c.Writer.Flush()
}

// submit 把条目送进会话的常驻 loop。
//
// loop 已存在且在跑时用抢占式 Push：在安全点（模型返回后 / 工具调用全部结束后）
// 取消当前轮，开新一轮——新一轮的 GenInput 能看到包括新条目在内的全部缓冲条目，
// 上文不丢。恢复条目不抢占：它要的是让已中断的那一轮继续，而不是打断谁。
func submit(tc turnloop.ThreadContext, item *turnloop.ChatItem, isResume bool) {
	loop, ok := turnloop.TurnloopHolderInstance.Get(tc.ThreadID)
	if ok {
		if pushed := pushInto(loop, item, isResume); pushed {
			loop.Run(context.Background())
			return
		}
		// Push 返回 false 说明 loop 已停止（上一轮出错或被 abort）。重试同一个
		// loop 必然再失败，必须摘掉登记后重建。
		log.Printf("[chat] thread=%s loop 已退出，重建", tc.ThreadID)
		turnloop.TurnloopHolderInstance.Delete(tc.ThreadID)
	}

	loop = turnloop.NewTurnLoop(tc)
	turnloop.TurnloopHolderInstance.Put(tc.ThreadID, loop)
	loop.Push(item)
	loop.Run(context.Background())
	watchLoopExit(tc.ThreadID, loop)
}

// pushInto 向已有 loop 投递条目，返回是否投递成功。
func pushInto(loop *adk.TurnLoop[*turnloop.ChatItem, *schema.AgenticMessage], item *turnloop.ChatItem, isResume bool) bool {
	if isResume {
		ok, _ := loop.Push(item)
		return ok
	}
	ok, _ := loop.Push(item, adk.WithPreempt[*turnloop.ChatItem, *schema.AgenticMessage](adk.AnySafePoint))
	return ok
}

// watchLoopExit 等 loop 退出后摘掉登记，使下次请求新建而不是对着死 loop Push。
//
// loop 并非永生：业务中断（等待审批）和错误都会让它退出以落快照，空闲超时也会回收。
func watchLoopExit(threadID string, loop *adk.TurnLoop[*turnloop.ChatItem, *schema.AgenticMessage]) {
	go func() {
		state := loop.Wait()
		turnloop.TurnloopHolderInstance.DeleteIf(threadID, loop)
		if state.ExitReason != nil {
			log.Printf("[loop] thread=%s 退出: %v", threadID, state.ExitReason)
			return
		}
		log.Printf("[loop] thread=%s 正常退出", threadID)
	}()
}
