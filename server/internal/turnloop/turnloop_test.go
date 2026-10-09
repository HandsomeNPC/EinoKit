package turnloop

import (
	"bytes"
	"encoding/gob"
	"sync"
	"testing"

	"github.com/cloudwego/eino/adk"
)

// TestToolCallIDOf 固定住 "call ID 在 SubID 而非 ID" 这条约定。
// compose 压入的地址段是 (ID=工具名, SubID=call ID)，取错字段会让审批卡片关联不上。
func TestToolCallIDOf(t *testing.T) {
	cases := []struct {
		name string
		addr adk.Address
		want string
	}{
		{
			name: "agent 加 tool 两段",
			addr: adk.Address{
				{Type: adk.AddressSegmentAgent, ID: "MilkTeaAgent"},
				{Type: adk.AddressSegmentTool, ID: "Demo", SubID: "call_abc"},
			},
			want: "call_abc",
		},
		{
			name: "嵌套取最内层 tool",
			addr: adk.Address{
				{Type: adk.AddressSegmentTool, ID: "Outer", SubID: "call_outer"},
				{Type: adk.AddressSegmentTool, ID: "Inner", SubID: "call_inner"},
			},
			want: "call_inner",
		},
		{
			name: "只有 agent 段",
			addr: adk.Address{{Type: adk.AddressSegmentAgent, ID: "A"}},
			want: "",
		},
		{
			name: "tool 段缺 SubID",
			addr: adk.Address{{Type: adk.AddressSegmentTool, ID: "Demo"}},
			want: "",
		},
		{name: "空地址", addr: nil, want: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := toolCallIDOf(tc.addr); got != tc.want {
				t.Errorf("toolCallIDOf() = %q, 期望 %q", got, tc.want)
			}
		})
	}
}

func TestInterruptsOf(t *testing.T) {
	ictxs := []*adk.InterruptCtx{
		{
			ID:   "uuid-1",
			Info: "确认下单 2 杯芋泥波波？",
			Address: adk.Address{
				{Type: adk.AddressSegmentTool, ID: "Demo", SubID: "call_1"},
			},
		},
		nil, // 必须被跳过
	}

	got := interruptsOf(ictxs)
	if len(got) != 1 {
		t.Fatalf("得到 %d 条中断，期望 1 条", len(got))
	}
	if got[0].ID != "uuid-1" {
		t.Errorf("ID = %q，期望 uuid-1", got[0].ID)
	}
	if got[0].ToolCallID != "call_1" {
		t.Errorf("ToolCallID = %q，期望 call_1", got[0].ToolCallID)
	}
	if got[0].Message != "确认下单 2 杯芋泥波波？" {
		t.Errorf("Message = %q", got[0].Message)
	}
	if got[0].Reason != InterruptReasonToolCall {
		t.Errorf("Reason = %q，期望 %q", got[0].Reason, InterruptReasonToolCall)
	}
}

// TestTurnOfPicksLatest 固定住"抢占时收尾归最新那一轮"。
// 旧轮的 SSE 流已被取代，发到它上面的事件没人收。
func TestTurnOfPicksLatest(t *testing.T) {
	older := NewTurn("t1", nil)
	newer := NewTurn("t1", nil)
	if newer.Seq <= older.Seq {
		t.Fatalf("Seq 未递增: older=%d newer=%d", older.Seq, newer.Seq)
	}

	items := []*ChatItem{
		{Prompt: "旧", turn: older},
		nil, // 必须被跳过
		{Prompt: "没有 turn"},
		{Prompt: "新", turn: newer},
	}
	if got := turnOf(items); got != newer {
		t.Errorf("turnOf 未返回最新的 turn")
	}

	// 全部不带 turn（从 checkpoint 恢复的条目）时返回 nil，调用方需判空。
	if got := turnOf([]*ChatItem{{Prompt: "x"}}); got != nil {
		t.Errorf("无 turn 时应返回 nil，得到 %v", got)
	}
	if got := turnOf(nil); got != nil {
		t.Errorf("空切片应返回 nil")
	}
}

// TestTurnFinishIdempotent 多条收尾路径都会调 Finish，重复 close 会 panic。
func TestTurnFinishIdempotent(t *testing.T) {
	turn := NewTurn("t1", nil)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			turn.Finish()
		}()
	}
	wg.Wait()

	select {
	case <-turn.Done():
	default:
		t.Fatal("Finish 后 Done 应已关闭")
	}

	// nil turn 上调用也必须安全：从 checkpoint 恢复的轮次没有 turn。
	var nilTurn *Turn
	nilTurn.finishIfSet()
}

func TestTurnRunIDNilSafe(t *testing.T) {
	var nilTurn *Turn
	if got := nilTurn.RunID(); got != "" {
		t.Errorf("nil turn 的 RunID 应为空串，得到 %q", got)
	}
	if got := nilTurn.emitterOrNil(); got != nil {
		t.Errorf("nil turn 的 emitter 应为 nil")
	}
	// emitter 为 nil 的 turn 同样不能 panic（单测场景）。
	if got := NewTurn("t1", nil).RunID(); got != "" {
		t.Errorf("无 emitter 时 RunID 应为空串，得到 %q", got)
	}
}

// TestChatItemGobIgnoresTurn 固定住一个关键约定：TurnLoop 用 gob 持久化未处理条目，
// turn 必须不被编码——否则从 checkpoint 恢复的条目会带着早已关闭的 SSE 流回来。
func TestChatItemGobIgnoresTurn(t *testing.T) {
	item := &ChatItem{Prompt: "买奶茶", turn: NewTurn("t1", nil)}

	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(item); err != nil {
		t.Fatalf("gob 编码失败（ChatItem 必须可 gob 编码）: %v", err)
	}

	var decoded ChatItem
	if err := gob.NewDecoder(&buf).Decode(&decoded); err != nil {
		t.Fatalf("gob 解码失败: %v", err)
	}
	if decoded.Prompt != "买奶茶" {
		t.Errorf("Prompt 丢失: %q", decoded.Prompt)
	}
	if decoded.turn != nil {
		t.Error("turn 不该被 gob 保留——恢复的条目会携带已关闭的 SSE 流")
	}
}
