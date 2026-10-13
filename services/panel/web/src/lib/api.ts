import { ApiError } from "./errors"

type Method = "GET" | "POST" | "PATCH" | "PUT" | "DELETE"

let onUnauthorized: (() => void) | null = null

export function setUnauthorizedHandler(fn: () => void) {
  onUnauthorized = fn
}

async function request<T>(method: Method, url: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = { "X-Sylph-Panel": "1" }
  if (body !== undefined) headers["Content-Type"] = "application/json"
  let res: Response
  try {
    res = await fetch(url, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
      credentials: "same-origin",
    })
  } catch {
    throw new ApiError(0, "network", "", "Панель не отвечает — проверь соединение")
  }
  const text = await res.text()
  let data: unknown = null
  if (text) {
    try {
      data = JSON.parse(text)
    } catch {
      data = null
    }
  }
  if (!res.ok) {
    const e = (data ?? {}) as { code?: string; field?: string; message?: string }
    const err = new ApiError(res.status, e.code ?? "", e.field ?? "", e.message ?? text)
    if (res.status === 401 && e.code === "unauthorized") onUnauthorized?.()
    throw err
  }
  return data as T
}

export const core = {
  get: <T>(path: string) => request<T>("GET", `/api/core${path}`),
  post: <T>(path: string, body?: unknown) => request<T>("POST", `/api/core${path}`, body ?? {}),
  patch: <T>(path: string, body: unknown) => request<T>("PATCH", `/api/core${path}`, body),
  put: <T>(path: string) => request<T>("PUT", `/api/core${path}`),
  del: <T>(path: string) => request<T>("DELETE", `/api/core${path}`),
}

export const auth = {
  me: () => request<{ name: string }>("GET", "/auth/me"),
  login: (name: string, password: string, code: string) =>
    request<{ name: string }>("POST", "/auth/login", { name, password, code }),
  logout: () => request<{ ok: boolean }>("POST", "/auth/logout"),
}

export function qs(params: Record<string, string | number | boolean | null | undefined>): string {
  const p = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) {
    if (v !== null && v !== undefined && v !== "") p.set(k, String(v))
  }
  const s = p.toString()
  return s ? `?${s}` : ""
}
