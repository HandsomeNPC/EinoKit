package turnloop

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// stubAgent 是最小 TypedAgent：产出一条 assistant 文本后发 Exit。
type stubAgent struct{ reply string }

func (a *stubAgent) Name(context.Context) string        { return "stub" }
func (a *stubAgent) Description(context.Context) string { return "stub agent" }

func (a *stubAgent) Run(ctx context.Context, input *adk.TypedAgentInput[*schema.AgenticMessage],
	_ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.TypedAgentEvent[*schema.AgenticMessage]] {
	iter, gen := adk.NewAsyncIteratorPair[*adk.TypedAgentEvent[*schema.AgenticMessage]]()
	go func() {
		defer gen.Close()
		gen.Send(&adk.TypedAgentEvent[*schema.AgenticMessage]{
			AgentName: "stub",
			Output: &adk.TypedAgentOutput[*schema.AgenticMessage]{
				MessageOutput: &adk.TypedMessageVariant[*schema.AgenticMessage]{
					Message: &schema.AgenticMessage{
						Role: schema.AgenticRoleTypeAssistant,
						ContentBlocks: []*schema.ContentBlock{
							schema.NewContentBlock(&schema.AssistantGenText{Text: a.reply}),
						},
					},
				},
			},
		})
		gen.Send(&adk.TypedAgentEvent[*schema.AgenticMessage]{
			AgentName: "stub",
			Action:    &adk.AgentAction{Exit: true},
		})
	}()
	return iter
}

// slowAgent 在产出回复前先等一个信号，便于测试抢占正在进行中的轮次。
type slowAgent struct {
	reply   string
	release <-chan struct{}
	started chan<- struct{}
}

func (a *slowAgent) Name(context.Context) string        { return "slow" }
func (a *slowAgent) Description(context.Context) string { return "slow agent" }

func (a *slowAgent) Run(ctx context.Context, input *adk.TypedAgentInput[*schema.AgenticMessage],
	_ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.TypedAgentEvent[*schema.AgenticMessage]] {
	iter, gen := adk.NewAsyncIteratorPair[*adk.TypedAgentEvent[*schema.AgenticMessage]]()
	go func() {
		defer gen.Close()
		select {
		case a.started <- struct{}{}:
		default:
		}
		select {
		case <-a.release:
		case <-ctx.Done():
			return
		}
		gen.Send(&adk.TypedAgentEvent[*schema.AgenticMessage]{
			AgentName: "slow",
			Output: &adk.TypedAgentOutput[*schema.AgenticMessage]{
				MessageOutput: &adk.TypedMessageVariant[*schema.AgenticMessage]{
					Message: &schema.AgenticMessage{
						Role: schema.AgenticRoleTypeAssistant,
						ContentBlocks: []*schema.ContentBlock{
							schema.NewContentBlock(&schema.AssistantGenText{Text: a.reply}),
						},
					},
				},
			},
		})
		gen.Send(&adk.TypedAgentEvent[*schema.AgenticMessage]{
			AgentName: "slow",
			Action:    &adk.AgentAction{Exit: true},
		})
	}()
	return iter
}

