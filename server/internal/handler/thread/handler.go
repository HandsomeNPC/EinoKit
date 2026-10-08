// Package thread 会话相关接口。
package thread

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/HandsomeNPC/EinoKit/internal/agents"
	"github.com/HandsomeNPC/EinoKit/internal/agui"
	"github.com/HandsomeNPC/EinoKit/internal/checkpoint"
	"github.com/HandsomeNPC/EinoKit/internal/memory/thread"
	"github.com/HandsomeNPC/EinoKit/internal/model/bo"
	"github.com/HandsomeNPC/EinoKit/internal/turnloop"
)

// createThreadRequest 是创建会话的请求体。两个字段都可省略：
// Title 留空由首轮对话内容回填，AgentType 留空用 demo agent。
type createThreadRequest struct {
	Title     string `json:"title"`
	AgentType string `json:"agentType"`
}

// CreateThread 创建会话。POST /threads
func CreateThread(c *gin.Context) {
	var req createThreadRequest
	// 请求体允许为空（全部走默认值），只有非空且格式错误才拒绝。
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误: " + err.Error()})
		return
	}

	agentType := strings.TrimSpace(req.AgentType)
	if agentType == "" {
		agentType = agents.AgentNameDemo
	}
	// 提前校验 agent 是否注册，避免建完会话后每次 /chat 都失败。
	if !agents.IsRegistered(agentType) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "未注册的 agentType: " + agentType})
		return
	}

	info := &thread.ThreadInfo{
		ThreadID:  uuid.New().String(),
		Title:     strings.TrimSpace(req.Title),
		AgentType: agentType,
	}
	if err := thread.Insert(info); err != nil {
		log.Printf("[thread] 创建会话失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建会话失败"})
		return
	}
	c.JSON(http.StatusOK, info)
}

// ListThreads 列出全部会话。GET /threads
func ListThreads(c *gin.Context) {
	rows, err := thread.SelectList()
	if err != nil {
		log.Printf("[thread] 查询会话列表失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询会话列表失败"})
		return
	}
	// 显式兜空切片，保证 JSON 是 [] 而不是 null。
	if rows == nil {
		rows = []*thread.ThreadInfo{}
	}
	c.JSON(http.StatusOK, rows)
}

// bindThreadID 从请求体读出 threadId 并校验会话存在。
//
// 会话级接口的公共前置：失败时已写好响应（400/404/500），调用方直接 return。
func bindThreadID(c *gin.Context) (string, bool) {
	var req bo.ThreadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误: " + err.Error()})
		return "", false
	}
	id := strings.TrimSpace(req.ThreadID)
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "threadId 不能为空"})
		return "", false
	}
	if _, err := thread.SelectByID(id); err != nil {
		writeThreadLookupError(c, err)
		return "", false
	}
	return id, true
}

