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

export function formatBytes(value?: number) {
  if (!value) return "—"
  if (value < 1024) return `${value} B`
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`
  return `${(value / (1024 * 1024)).toFixed(1)} MB`
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
  review: "待处理",
  reviewing: "外部审核中",
  running: "执行中",
  failed: "失败",
  cancelled: "已取消",
  published: "已发布",
  draft: "草稿",
  superseded: "已替代",
  open: "未关闭",
  investigating: "调查中",
  replied: "已回复",
  resolved: "已关闭",
  banned: "已封禁",
  enabled: "启用",
  disabled: "停用",
  standard: "普通",
}
export const eventLabel: Record<string, string> = {
  checklist_saved: "保存清单",
  approved: "通过",
  rejected: "退回",
  assigned: "分配审核员",
  priority: "改优先级",
  submitted: "提交审核",
  created: "创建",
}
export const mediaRoleLabel: Record<string, string> = { preview: "预览图", icon: "图标", cover: "封面" }

export function targetLabel(value?: string) {
  switch ((value || "").toLowerCase()) {
    case "oronbox":
      return "OronBox"
    case "bandbbs":
      return "米坛"
    case "astrobox":
      return "AstroBox"
    default:
      return value || "—"
  }
}

const statusClass: Record<string, string> = {
  pending: "warn",
  review: "warn",
  submitted: "warn",
  reviewing: "warn",
  running: "warn",
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
  return <span className={`badge ${statusClass[value] || ""}`}>{label || stateLabel[value] || value || "—"}</span>
}

export function TargetChips({ targets }: { targets?: string[] | null }) {
  if (!targets?.length) return <span className="chip">未配置目标</span>
  return (
    <span className="chip-row">
      {targets.map((target) => (
        <span className="chip" key={target}>
          {targetLabel(target)}
        </span>
      ))}
    </span>
  )
}

export type PlanItem = { target?: string; config?: Record<string, any> }

function planItems(plan: unknown, targets?: string[]): PlanItem[] {
  if (Array.isArray(plan) && plan.length) return plan as PlanItem[]
  return (targets || []).map((target) => ({ target, config: {} }))
}

export function PublicationCards({ plan, targets }: { plan?: unknown; targets?: string[] }) {
  const items = planItems(plan, targets)
  if (!items.length) {
    return (
      <div className="panel">
        <h3>发布目标</h3>
        <p className="hint">仅发布到 OronBox，不同步外部平台。</p>
      </div>
    )
  }
  return (
    <div className="panel">
      <h3>发布目标</h3>
      <p className="hint">审核通过后会按这里的平台入队。米坛要看板块，AstroBox 要看是否同步。</p>
      <div className="plan-grid">
        {items.map((item, index) => {
          const target = item.target || ""
          const config = item.config || {}
          const bandTargets = Array.isArray(config.targets) ? config.targets : []
          return (
            <article className="plan-card" key={`${target}-${index}`}>
              <div className="plan-card-title">{targetLabel(target)}</div>
              {target === "bandbbs" ? (
                <dl className="kv">
                  <div>
                    <dt>发布协议</dt>
                    <dd>{config.agreement ? "已确认" : "未确认"}</dd>
                  </div>
                  <div>
                    <dt>发布板块</dt>
                    <dd>
                      {bandTargets.length ? (
                        bandTargets.map((row: Record<string, unknown>, rowIndex: number) => (
                          <div key={rowIndex} className="hint">
                            分类 {String(row.category_id ?? "—")}
                            {row.prefix_id ? ` · 前缀 ${String(row.prefix_id)}` : ""}
                            {row.package_id ? ` · ${String(row.package_id)}` : ""}
                          </div>
                        ))
                      ) : (
                        <span className="overdue">没有配置米坛目标板块</span>
                      )}
                    </dd>
                  </div>
                </dl>
              ) : target === "astrobox" ? (
                <dl className="kv">
                  <div>
                    <dt>同步平台</dt>
                    <dd>AstroBox</dd>
                  </div>
                  {config.item_id || config.repo_name ? (
                    <div>
                      <dt>仓库</dt>
                      <dd>
                        {[config.repo_owner, config.repo_name, config.item_id].filter(Boolean).join(" / ") || "新建条目"}
                      </dd>
                    </div>
                  ) : (
                    <div>
                      <dt>条目</dt>
                      <dd>审核通过后创建或更新</dd>
                    </div>
                  )}
                </dl>
              ) : (
                <dl className="kv">
                  <div>
                    <dt>发布位置</dt>
                    <dd>OronBox 主资源库</dd>
                  </div>
                  <div>
                    <dt>状态</dt>
                    <dd>审核通过后进入发布队列</dd>
                  </div>
                </dl>
              )}
            </article>
          )
        })}
      </div>
    </div>
  )
}

export function Pagination({ page, total, perPage, onChange }: { page: number; total: number; perPage: number; onChange: (page: number) => void }) {
  const pages = Math.max(1, Math.ceil((total || 0) / perPage))
  if (!total) return <div className="pager">共 0 条</div>
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

export function PageHeader({ title, hint, children }: { title?: string; hint?: string; children?: ReactNode }) {
  return (
    <header className="page-head">
      <div>{hint ? <p>{hint}</p> : title ? <p>{title}</p> : null}</div>
      <div className="page-head-actions">{children}</div>
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

export function FieldList({ row, prefer }: { row: Record<string, unknown> | null; prefer?: string[] }) {
  if (!row) return null
  const keys = prefer?.filter((key) => row[key] !== undefined) || Object.keys(row).filter((key) => typeof row[key] !== "object")
  return (
    <dl className="kv">
      {keys.map((key) => (
        <div key={key}>
          <dt>{key}</dt>
          <dd>{row[key] === null || row[key] === undefined ? "—" : typeof row[key] === "object" ? JSON.stringify(row[key]) : String(row[key])}</dd>
        </div>
      ))}
    </dl>
  )
}
