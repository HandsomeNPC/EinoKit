package turnloop

import (
	"sync"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// Holder 按 ThreadID 管理常驻 TurnLoop。
type Holder struct {
	mu    sync.Mutex
	loops map[string]*adk.TurnLoop[*ChatItem, *schema.AgenticMessage]
}

// TurnloopHolderInstance 是进程级单例 Holder,按 ThreadID 管理常驻 TurnLoop。
var TurnloopHolderInstance = &Holder{
	loops: make(map[string]*adk.TurnLoop[*ChatItem, *schema.AgenticMessage]),
}

// Get 取出会话的 loop，不存在返回 nil。ok 报告是否命中。
func (h *Holder) Get(threadID string) (loop *adk.TurnLoop[*ChatItem, *schema.AgenticMessage], ok bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	loop, ok = h.loops[threadID]
	return loop, ok
}

// Put 登记会话的 loop，覆盖已有同名条目。
func (h *Holder) Put(threadID string, loop *adk.TurnLoop[*ChatItem, *schema.AgenticMessage]) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.loops[threadID] = loop
}

// Delete 摘掉会话的 loop 登记。
//
// 用于 loop 已退出（Push 返回 false）或被 abort 之后：留着一个死 loop 会让后续
// 请求一直对它 Push 而永远拿不到响应。只摘登记，不负责停止 loop。
func (h *Holder) Delete(threadID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.loops, threadID)
}

// DeleteIf 仅当登记的仍是 loop 本身时才摘掉。
//
// 供 loop 退出后的异步清理使用：期间可能已有新请求建了新 loop 并覆盖登记，
// 此时不应把新 loop 误删。
func (h *Holder) DeleteIf(threadID string, loop *adk.TurnLoop[*ChatItem, *schema.AgenticMessage]) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.loops[threadID] == loop {
		delete(h.loops, threadID)
	}
}
