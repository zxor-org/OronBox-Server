import { useEffect, useRef, useState } from "react"
import { api } from "../api"
import { Status, formatBytes, formatRelative } from "../ui"

export type Row = Record<string, any>
export type SystemColumn = { key: string; label: string; format?: "bytes" | "date" | "status" | "boolean" }
export type SystemConfig = {
  title: string
  hint: string
  path: string
  columns: SystemColumn[]
  searchPlaceholder?: string
  detailPath?: (row: Row) => string
}

export function systemCell(row: Row, column: SystemColumn) {
  let value = row[column.key]
  if (column.key === "status" && (value === null || value === undefined || value === "") && (row.used_at !== undefined || row.expires_at !== undefined)) {
    const expiresAt = Date.parse(String(row.expires_at))
    value = row.used_at ? "used" : Number.isFinite(expiresAt) ? expiresAt <= Date.now() ? "expired" : "active" : ""
  }
  if (value === null || value === undefined || value === "") return "—"
  if (column.format === "bytes") return formatBytes(Number(value))
  if (column.format === "date") return formatRelative(String(value))
  if (column.format === "status") return <Status value={String(value)} />
  if (column.format === "boolean") return value ? "是" : "否"
  if (column.key === "target" && typeof value === "object") {
    const target = value as Row
    return [target.label || target.type, target.id].filter(Boolean).join(" / ") || "—"
  }
  return typeof value === "object" ? JSON.stringify(value) : String(value)
}

export function detailRecord(value: Row | null) {
  if (!value) return null
  for (const key of ["blob", "event", "state", "ticket", "stats", "collection", "plugin"]) {
    const candidate = value[key]
    if (candidate && typeof candidate === "object" && !Array.isArray(candidate)) return candidate as Row
  }
  return value
}

type HealthSnapshot = {
  database_size_bytes?: number
  database_sessions?: number
  publications?: Record<string, number>
  blobs?: Record<string, number>
  oauth?: Record<string, number>
}

function healthNumber(value: unknown) {
  return typeof value === "number" ? value.toLocaleString("zh-CN") : "—"
}

export function HealthDiagnostics({ value }: { value: unknown }) {
  if (!value || typeof value !== "object") return null
  const snapshot = value as HealthSnapshot
  const publications = snapshot.publications || {}
  const blobs = snapshot.blobs || {}
  const oauth = snapshot.oauth || {}
  return (
    <section className="diagnostics" aria-label="运行诊断">
      <div className="diagnostics-head">
        <div>
          <h2>运行诊断</h2>
          <p>数据库、发布队列、文件副本和授权事件的实时快照</p>
        </div>
      </div>
      <div className="diagnostics-grid">
        <section className="diagnostic-card">
          <h3>数据库</h3>
          <div className="diagnostic-metrics">
            <div><span>占用空间</span><strong>{formatBytes(snapshot.database_size_bytes)}</strong></div>
            <div><span>连接数</span><strong>{healthNumber(snapshot.database_sessions)}</strong></div>
          </div>
        </section>
        <section className="diagnostic-card">
          <h3>发布队列</h3>
          <div className="diagnostic-metrics">
            <div><span>待处理</span><strong>{healthNumber(publications.pending)}</strong></div>
            <div><span>执行中</span><strong>{healthNumber(publications.running)}</strong></div>
            <div><span>失败</span><strong className={publications.failed ? "danger-text" : ""}>{healthNumber(publications.failed)}</strong></div>
            <div><span>超时运行</span><strong className={publications.stale_running ? "danger-text" : ""}>{healthNumber(publications.stale_running)}</strong></div>
          </div>
        </section>
        <section className="diagnostic-card">
          <h3>Blob 存储</h3>
          <div className="diagnostic-metrics">
            <div><span>文件数</span><strong>{healthNumber(blobs.count)}</strong></div>
            <div><span>总大小</span><strong>{formatBytes(blobs.size_bytes)}</strong></div>
            <div><span>待补副本</span><strong className={blobs.replica_missing ? "danger-text" : ""}>{healthNumber(blobs.replica_missing)}</strong></div>
            <div><span>副本失败</span><strong className={blobs.replica_failed ? "danger-text" : ""}>{healthNumber(blobs.replica_failed)}</strong></div>
          </div>
        </section>
        <section className="diagnostic-card">
          <h3>OAuth（24 小时）</h3>
          <div className="diagnostic-metrics">
            <div><span>事件</span><strong>{healthNumber(oauth.events_24_hours)}</strong></div>
            <div><span>失败</span><strong className={oauth.failures_24_hours ? "danger-text" : ""}>{healthNumber(oauth.failures_24_hours)}</strong></div>
            <div><span>失败率</span><strong>{typeof oauth.failure_rate === "number" ? `${oauth.failure_rate.toFixed(1)}%` : "—"}</strong></div>
          </div>
        </section>
      </div>
    </section>
  )
}

export function useRows(path: string, extra: Record<string, string | number | undefined> = {}) {
  const [items, setItems] = useState<Row[]>([])
  const [total, setTotal] = useState(0)
  const [payload, setPayload] = useState<Record<string, unknown>>({})
  const [q, setQ] = useState("")
  const [page, setPage] = useState(1)
  const [error, setError] = useState("")
  const [loading, setLoading] = useState(true)
  const requestSequence = useRef(0)
  const load = async (search = q, next = page) => {
    const requestID = ++requestSequence.current
    setLoading(true)
    setError("")
    try {
      const data = await api.list<Row>(path, { q: search, page: next, per_page: 25, ...extra })
      if (requestID !== requestSequence.current) return
      setItems(data.items || [])
      setTotal(data.total || 0)
      setPayload(data as unknown as Record<string, unknown>)
    } catch (err) {
      if (requestID === requestSequence.current) setError((err as Error).message)
    } finally {
      if (requestID === requestSequence.current) setLoading(false)
    }
  }
  useEffect(() => {
    setQ("")
    setPage(1)
    setItems([])
    setTotal(0)
    setPayload({})
    setError("")
  }, [path])
  useEffect(() => {
    void load(q, page)
  }, [path, page])
  return { items, total, payload, q, setQ, page, setPage, error, loading, load }
}