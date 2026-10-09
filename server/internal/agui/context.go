package agui

import "context"

// emitterKey 是 Emitter 在 context 中的键。用独立类型避免与其他包的键冲突。
type emitterKey struct{}

// WithEmitter 把本轮的 Emitter 挂到 ctx 上。
//
// 由 TurnLoop 的 GenInput 通过 GenInputResult.RunCtx 注入：loop 跨请求存活，
// 每轮的 SSE 流不同，所以 emitter 只能随轮传递，不能在构造期绑定。
func WithEmitter(ctx context.Context, e *Emitter) context.Context {
	return context.WithValue(ctx, emitterKey{}, e)
}

// FromContext 取出本轮的 Emitter，供工具内部直接发 STATE_*/ACTIVITY_* 事件——
// 这类"工具执行到第几步"的中途状态在 Eino 的事件流里没有对应物，OnAgentEvents
// 拿不到（工具还在跑，工具结果尚未产生）。
//
// 取不到时返回 nil，调用方需判空：工具可能在没有 SSE 流的场景下被调用（如单测）。
func FromContext(ctx context.Context) *Emitter {
	e, _ := ctx.Value(emitterKey{}).(*Emitter)
	return e
}
