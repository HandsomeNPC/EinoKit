/**
 * 后端 REST 接口封装。
 *
 * 只覆盖非流式接口：对话流(POST /threads/:id/chat)由 ChatEngine 的 SSE
 * 通道直接接管(见 src/chat/service.ts)，不在这里重复实现。
 */
import type {
    AbortResponse,
    RenderResponse,
    ThreadInfo,
    UploadResponse,
} from './types'

/** 后端返回的错误体统一是 `{"error": "..."}`。 */
interface ErrorBody {
    error?: string
}

/** 带状态码的请求错误，便于上层区分 404/409。 */
export class ApiError extends Error {
    readonly status: number

    constructor(status: number, message: string) {
        super(message)
        this.name = 'ApiError'
        this.status = status
    }
}

/**
 * 发起请求并解析 JSON。
 *
 * 失败时抛 ApiError 并尽量带上后端的 error 文案——解析失败就退回状态码，
 * 避免把一句 "Unexpected token" 当成业务错误抛给用户。
 */
async function request<T>(url: string, init?: RequestInit): Promise<T> {
    const resp = await fetch(url, init)

    if (!resp.ok) {
        let message = `请求失败(${resp.status})`
        try {
            const body = (await resp.json()) as ErrorBody
            if (body?.error) message = body.error
        } catch {
            // 响应体不是 JSON(如网关返回的 HTML)，保留默认文案。
        }
        throw new ApiError(resp.status, message)
    }

    // 204 没有响应体，JSON 解析会炸。
    if (resp.status === 204) return undefined as T
    return (await resp.json()) as T
}

const JSON_HEADERS = {'Content-Type': 'application/json'}

/** 创建会话。两个入参都可省略：标题留空由首轮提问回填。 */
export function createThread(params?: {
    title?: string
    agentType?: string
}): Promise<ThreadInfo> {
    return request<ThreadInfo>('/threads', {
        method: 'POST',
        headers: JSON_HEADERS,
        body: JSON.stringify(params ?? {}),
    })
}

/** 列出全部会话。 */
export function listThreads(): Promise<ThreadInfo[]> {
    return request<ThreadInfo[]>('/threads')
}

/** 删除会话。后端返回 204，无响应体。 */
export function deleteThread(threadId: string): Promise<void> {
    return request<void>(`/threads/${encodeURIComponent(threadId)}`, {
        method: 'DELETE',
    })
}

/** 渲染会话历史(含待审批状态与运行状态)。 */
export function renderThread(threadId: string): Promise<RenderResponse> {
    return request<RenderResponse>(
        `/threads/${encodeURIComponent(threadId)}/render`,
    )
}

/** 终止会话当前运行。 */
export function abortThread(threadId: string): Promise<AbortResponse> {
    return request<AbortResponse>(
        `/threads/${encodeURIComponent(threadId)}/abort`,
        {method: 'POST'},
    )
}

/** 上传文档到会话工作区。表单字段名必须是 file(后端 c.FormFile("file"))。 */
export function uploadDoc(
    threadId: string,
    file: File,
): Promise<UploadResponse> {
    const form = new FormData()
    form.append('file', file)
    // 不手写 Content-Type：浏览器要自己补 multipart 的 boundary。
    return request<UploadResponse>(
        `/threads/${encodeURIComponent(threadId)}/docs`,
        {method: 'POST', body: form},
    )
}

/** 对话流地址。交给 ChatEngine 作为 SSE endpoint。 */
export function chatEndpoint(threadId: string): string {
    return `/threads/${encodeURIComponent(threadId)}/chat`
}