// DeleteThread 删除指定会话。POST /threads/delete
func DeleteThread(c *gin.Context) {
	id, ok := bindThreadID(c)
	if !ok {
		return
	}

	// 会话上还有在跑的 run 时先终止，否则它会继续往已删除的会话里写消息。
	if turnloop.Abort(id) {
		log.Printf("[thread] 删除会话前终止了运行中的 run thread=%s", id)
	}

	if err := thread.DeleteByID(id); err != nil {
		log.Printf("[thread] 删除会话失败 thread=%s: %v", id, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除会话失败"})
		return
	}
	// 清掉可能残留的审批快照，避免同 ID 复用时读到过期状态。
	if deleter, ok := checkpoint.GetInstance().(interface {
		Delete(ctx context.Context, key string) error
	}); ok {
		if err := deleter.Delete(c.Request.Context(), id); err != nil {
			log.Printf("warn: 删除 checkpoint 失败 thread=%s: %v", id, err)
		}
	}
	c.Status(http.StatusNoContent)
}

// Chat 会话对话。POST /threads/chat
//
// 以 AG-UI 事件流（SSE）返回。请求体必带 threadId，并二选一（见 bo.ChatRequest）：
//   - 新一轮提问：{"threadId": "...", "prompt": "..."}
//   - 恢复中断：{"threadId": "...", "resume": [{"status": "resolved", "payload": true}]}
//
// resume 的 interruptId 可以省略：后端用 thread_info 里的中断快照回填
// （见 buildChatItem）。客户端传了也以服务端快照为准。
//
// 一个会话同一时刻只允许一个 run；并发请求返回 409。
func Chat(c *gin.Context) {
	var req bo.ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误: " + err.Error()})
		return
	}

	id := strings.TrimSpace(req.ThreadID)
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "threadId 不能为空"})
		return
	}
	info, err := thread.SelectByID(id)
	if err != nil {
		writeThreadLookupError(c, err)
		return
	}

	// 以 checkpoint 为准判断有没有待确认的审批：干净跑完和主动终止都会删掉快照
	// （见 turnloop.Abort 的 WithSkipCheckpoint 说明），所以两者不会混淆。
	// interrupts 则是中断详情，resume 时用来回填 interruptId。
	interrupts, err := loadPendingInterrupts(c.Request.Context(), id, info)
	if err != nil {
		log.Printf("[thread] 读取会话状态失败 thread=%s: %v", id, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取会话状态失败"})
		return
	}
	pending := len(interrupts) > 0
	switch {
	case req.IsResume() && !pending:
		c.JSON(http.StatusConflict, gin.H{"error": "当前没有待确认的操作，无法恢复"})
		return
	case !req.IsResume() && pending:
		// 不能直接丢掉新 prompt 去跑 resume：TurnLoop 读到带 runner 状态的快照
		// 就会走 GenResume，新输入会被静默忽略。明确要求先解决审批。
		c.JSON(http.StatusConflict, gin.H{"error": "有待确认的操作，请先批准或取消后再提问"})
		return
	}

	item, err := buildChatItem(&req, interrupts)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	runID := uuid.New().String()
	emitter := agui.NewEmitter(c, id, runID)

	// loop 用脱离请求的 context：浏览器断开后 OnAgentEvents 仍要把已产出的
	// 消息累积落库（它内部用 writerBroken 兜住写失败），不能被请求取消带走。
	// 终止由 /abort 经 registry 下达，handler 则 Wait 到 loop 真正结束——
	// 这样才不会在 gin 回收 ResponseWriter 之后还往里写。
	loop := turnloop.NewTurnLoop(id, runID, emitter)

	// 先占到运行权再写 SSE 头：抢占失败要回 409 JSON，头一旦发出就只能发事件了。
	deregister, err := turnloop.TryRegister(id, loop)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	defer deregister()

	writeSSEHeaders(c)
	emitter.RunStarted()

	if ok, _ := loop.Push(item); !ok {
		emitter.RunError("会话已停止，无法处理本次请求")
		return
	}
	loop.Run(context.WithoutCancel(c.Request.Context()))

	state := loop.Wait()
	logExit(id, runID, state)
	maybeBackfillTitle(id, item.Query)
}

// maybeBackfillTitle 在会话标题为空时用首轮用户输入回填，便于侧边栏展示。
// 失败只记日志：标题是展示优化，不该影响已经成功的对话。
func maybeBackfillTitle(threadID, query string) {
	query = strings.TrimSpace(query)
	if query == "" {
		return
	}
	info, err := thread.SelectByID(threadID)
	if err != nil || info == nil || info.Title != "" {
		return
	}
	title := []rune(query)
	if len(title) > 30 {
		title = title[:30]
	}
	if err := thread.UpdateTitle(threadID, string(title)); err != nil {
		log.Printf("warn: 回填会话标题失败 thread=%s: %v", threadID, err)
	}
}

