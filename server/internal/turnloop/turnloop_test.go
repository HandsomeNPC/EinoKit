package turnloop

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// memStore 是测试用的内存 CheckPointStore，带 Delete 以匹配生产 Redis store
// 对 CheckPointDeleter 的实现（TurnLoop 干净退出时会断言它来清快照）。
type memStore struct {
	mu   sync.Mutex
	data map[string][]byte
}

func newMemStore() *memStore { return &memStore{data: map[string][]byte{}} }

func (s *memStore) Get(_ context.Context, key string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.data[key]
	return v, ok, nil
}

func (s *memStore) Set(_ context.Context, key string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = value
	return nil
}

func (s *memStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, key)
	return nil
}

func (s *memStore) has(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.data[key]
	return ok
}

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

// TestSingleTurnLoopExits 锁定 handler 依赖的核心契约：OnAgentEvents 里调用
// Stop 之后，一轮跑完 loop 必须真正退出，Wait() 不能挂住。
//
// 这是回归测试：不 Stop 的话 loop 会阻塞在空队列的 Receive 上等下一轮输入，
// HTTP handler 的 Wait() 永不返回，请求挂死。
func TestSingleTurnLoopExits(t *testing.T) {
	store := newMemStore()
	const threadID = "thread-exit"

	var seen string
	loop := adk.NewTurnLoop(adk.TurnLoopConfig[*ChatItem, *schema.AgenticMessage]{
		GenInput: func(_ context.Context, _ *adk.TurnLoop[*ChatItem, *schema.AgenticMessage],
			items []*ChatItem) (*adk.GenInputResult[*ChatItem, *schema.AgenticMessage], error) {
			return &adk.GenInputResult[*ChatItem, *schema.AgenticMessage]{
				Input: &adk.TypedAgentInput[*schema.AgenticMessage]{
					Messages: []*schema.AgenticMessage{schema.UserAgenticMessage(items[0].Query)},
				},
				Consumed: items,
			}, nil
		},
		PrepareAgent: func(context.Context, *adk.TurnLoop[*ChatItem, *schema.AgenticMessage],
			[]*ChatItem) (adk.TypedAgent[*schema.AgenticMessage], error) {
			return &stubAgent{reply: "好的"}, nil
		},
		OnAgentEvents: func(_ context.Context, tc *adk.TurnContext[*ChatItem, *schema.AgenticMessage],
			evIter *adk.AsyncIterator[*adk.TypedAgentEvent[*schema.AgenticMessage]]) error {
			// 与生产代码同一时序：进入事件处理后立刻 Stop。
			tc.Loop.Stop(adk.WithStopCause("single-turn run"))
			for {
				ev, ok := evIter.Next()
				if !ok {
					break
				}
				if ev.Output != nil && ev.Output.MessageOutput != nil && ev.Output.MessageOutput.Message != nil {
					for _, b := range ev.Output.MessageOutput.Message.ContentBlocks {
						if b != nil && b.AssistantGenText != nil {
							seen += b.AssistantGenText.Text
						}
					}
				}
				if ev.Action != nil && ev.Action.Exit {
					break
				}
			}
			return nil
		},
		Store:        store,
		CheckpointID: threadID,
	})

	if ok, _ := loop.Push(&ChatItem{Query: "来一杯"}); !ok {
		t.Fatal("Push 失败")
	}
	loop.Run(context.Background())

	done := make(chan *adk.TurnLoopExitState[*ChatItem, *schema.AgenticMessage], 1)
	go func() { done <- loop.Wait() }()

	select {
	case state := <-done:
		if state.ExitReason != nil {
			t.Fatalf("期望干净退出，实际 ExitReason=%v", state.ExitReason)
		}
		// handler 用"快照是否存在"判断有没有待审批，所以干净跑完必须不留快照，
		// 否则下一次 /chat 会误判成有待确认操作而拒绝新提问。
		if store.has(threadID) {
			t.Error("干净退出后仍残留 checkpoint，会让后续 /chat 误判为待审批")
		}
		if seen != "好的" {
			t.Errorf("消息未透传，got %q", seen)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("loop.Wait() 超时未返回：单轮结束后没有退出，HTTP 请求会挂死")
	}
}

// TestUnconsumedItemDoesNotHang 覆盖 GenInput 没有可用 query 时的退出路径
// （gen_input.go 在这种情况下调 loop.Stop）。
func TestUnconsumedItemDoesNotHang(t *testing.T) {
	loop := adk.NewTurnLoop(adk.TurnLoopConfig[*ChatItem, *schema.AgenticMessage]{
		GenInput: func(_ context.Context, l *adk.TurnLoop[*ChatItem, *schema.AgenticMessage],
			items []*ChatItem) (*adk.GenInputResult[*ChatItem, *schema.AgenticMessage], error) {
			l.Stop(adk.WithStopCause("no query items"))
			return &adk.GenInputResult[*ChatItem, *schema.AgenticMessage]{
				Input: &adk.TypedAgentInput[*schema.AgenticMessage]{
					Messages: []*schema.AgenticMessage{schema.UserAgenticMessage("done")},
				},
				Remaining: items,
			}, nil
		},
		PrepareAgent: func(context.Context, *adk.TurnLoop[*ChatItem, *schema.AgenticMessage],
			[]*ChatItem) (adk.TypedAgent[*schema.AgenticMessage], error) {
			return &stubAgent{reply: "x"}, nil
		},
	})

	loop.Push(&ChatItem{})
	loop.Run(context.Background())

	done := make(chan struct{})
	go func() { loop.Wait(); close(done) }()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("GenInput 调用 Stop 后 loop 未退出")
	}
}
