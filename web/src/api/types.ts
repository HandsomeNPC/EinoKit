/**
 * 与后端对齐的接口类型。
 *
 * 命名与字段严格对齐 Go 侧：
 *   - ThreadInfo        ← internal/memory/thread.ThreadInfo
 *   - ChatRequest       ← internal/model/bo.ChatRequest
 *   - ResumeEntry       ← ag-ui types.ResumeEntry
 *   - AGUIHistoryMessage← ag-ui types.Message(由 /render 返回)
 */

/** 会话元信息。GET/POST /threads 的返回体。 */
export interface ThreadInfo {
    threadId: string
    title: string
    agentType: string
    createdAt: string
    updatedAt: string
}

/**
 * 中断恢复条目。对齐 ag-ui 的 ResumeEntry：
 * 批准 `{ status: 'resolved', payload: true }`，拒绝 `{ status: 'cancelled' }`。
 *
 * interruptId 由后端用 thread_info 里的中断快照回填，前端不必传——
 * 服务端只认自己记录的那个 ID，客户端传了也会被忽略。
 *
 * 后端 resumeDecision 把 cancelled 归一成 false、resolved 无 payload 归一成 true，
 * 所以拒绝时不必显式传 payload。
 */
export type ResumeStatus = 'resolved' | 'cancelled'

export interface ResumeEntry {
    status: ResumeStatus
    payload?: unknown
    metadata?: Record<string, unknown>
}

/** 附件快照。对齐 bo.AttachmentItem 与 ChatEngine 的 AttachmentItem。 */
export interface AttachmentItem {
    fileType: 'image' | 'video' | 'audio' | 'pdf' | 'doc' | 'ppt' | 'txt'
    size?: number
    name?: string
    url?: string
    isReference?: boolean
    width?: number
    height?: number
    extension?: string
    metadata?: Record<string, unknown>
}

/**
 * POST /api/threads/chat 的请求体。
 *
 * threadId 必填；prompt 与 resume 互斥(一次请求只做一件事)。
 */
export interface ChatRequestBody {
    threadId: string
    prompt?: string
    messageID?: string
    attachments?: AttachmentItem[]
    resume?: ResumeEntry[]
}

/**
 * /render 返回的历史消息。
 *
 * 原样喂给 `AGUIAdapter.convertHistoryMessages`，这里只声明用得到的字段，
 * 不复刻整个 AG-UI 消息联合类型。
 */
export interface AGUIHistoryMessage {
    id: string
    role: 'system' | 'user' | 'assistant' | 'tool' | 'reasoning' | 'developer' | 'activity'
    content?: unknown
    toolCalls?: unknown[]
    toolCallId?: string

    [key: string]: unknown
}

/** POST /api/threads/render 的返回体。 */
export interface RenderResponse {
    threadId: string
    messages: AGUIHistoryMessage[]
    /** 是否存在待确认的审批。与 `interrupts.length > 0` 等价。 */
    pendingApproval: boolean
    /**
     * 待确认的中断详情。
     *
     * 后端只在 checkpoint 仍然有效时才返回（见 loadPendingInterrupts），
     * 所以这里非空就一定能 resume。前端据此在刷新页面后恢复审批卡片。
     */
    interrupts: AGUIInterrupt[]
    running: boolean
}

/** POST /api/threads/abort 的返回体。 */
export interface AbortResponse {
    aborted: boolean
    message?: string
}

/**
 * 一个待用户确认的中断。
 *
 * 来自 RUN_FINISHED 事件的 `outcome.interrupts[]`(ag-ui Go SDK 的
 * WithInterruptOutcome 写入)，批准/拒绝时把 `id` 原样回填到
 * ResumeEntry.interruptId。
 *
 * toolCallId 由后端从中断地址里的 tool 段解析而来，指向触发这次中断的
 * tool call。前端据此把中断关联回对应的 tool call 块，复用该工具已注册的
 * 渲染组件——这样新增需要审批的工具时，前端不需要任何改动。
 */
export interface AGUIInterrupt {
    id: string
    reason?: string
    message?: string
    toolCallId?: string
    responseSchema?: Record<string, unknown>
    expiresAt?: string
    subagentRunId?: string
    metadata?: Record<string, unknown>
}

/** RUN_FINISHED 的 outcome 字段。type 为 'interrupt' 时 interrupts 非空。 */
export interface RunFinishedOutcome {
    type: 'success' | 'interrupt'
    interrupts?: AGUIInterrupt[]
}
