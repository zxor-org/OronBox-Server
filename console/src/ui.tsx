import { FormEvent, ReactNode, useEffect, useState } from "react"

type Toast = { id: number; text: string; tone: "ok" | "err" }

let nextId = 1
const listeners = new Set<(items: Toast[]) => void>()
let toasts: Toast[] = []

function emit() {
  listeners.forEach((listener) => listener(toasts))
}

export function toast(text: string, tone: "ok" | "err" = "ok") {
  const item = { id: nextId++, text, tone }
  toasts = [...toasts, item]
  emit()
  window.setTimeout(() => {
    toasts = toasts.filter((toastItem) => toastItem.id !== item.id)
    emit()
  }, 2800)
}

export function ToastHost() {
  const [items, setItems] = useState<Toast[]>([])
  useEffect(() => {
    listeners.add(setItems)
    setItems(toasts)
    return () => {
      listeners.delete(setItems)
    }
  }, [])
  if (!items.length) return null
  return (
    <div className="toasts">
      {items.map((item) => (
        <div key={item.id} className={`toast ${item.tone}`}>
          {item.text}
        </div>
      ))}
    </div>
  )
}

export function formatRelative(value?: string) {
  if (!value) return "—"
  const time = new Date(value).getTime()
  if (Number.isNaN(time)) return value
  const delta = Date.now() - time
  const minute = 60 * 1000
  const hour = 60 * minute
  const day = 24 * hour
  if (delta < minute) return "刚刚"
  if (delta < hour) return `${Math.floor(delta / minute)} 分钟前`
  if (delta < day) return `${Math.floor(delta / hour)} 小时前`
  if (delta < 7 * day) return `${Math.floor(delta / day)} 天前`
  return new Date(value).toLocaleString("zh-CN", { month: "numeric", day: "numeric", hour: "2-digit", minute: "2-digit" })
}

const statusClass: Record<string, string> = {
  pending: "warn",
  review: "warn",
  submitted: "warn",
  failed: "danger",
  rejected: "danger",
  hidden: "danger",
  banned: "danger",
  frozen: "warn",
  suspended: "danger",
  visible: "ok",
  approved: "ok",
  listed: "ok",
  resolved: "ok",
  replied: "ok",
  featured: "ok",
  enabled: "ok",
  published: "ok",
}

export function Status({ value, label }: { value: string; label?: string }) {
  return <span className={`badge ${statusClass[value] || ""}`}>{label || value || "—"}</span>
}

export function Pagination({ page, total, perPage, onChange }: { page: number; total: number; perPage: number; onChange: (page: number) => void }) {
  const pages = Math.max(1, Math.ceil(total / perPage))
  if (total <= perPage) return null
  return (
    <div className="pager">
      <button className="btn" type="button" disabled={page <= 1} onClick={() => onChange(page - 1)}>
        上一页
      </button>
      <span>
        {page} / {pages} · 共 {total}
      </span>
      <button className="btn" type="button" disabled={page >= pages} onClick={() => onChange(page + 1)}>
        下一页
      </button>
    </div>
  )
}

export function SearchForm({ value, onChange, onSubmit, placeholder }: { value: string; onChange: (value: string) => void; onSubmit: () => void; placeholder: string }) {
  return (
    <form
      onSubmit={(event: FormEvent) => {
        event.preventDefault()
        onSubmit()
      }}
    >
      <input className="search" value={value} onChange={(event) => onChange(event.target.value)} placeholder={placeholder} />
    </form>
  )
}

export function PageHeader({ title, hint, children }: { title: string; hint?: string; children?: ReactNode }) {
  return (
    <header className="page-head">
      <div>
        <h1>{title}</h1>
        {hint ? <p>{hint}</p> : null}
      </div>
      {children}
    </header>
  )
}

export function Empty({ children }: { children: ReactNode }) {
  return <div className="empty">{children}</div>
}

export function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="field">
      <span>{label}</span>
      {children}
    </label>
  )
}

export function Dialog({
  open,
  title,
  hint,
  children,
  onClose,
  footer,
  wide,
}: {
  open: boolean
  title: string
  hint?: string
  children?: ReactNode
  onClose: () => void
  footer?: ReactNode
  wide?: boolean
}) {
  useEffect(() => {
    if (!open) return
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose()
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [open, onClose])
  if (!open) return null
  return (
    <div className="dialog-back" onClick={onClose}>
      <div className={`dialog ${wide ? "wide" : ""}`} role="dialog" aria-modal="true" onClick={(event) => event.stopPropagation()}>
        <div className="dialog-head">
          <div>
            <h2>{title}</h2>
            {hint ? <p className="hint">{hint}</p> : null}
          </div>
          <button className="btn" type="button" onClick={onClose}>
            关闭
          </button>
        </div>
        <div className="dialog-body">{children}</div>
        {footer ? <div className="dialog-foot">{footer}</div> : null}
      </div>
    </div>
  )
}

export function Tabs({ value, onChange, items }: { value: string; onChange: (value: string) => void; items: { id: string; label: string }[] }) {
  return (
    <div className="tabs">
      {items.map((item) => (
        <button key={item.id} type="button" className={item.id === value ? "active" : ""} onClick={() => onChange(item.id)}>
          {item.label}
        </button>
      ))}
    </div>
  )
}

export const kindLabel: Record<string, string> = { watchface: "表盘", app: "应用", quickapp: "快应用" }
export const paidLabel: Record<string, string> = { free: "免费", paid: "付费", force_paid: "强制付费" }
export const stateLabel: Record<string, string> = {
  visible: "展示中",
  hidden: "已隐藏",
  frozen: "已冻结",
  suspended: "已下架",
  pending: "待审核",
  approved: "已通过",
  rejected: "已退回",
  submitted: "已提交",
  listed: "已上架",
  featured: "精选",
}
