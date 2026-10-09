import type {
    AbortResponse,
    RenderResponse,
    ThreadInfo,
} from './types'
import {BASE, post, request} from './request'

/** 创建会话。两个入参都可省略：标题留空由首轮提问回填。 */
export function createThread(params?: {
    title?: string
    agentType?: string
}): Promise<ThreadInfo> {
    return post<ThreadInfo>('/threads', params ?? {})
}

/** 列出全部会话。 */
export function listThreads(): Promise<ThreadInfo[]> {
    return request<ThreadInfo[]>(`${BASE}/threads`)
}

/** 删除会话。后端返回 204，无响应体。 */
export function deleteThread(threadId: string): Promise<void> {
    return post<void>('/threads/delete', {threadId})
}

/** 渲染会话历史(含待审批状态与运行状态)。 */
export function renderThread(threadId: string): Promise<RenderResponse> {
    return post<RenderResponse>('/threads/render', {threadId})
}

/** 终止会话当前运行。 */
export function abortThread(threadId: string): Promise<AbortResponse> {
    return post<AbortResponse>('/threads/abort', {threadId})
}

/**
 * 对话流地址。交给 ChatEngine 作为 SSE endpoint。
 */
export const CHAT_ENDPOINT = `${BASE}/threads/chat`