// TestLoopSurvivesMultipleTurns 锁定常驻 loop 的核心契约：一轮跑完之后
// loop 不能退出，要继续等下一条输入。
//
// 这是回归测试：旧实现在 OnAgentEvents 里调 Stop 把 loop 限制成单轮，
// 于是第二个请求只能被 409 拒掉，用不上框架的抢占能力。
func TestLoopSurvivesMultipleTurns(t *testing.T) {
	var mu sync.Mutex
	var turns []string

	loop := adk.NewTurnLoop(adk.TurnLoopConfig[*ChatItem, *schema.AgenticMessage]{
		GenInput: func(_ context.Context, _ *adk.TurnLoop[*ChatItem, *schema.AgenticMessage],
			items []*ChatItem) (*adk.GenInputResult[*ChatItem, *schema.AgenticMessage], error) {
			return &adk.GenInputResult[*ChatItem, *schema.AgenticMessage]{
				Input: &adk.TypedAgentInput[*schema.AgenticMessage]{
					Messages: []*schema.AgenticMessage{schema.UserAgenticMessage(items[0].Query)},
				},
				Consumed: items[:1],
			}, nil
		},
		PrepareAgent: func(context.Context, *adk.TurnLoop[*ChatItem, *schema.AgenticMessage],
			[]*ChatItem) (adk.TypedAgent[*schema.AgenticMessage], error) {
			return &stubAgent{reply: "ok"}, nil
		},
		OnAgentEvents: func(_ context.Context, tc *adk.TurnContext[*ChatItem, *schema.AgenticMessage],
			evIter *adk.AsyncIterator[*adk.TypedAgentEvent[*schema.AgenticMessage]]) error {
			mu.Lock()
			turns = append(turns, tc.Consumed[0].Query)
			mu.Unlock()
			for {
				ev, ok := evIter.Next()
				if !ok {
					break
				}
				if ev.Action != nil && ev.Action.Exit {
					break
				}
			}
			return nil
		},
	})

	loop.Push(&ChatItem{Query: "first"})
	loop.Run(context.Background())

	deadline := time.After(5 * time.Second)
	for {
		mu.Lock()
		n := len(turns)
		mu.Unlock()
		if n >= 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("第一轮未开始")
		case <-time.After(10 * time.Millisecond):
		}
	}
	// 第二条在第一轮之后压入：loop 仍活着才会处理它。
	if ok, _ := loop.Push(&ChatItem{Query: "second"}); !ok {
		t.Fatal("loop 已退出，第二条无法入队——常驻契约被破坏")
	}

	for {
		mu.Lock()
		n := len(turns)
		mu.Unlock()
		if n >= 2 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("第二轮未执行，loop 在单轮后就退出了 turns=%v", turns)
		case <-time.After(10 * time.Millisecond):
		}
	}

	loop.Stop(adk.WithImmediate())
	loop.Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(turns) < 2 || turns[0] != "first" || turns[1] != "second" {
		t.Errorf("两轮应按序执行，got %v", turns)
	}
}

// TestPreemptCancelsRunningTurn 锁定抢占语义：新消息到达时，正在跑的那一轮
// 在安全点被取消，新一轮的 GenInput 能看到全部缓冲条目（上下文不丢）。
//
// 这是"用户打断 AI 回答"的底层机制，替代了旧实现的 409。
func TestPreemptCancelsRunningTurn(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 1)

	var mu sync.Mutex
	var inputBatches [][]string
	preempted := make(chan struct{})

	loop := adk.NewTurnLoop(adk.TurnLoopConfig[*ChatItem, *schema.AgenticMessage]{
		GenInput: func(_ context.Context, _ *adk.TurnLoop[*ChatItem, *schema.AgenticMessage],
			items []*ChatItem) (*adk.GenInputResult[*ChatItem, *schema.AgenticMessage], error) {
			batch := make([]string, 0, len(items))
			for _, it := range items {
				batch = append(batch, it.Query)
			}
			mu.Lock()
			inputBatches = append(inputBatches, batch)
			mu.Unlock()
			return &adk.GenInputResult[*ChatItem, *schema.AgenticMessage]{
				Input: &adk.TypedAgentInput[*schema.AgenticMessage]{
					Messages: []*schema.AgenticMessage{schema.UserAgenticMessage(items[0].Query)},
				},
				Consumed: items,
			}, nil
		},
		PrepareAgent: func(context.Context, *adk.TurnLoop[*ChatItem, *schema.AgenticMessage],
			[]*ChatItem) (adk.TypedAgent[*schema.AgenticMessage], error) {
			return &slowAgent{reply: "done", release: release, started: started}, nil
		},
		OnAgentEvents: func(_ context.Context, tc *adk.TurnContext[*ChatItem, *schema.AgenticMessage],
			evIter *adk.AsyncIterator[*adk.TypedAgentEvent[*schema.AgenticMessage]]) error {
			for {
				select {
				case <-tc.Preempted:
					close(preempted)
					return nil
				default:
				}
				ev, ok := evIter.Next()
				if !ok {
					return nil
				}
				if ev.Action != nil && ev.Action.Exit {
					return nil
				}
			}
		},
	})

	loop.Push(&ChatItem{Query: "slow question"})
	loop.Run(context.Background())

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("第一轮未开始")
	}

	// 用户在 AI 还在回答时发了新消息：带抢占压入。
	ok, ack := loop.Push(&ChatItem{Query: "actually, stop"},
		adk.WithPreemptTimeout[*ChatItem, *schema.AgenticMessage](adk.AnySafePoint, 2*time.Second))
	if !ok {
		t.Fatal("抢占 Push 失败")
	}
	select {
	case <-ack:
	case <-time.After(5 * time.Second):
		t.Fatal("抢占未被确认")
	}
	close(release)

	select {
	case <-preempted:
	case <-time.After(5 * time.Second):
		t.Fatal("tc.Preempted 未触发：OnAgentEvents 察觉不到抢占，旧轮会继续写 SSE")
	}

	// 等到新一轮的 GenInput 真正跑起来再 Stop，否则 Stop 在两轮之间的空闲点
	// 落地会让 loop 直接退出，永远不跑新条目——这只是测试时序问题，生产里
	// loop 不会被外部 Stop。
	deadline := time.After(5 * time.Second)
	for {
		mu.Lock()
		n := len(inputBatches)
		mu.Unlock()
		if n >= 2 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("抢占后新一轮未启动，inputBatches=%d", n)
		case <-time.After(10 * time.Millisecond):
		}
	}

	loop.Stop(adk.WithImmediate())
	loop.Wait()

	// 关键：新一轮必须能看到被抢占那轮的条目，否则用户的上文就丢了。
	mu.Lock()
	defer mu.Unlock()
	if len(inputBatches) < 2 {
		t.Fatalf("抢占后应开启新一轮，实际只有 %d 轮", len(inputBatches))
	}
	last := inputBatches[len(inputBatches)-1]
	var sawNew bool
	for _, q := range last {
		if q == "actually, stop" {
			sawNew = true
		}
	}
	if !sawNew {
		t.Errorf("新一轮的 GenInput 应看到新条目，got %v", last)
	}
}

