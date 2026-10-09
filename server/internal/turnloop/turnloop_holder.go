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

// NewHolder 构造一个空的 Holder。
func NewHolder() *Holder {
	return &Holder{loops: make(map[string]*adk.TurnLoop[*ChatItem, *schema.AgenticMessage])}
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

// Delete 移除会话的 loop 登记，不存在时无副作用。
func (h *Holder) Delete(threadID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.loops, threadID)
}

// Len 返回当前登记的 loop 数量。
func (h *Holder) Len() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.loops)
}
