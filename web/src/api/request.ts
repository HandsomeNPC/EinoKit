/**
 * 请求通用方法。
 *
 * 后端基址与 JSON 请求的快捷封装，供 api/index.ts 的各业务接口复用。
 */

/** 后端基址。开发期由 vite 代理 rewrite 掉 /api，生产由网关承担。 */
export const BASE = '/api'

/**
 * 发起请求并解析 JSON。
 *
 * 请求失败直接返回 undefined，不抛错——错误处理交给调用方按需判断返回值。
 */
export async function request<T>(
    url: string,
    init?: RequestInit,
): Promise<T> {
    const resp = await fetch(url, init)
    if (!resp.ok) return undefined as T
    // 204 没有响应体，JSON 解析会炸。
    if (resp.status === 204) return undefined as T
    try {
        return (await resp.json()) as T
    } catch {
        return undefined as T
    }
}

/** POST JSON 的快捷方式——会话级接口清一色是这个形状。 */
export function post<T>(path: string, body: unknown): Promise<T> {
    return request<T>(`${BASE}${path}`, {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify(body),
    })
}