// TestToAGUIInterruptsCarriesToolCallID 锁定中断 → tool call 的关联方式。
//
// 前端靠 Interrupt.ToolCallID 把审批请求关联回对应的 tool call 块，从而复用该
// 工具已注册的渲染组件。关联键取自 Address 里 tool 段的 SubID 而非 ID：
// compose 的 ToolsNode 调 appendToolAddressSegment(ctx, task.name, task.callID)，
// 所以 ID 是工具名、SubID 才是 call ID。写反了前端会拿工具名当 ID 去匹配，
// 永远匹配不上且不报错——正是这个测试要挡住的。
func TestToAGUIInterruptsCarriesToolCallID(t *testing.T) {
	ictxs := []*adk.InterruptCtx{
		{
			ID: "agent:demo;tool:order_milk_tea:call_abc123",
			Address: adk.Address{
				{Type: adk.AddressSegmentAgent, ID: "demo"},
				{Type: adk.AddressSegmentTool, ID: "order_milk_tea", SubID: "call_abc123"},
			},
			Info:        "确认下单？",
			IsRootCause: true,
		},
	}

	got := toAGUIInterrupts(ictxs)
	if len(got) != 1 {
		t.Fatalf("期望 1 个 interrupt，got %d", len(got))
	}
	if got[0].ID != ictxs[0].ID {
		t.Errorf("ID 应原样透传（resume 要拿它回填 Targets），got %q", got[0].ID)
	}
	if got[0].ToolCallID != "call_abc123" {
		t.Errorf("ToolCallID 应取 tool 段的 SubID，got %q", got[0].ToolCallID)
	}
	if got[0].Reason != "tool_call" {
		t.Errorf("Reason 应为 tool_call，got %q", got[0].Reason)
	}
	if got[0].Message != "确认下单？" {
		t.Errorf("Message 应来自 Info，got %q", got[0].Message)
	}
}

// TestToAGUIInterruptsPicksInnermostTool 覆盖 agent-as-tool 嵌套：
// 最靠近叶子的 tool 段才是真正发起调用的那个。
func TestToAGUIInterruptsPicksInnermostTool(t *testing.T) {
	got := toAGUIInterrupts([]*adk.InterruptCtx{
		{
			Address: adk.Address{
				{Type: adk.AddressSegmentTool, ID: "outer_agent", SubID: "call_outer"},
				{Type: adk.AddressSegmentTool, ID: "inner_tool", SubID: "call_inner"},
			},
			IsRootCause: true,
		},
	})
	if got[0].ToolCallID != "call_inner" {
		t.Errorf("应取最内层 tool 段，got %q", got[0].ToolCallID)
	}
}

// TestToAGUIInterruptsWithoutToolSegment 非工具触发的中断不该伪造 tool 关联，
// 否则前端会拿空 ID 去匹配 tool call 块。
func TestToAGUIInterruptsWithoutToolSegment(t *testing.T) {
	got := toAGUIInterrupts([]*adk.InterruptCtx{
		{
			ID:          "agent:demo",
			Address:     adk.Address{{Type: adk.AddressSegmentAgent, ID: "demo"}},
			IsRootCause: true,
		},
	})
	if got[0].ToolCallID != "" {
		t.Errorf("无 tool 段时 ToolCallID 应为空，got %q", got[0].ToolCallID)
	}
	if got[0].Reason != "" {
		t.Errorf("无 tool 段时 Reason 应为空，got %q", got[0].Reason)
	}
}
