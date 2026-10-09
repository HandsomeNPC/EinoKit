package turnloop

import (
	"sync"
	"sync/atomic"

	"github.com/HandsomeNPC/EinoKit/internal/agui"
)

// turnSeq 是进程级自增的轮次序号，用于在一批条目里挑出"最新"的那一轮。
var turnSeq atomic.Uint64

// Turn 是一次 HTTP 请求对应的轮次凭据。
//
// 为什么需要它：TurnLoop 跨请求常驻（见 turnloop_holder.go），而每次请求的 SSE 流
// 和 runID 都是新的。若把 emitter 闭包进 NewTurnLoop，第二次请求的事件会写向第一次
// 请求那条已关闭的连接。所以 emitter/runID 必须随条目走，由 OnAgentEvents 从
// tc.Consumed 里取出本轮的那个。
//
// 同时它承担第二个职责：Run() 是非阻塞的（adk 内部 go l.run(ctx)），handler 若直接
// 返回，gin 会关掉 ResponseWriter，后续所有写入都落空。handler 必须阻塞在 Done()
// 上，等 OnAgentEvents 调用 Finish 后才返回。
type Turn struct {
	Seq      uint64
	Emitter  *agui.Emitter
	ThreadID string

	done     chan struct{}
	finishMu sync.Mutex
	finished bool
}

// NewTurn 创建一轮凭据。
func NewTurn(threadID string, emitter *agui.Emitter) *Turn {
	return &Turn{
		Seq:      turnSeq.Add(1),
		Emitter:  emitter,
		ThreadID: threadID,
		done:     make(chan struct{}),
	}
}

// Done 返回本轮结束信号，供 handler 阻塞等待。
func (t *Turn) Done() <-chan struct{} { return t.done }

// Finish 标记本轮结束，唤醒等待的 handler。幂等：多条收尾路径（正常结束、出错、
// 中断、被抢占）都会调用它，重复关闭 channel 会 panic。
func (t *Turn) Finish() {
	t.finishMu.Lock()
	defer t.finishMu.Unlock()
	if t.finished {
		return
	}
	t.finished = true
	close(t.done)
}

// RunID 返回本轮的 runID。emitter 为 nil 时返回空串（如单测场景）。
func (t *Turn) RunID() string {
	if t == nil || t.Emitter == nil {
		return ""
	}
	return t.Emitter.RunID
}

// turnOf 从本轮消费的条目里挑出轮次凭据，取 Seq 最大的那个。
//
// 一轮可能消费多个条目：抢占时新条目与被抢占的旧条目会一起进入同一轮（GenInput
// 看到的是全部缓冲条目）。此时收尾应归最新那一轮——它才持有仍然活着的 SSE 流。
//
// 从 checkpoint resume 回来的条目不带 turn（turn 是非导出字段，gob 会忽略它），
// 返回 nil，调用方需判空。
func turnOf(items []*ChatItem) *Turn {
	var best *Turn
	for _, item := range items {
		if item == nil || item.turn == nil {
			continue
		}
		if best == nil || item.turn.Seq > best.Seq {
			best = item.turn
		}
	}
	return best
}