// buildChatItem 把请求体翻译成 TurnLoop 的队列条目。
//
// interrupts 是服务端记录的待确认中断（resume 路径下必非空，调用方已校验）。
func buildChatItem(req *bo.ChatRequest, interrupts []types.Interrupt) (*turnloop.ChatItem, error) {
	if req.IsResume() {
		// 一次只恢复一个中断点：当前 agent 只会产生单个根因中断
		// （见 turnloop.toAGUIInterrupts 的取值策略）。
		//
		// interruptId 取服务端快照而非客户端传入值：前者是 ADK 认的那个 ID，
		// 后者可能过期（页面停留期间 run 已被别的请求恢复）或被伪造。
		// 客户端只需要表达"批准还是拒绝"。
		entry := req.Resume[0]
		return &turnloop.ChatItem{
			InterruptID:      interrupts[0].ID,
			InterruptContent: resumeDecision(entry),
		}, nil
	}

	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return nil, errors.New("prompt 不能为空")
	}
	return &turnloop.ChatItem{Query: prompt}, nil
}

// resumeDecision 把 AG-UI 的 ResumeEntry 归一成工具侧 GetResumeContext[bool]
// 能读到的决定值。返回值必须非 nil——GenResume 用 InterruptContent != nil
// 来认定"这是一条审批条目"。
func resumeDecision(entry types.ResumeEntry) any {
	if entry.Status == types.ResumeStatusCancelled {
		return false
	}
	// resolved：优先用客户端给的 payload；没带 payload 视为同意。
	if b, ok := entry.Payload.(bool); ok {
		return b
	}
	if entry.Payload != nil {
		return entry.Payload
	}
	return true
}

// hasPendingInterrupt 判断会话是否存在待恢复的审批快照。
func hasPendingInterrupt(ctx context.Context, threadID string) (bool, error) {
	_, existed, err := checkpoint.GetInstance().Get(ctx, threadID)
	if err != nil {
		return false, err
	}
	return existed, nil
}

// loadPendingInterrupts 读出会话待确认的中断详情。
//
// 以 checkpoint 为准：checkpoint 不在就一律视为"无待确认中断"，即使
// thread_info 里还留着快照。两者本该同生同死，但 checkpoint 可能配了 TTL
// 过期、或 Redis 被清空，此时那条中断已经不可能 resume 了——返回它只会让
// 前端显示一张点了就报错的审批卡片。info 为 nil 时只做存在性判断。
func loadPendingInterrupts(ctx context.Context, threadID string, info *thread.ThreadInfo) ([]types.Interrupt, error) {
	pending, err := hasPendingInterrupt(ctx, threadID)
	if err != nil {
		return nil, err
	}
	if !pending || info == nil || len(info.PendingInterrupt) == 0 {
		return nil, nil
	}

	var interrupts []types.Interrupt
	if uErr := json.Unmarshal(info.PendingInterrupt, &interrupts); uErr != nil {
		// 快照坏了不该让接口挂掉：checkpoint 还在，用户仍可通过
		// 前端 SSE 里拿到的 interruptId 恢复。
		log.Printf("warn: 解析中断快照失败 thread=%s: %v", threadID, uErr)
		return nil, nil
	}
	return interrupts, nil
}

// logExit 把 loop 退出原因落日志。中断退出是预期路径，不记为错误。
func logExit(threadID, runID string, state *adk.TurnLoopExitState[*turnloop.ChatItem, *schema.AgenticMessage]) {
	if state == nil {
		return
	}
	if state.CheckpointErr != nil {
		log.Printf("[thread] 保存 checkpoint 失败 thread=%s run=%s: %v", threadID, runID, state.CheckpointErr)
	}
	if state.ExitReason != nil {
		log.Printf("[thread] run 结束 thread=%s run=%s reason=%v cause=%q interrupted=%d unhandled=%d",
			threadID, runID, state.ExitReason, state.StopCause,
			len(state.InterruptedItems), len(state.UnhandledItems))
		return
	}
	log.Printf("[thread] run 正常结束 thread=%s run=%s", threadID, runID)
}

