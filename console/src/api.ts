export type Session = {
  user: string
  role: string
  csrf_token: string
  pending_reviews: number
}

export type List<T> = { items: T[]; total: number; page: number; per_page: number; total_pages?: number }

let csrf = ""

function headers(jsonBody = false, extra?: HeadersInit) {
  const value = new Headers(extra)
  if (csrf) value.set("X-CSRF-Token", csrf)
  if (jsonBody) value.set("Content-Type", "application/json")
  return value
}

async function parse(response: Response) {
  if (response.status === 401) {
    window.location.href = "/admin/login"
    throw new Error("unauthenticated")
  }
  const data = await response.json().catch(() => ({}))
  if (!response.ok) throw new Error((data as { message?: string }).message || "请求失败")
  return data
}

export async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const jsonBody = typeof init?.body === "string"
  const response = await fetch(path, { ...init, headers: headers(jsonBody, init?.headers), credentials: "same-origin" })
  return parse(response) as Promise<T>
}

export async function upload<T>(path: string, body: FormData): Promise<T> {
  const sep = path.includes("?") ? "&" : "?"
  const response = await fetch(`${path}${sep}format=json`, {
    method: "POST",
    headers: headers(false, { Accept: "application/json" }),
    body,
    credentials: "same-origin",
  })
  return parse(response) as Promise<T>
}

export function qs(params: Record<string, string | number | undefined>) {
  const search = new URLSearchParams()
  Object.entries(params).forEach(([key, value]) => {
    if (value !== undefined && value !== "") search.set(key, String(value))
  })
  const query = search.toString()
  return query ? `?${query}` : ""
}

export async function loadSession(): Promise<Session> {
  const session = await request<Session>("/admin/api/session")
  csrf = session.csrf_token
  return session
}

export function logout() {
  return fetch("/admin/logout", { method: "POST", headers: csrf ? { "X-CSRF-Token": csrf } : undefined, credentials: "same-origin" })
}

export const api = {
  list: <T>(path: string, params: Record<string, string | number | undefined> = {}) => request<List<T>>(`${path}${qs(params)}`),
  get: <T>(path: string) => request<T>(path),
  post: <T>(path: string, body: unknown = {}) => request<T>(path, { method: "POST", body: JSON.stringify(body) }),
  upload,
}

export function decideReview(id: string, decision: "approve" | "reject", note = "", grade = "", items: string[] = [], attributes: string[] = []) {
  return api.post(`/admin/api/reviews/${id}/decision`, { decision, note, grade, items, attributes })
}

export function saveReviewChecklist(id: string, items: string[]) {
  return api.post(`/admin/api/reviews/${id}/checklist`, { items })
}

export function bulkReviews(body: { action: string; ids: string[]; reviewer_id?: string; priority?: number; note?: string; grade?: string }) {
  return api.post("/admin/api/reviews/bulk", body)
}

export function saveResourceDraft(id: string, body: Record<string, unknown>) {
  return request<{ ok: boolean; revision_id: string }>(`/admin/api/resources/${id}/draft`, { method: "POST", body: JSON.stringify(body) })
}

export function submitResourceDraft(id: string, revision: string) {
  return api.post(`/admin/api/resources/${id}/draft/${revision}/submit`)
}

export function decideComment(id: string, action: string, note = "") {
  return api.post(`/admin/api/comments/${id}`, { action, note })
}

export function setUserState(id: string, action: string, reason = "", role = "") {
  return api.post(`/admin/api/users/${encodeURIComponent(id)}/state`, { action, reason, role })
}

export function replyTicket(id: string, status: string, reply = "") {
  return api.post(`/admin/api/tickets/${encodeURIComponent(id)}`, { status, reply })
}

export function reviewPlugin(id: string, decision: "approve" | "reject", note = "") {
  return api.post(`/admin/api/plugins/${encodeURIComponent(id)}`, { decision, note })
}

export function retryPublication(id: string) {
  return api.post(`/admin/api/publications/${encodeURIComponent(id)}`, { action: "requeue" })
}

export type ResourceItem = Record<string, unknown> & { id: string; name?: string; slug?: string; kind?: string; owner?: string; moderation?: string; revision_name?: string; revision_number?: number; review_state?: string; updated_at?: string; targets?: string[] }

export function listResources(q = "", kind = "", state = "", page = 1, target = "") {
  return api.list<ResourceItem>("/admin/api/resources", { q, kind, state, target, page, per_page: 25 })
}

export type CommentItem = { id: string; username: string; body: string; state: string; resource_id?: string; created_at?: string }

export function listComments(state = "review", q = "", page = 1) {
  return api.list<CommentItem>("/admin/api/comments", { state, q, page })
}

export function listReviews(state = "pending", kind = "", extra: Record<string, string | number | undefined> = {}) {
  return api.list("/admin/api/reviews", { state, kind, per_page: 40, ...extra })
}

export function getReview(id: string) {
  return api.get(`/admin/api/reviews/${id}`)
}

export function getResource(id: string) {
  return api.get<{
    name?: string
    summary?: string
    paid_type?: string
    revision_id?: string
    attributes?: string[]
    can_submit?: boolean
    pending?: boolean
    [key: string]: unknown
  }>(`/admin/api/resources/${id}`)
}
