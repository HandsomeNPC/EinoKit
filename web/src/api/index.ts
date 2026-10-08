/**
 * 后端 REST 接口封装。
 *
 * 只覆盖非流式接口：对话流(POST /api/threads/chat)由 ChatEngine 的 SSE
 * 通道直接接管，不在这里重复实现。
 *
 * 所有会话级接口都是 POST + 请求体里的 threadId，不走 URL 路径参数——
 * 这样 SSE endpoint 是静态的，切会话时不必 updateEndpoint。
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

/** 后端基址。开发期由 vite 代理 rewrite 掉 /api，生产由网关承担。 */
const BASE = '/api'

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

/** POST JSON 的快捷方式——会话级接口清一色是这个形状。 */
function post<T>(path: string, body: unknown): Promise<T> {
    return request<T>(`${BASE}${path}`, {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify(body),
    })
}

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
 * 上传文档到会话工作区。
 *
 * multipart 请求没法再带 JSON body，所以 threadId 走表单字段；
 * 文件字段名必须是 file(后端 c.FormFile("file"))。
 */
export function uploadDoc(
    threadId: string,
    file: File,
): Promise<UploadResponse> {
    const form = new FormData()
    form.append('threadId', threadId)
    form.append('file', file)
    // 不手写 Content-Type：浏览器要自己补 multipart 的 boundary。
    return request<UploadResponse>(`${BASE}/threads/docs`, {
        method: 'POST',
        body: form,
    })
}

/**
 * 对话流地址。交给 ChatEngine 作为 SSE endpoint。
 *
 * 是个常量：threadId 走请求体，切会话不需要换 endpoint。
 */
export const CHAT_ENDPOINT = `${BASE}/threads/chat`