// Render 渲染会话历史。POST /threads/render
//
// 返回 AG-UI 的消息数组，供前端 AGUIAdapter.convertHistoryMessages 还原气泡。
// 有待确认中断时连同中断详情一起返回，前端据此把审批卡片恢复出来——
// 这样刷新页面不会丢掉待审批状态。
func Render(c *gin.Context) {
	var req bo.ThreadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误: " + err.Error()})
		return
	}
	id := strings.TrimSpace(req.ThreadID)
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "threadId 不能为空"})
		return
	}
	// 整行读出来：下面要用 info.PendingInterrupt。
	info, err := thread.SelectByID(id)
	if err != nil {
		writeThreadLookupError(c, err)
		return
	}

	msgs, err := agui.RenderHistory(id)
	if err != nil {
		log.Printf("[thread] 渲染会话历史失败 thread=%s: %v", id, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "渲染会话历史失败"})
		return
	}

	interrupts, err := loadPendingInterrupts(c.Request.Context(), id, info)
	if err != nil {
		// 历史已经读到了，待审批状态读失败不该让整个接口挂掉。
		log.Printf("warn: 读取待确认中断失败 thread=%s: %v", id, err)
	}
	// 显式兜空切片，保证 JSON 是 [] 而不是 null。
	if interrupts == nil {
		interrupts = []types.Interrupt{}
	}

	c.JSON(http.StatusOK, gin.H{
		"threadId": id,
		"messages": msgs,
		// pendingApproval 保留为便捷布尔，与 interrupts 非空等价。
		"pendingApproval": len(interrupts) > 0,
		"interrupts":      interrupts,
		"running":         turnloop.IsRunning(id),
	})
}

// Abort 终止当前会话的运行。POST /threads/abort
func Abort(c *gin.Context) {
	id, ok := bindThreadID(c)
	if !ok {
		return
	}

	if !turnloop.Abort(id) {
		c.JSON(http.StatusOK, gin.H{"aborted": false, "message": "该会话当前没有运行中的任务"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"aborted": true})
}

// maxUploadBytes 单个上传文件的大小上限。
const maxUploadBytes = 20 << 20 // 20 MiB

// Upload 上传文档到会话工作区。POST /threads/docs
//
// multipart 表单：threadId 为会话 ID，file 为文件内容。
// 这里 threadId 走表单而非 JSON body——multipart 请求没法再带 JSON。
// 返回的 url 可直接填进下一次 /chat 的 attachments。
func Upload(c *gin.Context) {
	id := strings.TrimSpace(c.PostForm("threadId"))
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "threadId 不能为空"})
		return
	}
	if _, err := thread.SelectByID(id); err != nil {
		writeThreadLookupError(c, err)
		return
	}

	fh, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少上传文件（表单字段 file）"})
		return
	}
	if fh.Size > maxUploadBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{
			"error": fmt.Sprintf("文件超过 %d MiB 上限", maxUploadBytes>>20),
		})
		return
	}

	// 只取 basename 并换成新文件名，避免 ../ 穿越与同名覆盖。
	ext := filepath.Ext(filepath.Base(fh.Filename))
	stored := uuid.New().String() + ext
	dir := filepath.Join(workspaceRoot, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("[thread] 创建工作区失败 thread=%s: %v", id, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建工作区失败"})
		return
	}
	if err := c.SaveUploadedFile(fh, filepath.Join(dir, stored)); err != nil {
		log.Printf("[thread] 保存上传文件失败 thread=%s: %v", id, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存上传文件失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"name":      fh.Filename,
		"size":      fh.Size,
		"extension": strings.TrimPrefix(ext, "."),
		// TODO: 当前没有任何路由会 GET 这个地址（改路由前就是如此），
		// 下载能力待补：挂一个静态文件路由或对象存储直链。
		"url":        "/threads/docs/" + id + "/" + stored,
		"uploadedAt": time.Now().UTC().Format(time.RFC3339),
	})
}

// workspaceRoot 是会话上传文件的根目录，按 threadID 分子目录。
const workspaceRoot = "data/workspace"

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

// writeThreadLookupError 把查会话的错误翻译成 404 / 500。
func writeThreadLookupError(c *gin.Context, err error) {
	if errors.Is(err, thread.ErrThreadNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "会话不存在"})
		return
	}
	log.Printf("[thread] 查询会话失败: %v", err)
	c.JSON(http.StatusInternalServerError, gin.H{"error": "查询会话失败"})
}
