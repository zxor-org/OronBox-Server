import { FormEvent, useEffect, useRef, useState } from "react"
import { useLocation, useNavigate, useParams } from "react-router"
import { api } from "../api"
import { Dialog, Empty, Field, FieldList, PageHeader, Pagination, SearchForm, Status, TableState, formatBytes, formatRelative, toast } from "../ui"

type Row = Record<string, any>
type SystemColumn = { key: string; label: string; format?: "bytes" | "date" | "status" | "boolean" }
type SystemConfig = {
  title: string
  hint: string
  path: string
  columns: SystemColumn[]
  searchPlaceholder?: string
  detailPath?: (row: Row) => string
}

function systemCell(row: Row, column: SystemColumn) {
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

function detailRecord(value: Row | null) {
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

function HealthDiagnostics({ value }: { value: unknown }) {
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

function useRows(path: string, extra: Record<string, string | number | undefined> = {}) {
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

export function CollectionsPage() {
  const { id } = useParams()
  const navigate = useNavigate()
  const { items, total, q, setQ, page, setPage, error, loading, load } = useRows("/admin/api/collections")
  const [detail, setDetail] = useState<Row | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)
  const [detailError, setDetailError] = useState("")
  const [name, setName] = useState("")
  const [summary, setSummary] = useState("")
  useEffect(() => {
    if (!id) {
      setDetail(null)
      setDetailError("")
      setDetailLoading(false)
      return
    }
    let active = true
    setDetail(null)
    setDetailError("")
    setDetailLoading(true)
    api.get<Row>(`/admin/api/collections/${encodeURIComponent(id)}`).then((value) => {
      if (!active) return
      setDetail(value)
      setName(value.collection?.latest_revision_name || value.collection?.name || "")
      setSummary(value.collection?.latest_revision_summary || "")
    }).catch((err: Error) => {
      if (active) setDetailError(err.message)
    }).finally(() => {
      if (active) setDetailLoading(false)
    })
    return () => {
      active = false
    }
  }, [id])
  return (
    <>
      <PageHeader title="合集" hint="点进合集后可以改名称和成员，提交后进入合集审核。">
        <SearchForm value={q} onChange={setQ} onSubmit={() => { setPage(1); load(q, 1) }} placeholder="搜索合集" />
      </PageHeader>
      <div className="table-wrap">
        <TableState loading={loading} error={error} onRetry={() => void load()} isEmpty={!items.length} empty={q ? "没有匹配的合集" : "没有合集"}>
          <table>
            <thead>
              <tr>
                <th>名称</th>
                <th>作者</th>
                <th>类型</th>
                <th>成员</th>
                <th>状态</th>
                <th>更新</th>
              </tr>
            </thead>
            <tbody>
              {items.map((item) => (
                <tr key={item.id} className="clickable" onClick={() => navigate(`/collections/${item.id}`)}>
                  <td><strong>{item.name || item.slug}</strong><small>{item.slug}</small></td>
                  <td>{item.owner || item.owner_id || "—"}</td>
                  <td>{item.kind || item.platform || "—"}</td>
                  <td>{item.member_count ?? "—"}</td>
                  <td><Status value={item.state} /></td>
                  <td>{formatRelative(item.updated_at)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </TableState>
      </div>
      <Pagination page={page} total={total} perPage={25} onChange={setPage} />
      {id && detailLoading && !detail ? <div className="panel-surface detail-placeholder">加载合集详情</div> : null}
      {id && detailError && !detail ? <div className="panel-surface detail-placeholder error">{detailError}</div> : null}
      {detail && (
        <div className="page-body">
          <form
            className="stack"
            onSubmit={(event: FormEvent) => {
              event.preventDefault()
              api
                .post(`/admin/api/collections/${encodeURIComponent(String(id))}`, { name, summary, resource_ids: (detail.members || []).map((member: Row) => member.id) })
                .then(() => toast("已保存并进入审核"))
                .catch((err: Error) => toast(err.message, "err"))
            }}
          >
            <Field label="名称">
              <input value={name} onChange={(event) => setName(event.target.value)} />
            </Field>
            <Field label="简介">
              <textarea value={summary} onChange={(event) => setSummary(event.target.value)} />
            </Field>
            <button className="btn btn-primary" type="submit">
              保存并提交审核
            </button>
          </form>
        </div>
      )}
    </>
  )
}

export function ReportsPage() {
  const { items, total, q, setQ, page, setPage, error, loading, load } = useRows("/admin/api/tickets")
  const [current, setCurrent] = useState<Row | null>(null)
  const [currentDetail, setCurrentDetail] = useState<Row | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)
  const [detailError, setDetailError] = useState("")
  const [reply, setReply] = useState("")

  const openTicket = (item: Row) => {
    setCurrent(item)
    setCurrentDetail(item)
    setDetailError("")
    setDetailLoading(true)
    api
      .get<Row>(`/admin/api/tickets/${encodeURIComponent(String(item.id))}`)
      .then(setCurrentDetail)
      .catch((err: Error) => setDetailError(err.message))
      .finally(() => setDetailLoading(false))
  }

  const ticket = currentDetail?.ticket && typeof currentDetail.ticket === "object" ? currentDetail.ticket as Row : currentDetail || current
  return (
    <>
      <PageHeader title="举报与反馈" hint="点开工单后回复。回复走对话框，避免在表格里误发。">
        <SearchForm value={q} onChange={setQ} onSubmit={() => { setPage(1); load(q, 1) }} placeholder="搜索工单" />
      </PageHeader>
      <div className="table-wrap">
        <TableState loading={loading} error={error} onRetry={() => void load()} isEmpty={!items.length} empty={q ? "没有匹配的工单" : "没有工单"}>
          <table>
            <thead>
              <tr>
                <th>用户</th>
                <th>主题</th>
                <th>类型</th>
                <th>状态</th>
                <th>更新时间</th>
              </tr>
            </thead>
            <tbody>
              {items.map((item) => (
                <tr key={item.id} className="clickable" onClick={() => openTicket(item)}>
                  <td><strong>{item.username || "—"}</strong><small className="mono">{item.user_id || "—"}</small></td>
                  <td><div>{item.subject || "无主题"}</div><small className="message-cell">{item.message}</small></td>
                  <td>{item.kind || "—"}</td>
                  <td><Status value={item.status} /></td>
                  <td>{formatRelative(item.updated_at)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </TableState>
      </div>
      <Pagination page={page} total={total} perPage={25} onChange={setPage} />
      <Dialog
        open={!!current}
        title={ticket?.subject || "工单"}
        hint={ticket?.username || current?.username}
        onClose={() => { setCurrent(null); setCurrentDetail(null); setDetailError("") }}
        footer={
          <>
            <button className="btn" type="button" onClick={() => { setCurrent(null); setCurrentDetail(null); setDetailError("") }}>
              取消
            </button>
            <button
              className="btn btn-primary"
              disabled={!reply.trim()}
              onClick={() =>
                api
                  .post(`/admin/api/tickets/${current?.id}`, { status: "replied", reply })
                  .then(() => {
                    toast("已回复")
                    setCurrent(null)
                    setCurrentDetail(null)
                    setDetailError("")
                    setReply("")
                    load()
                  })
                  .catch((err: Error) => toast(err.message, "err"))
              }
            >
              发送回复
            </button>
          </>
        }
      >
        {detailLoading ? <p className="hint">加载详情…</p> : null}
        {detailError ? <div className="error">{detailError}</div> : null}
        <FieldList row={ticket} prefer={["id", "user_id", "username", "kind", "status", "target_source", "target_id", "target_url", "created_at", "updated_at", "closed_at"]} />
        <p className="summary">{ticket?.message || "—"}</p>
        {Array.isArray(currentDetail?.ticket?.replies) && currentDetail.ticket.replies.length ? (
          <div className="files">
            {currentDetail.ticket.replies.map((item: Row) => (
              <div className="file" key={item.id}>
                <div><strong>{item.author || "—"}</strong><small>{formatRelative(item.created_at)}</small></div>
                <div>{item.message || "—"}</div>
              </div>
            ))}
          </div>
        ) : null}
        <Field label="回复">
          <textarea value={reply} onChange={(event) => setReply(event.target.value)} />
        </Field>
      </Dialog>
    </>
  )
}

export function DevicesPage() {
  const { items, total, q, setQ, page, setPage, error, loading, load } = useRows("/admin/api/devices")
  const [open, setOpen] = useState(false)
  const [form, setForm] = useState({ display_name: "", codename: "", platform: "vela_os", vendor: "", astrobox_id: "", enabled: true, id: "" })
  return (
    <>
      <PageHeader title="设备目录" hint="新增和编辑用对话框，不在表格里直接改。">
        <SearchForm value={q} onChange={setQ} onSubmit={() => { setPage(1); load(q, 1) }} placeholder="搜索设备" />
        <button className="btn btn-primary" type="button" onClick={() => { setForm({ display_name: "", codename: "", platform: "vela_os", vendor: "", astrobox_id: "", enabled: true, id: "" }); setOpen(true) }}>
          新建设备
        </button>
      </PageHeader>
      <div className="table-wrap">
        <TableState loading={loading} error={error} onRetry={() => void load()} isEmpty={!items.length} empty={q ? "没有匹配的设备" : "没有设备"}>
          <table>
            <thead>
              <tr>
                <th>名称</th>
                <th>代号</th>
                <th>平台</th>
                <th>AstroBox ID</th>
                <th>资源</th>
                <th>状态</th>
              </tr>
            </thead>
            <tbody>
              {items.map((item) => (
                <tr key={item.id} className="clickable" onClick={() => { setForm({ ...form, ...item, display_name: item.name || item.display_name, id: item.id }); setOpen(true) }}>
                  <td><strong>{item.name || item.display_name || "未命名设备"}</strong><small>{item.vendor || "—"}</small></td>
                  <td className="mono">{item.codename || "—"}</td>
                  <td>{item.platform || "—"}</td>
                  <td className="mono">{item.astrobox_id || "—"}</td>
                  <td>{item.resource_count ?? item.artifact_count ?? "—"}</td>
                  <td><Status value={item.enabled ? "enabled" : "disabled"} /></td>
                </tr>
              ))}
            </tbody>
          </table>
        </TableState>
      </div>
      <Pagination page={page} total={total} perPage={25} onChange={setPage} />
      <Dialog
        open={open}
        title={form.id ? "编辑设备" : "新建设备"}
        onClose={() => setOpen(false)}
        footer={
          <button
            className="btn btn-primary"
            onClick={() =>
              api
                .post(form.id ? `/admin/api/devices/${form.id}` : "/admin/api/devices", form)
                .then(() => {
                  toast("已保存")
                  setOpen(false)
                  load()
                })
                .catch((err: Error) => toast(err.message, "err"))
            }
          >
            保存
          </button>
        }
      >
        <Field label="显示名">
          <input value={form.display_name} onChange={(event) => setForm({ ...form, display_name: event.target.value })} />
        </Field>
        <Field label="代号">
          <input value={form.codename} onChange={(event) => setForm({ ...form, codename: event.target.value })} />
        </Field>
        <Field label="平台">
          <select value={form.platform} onChange={(event) => setForm({ ...form, platform: event.target.value })}>
            <option value="vela_os">Vela</option>
            <option value="zepp_os">Zepp</option>
          </select>
        </Field>
        <Field label="厂商">
          <input value={form.vendor} onChange={(event) => setForm({ ...form, vendor: event.target.value })} />
        </Field>
        <Field label="AstroBox ID">
          <input value={form.astrobox_id} onChange={(event) => setForm({ ...form, astrobox_id: event.target.value })} />
        </Field>
        <label className="check-field"><input type="checkbox" checked={form.enabled} onChange={(event) => setForm({ ...form, enabled: event.target.checked })} /> 启用设备</label>
      </Dialog>
    </>
  )
}

export function CoinsPage() {
  const { items, total, q, setQ, page, setPage, error, loading, load } = useRows("/admin/api/coins/ledger")
  const [open, setOpen] = useState(false)
  const [form, setForm] = useState({ user: "", delta_units: 0, reason: "", action: "adjust" })
  return (
    <>
      <PageHeader title="硬币" hint="发放和冻结走对话框，台账只读。">
        <SearchForm value={q} onChange={setQ} onSubmit={() => { setPage(1); load(q, 1) }} placeholder="搜索用户" />
        <button className="btn btn-primary" type="button" onClick={() => setOpen(true)}>
          调整余额
        </button>
      </PageHeader>
      <div className="table-wrap">
        <TableState loading={loading} error={error} onRetry={() => void load()} isEmpty={!items.length} empty={q ? "没有匹配的台账记录" : "没有台账记录"}>
          <table>
            <thead>
              <tr>
                <th>用户</th>
                <th>类型</th>
                <th>变动</th>
                <th>关联对象</th>
                <th>原因</th>
                <th>时间</th>
              </tr>
            </thead>
            <tbody>
              {items.map((item, index) => (
                <tr key={item.id || index}>
                  <td><strong>{item.username || "—"}</strong><small className="mono">{item.user_id || "—"}</small></td>
                  <td>{item.kind || "—"}</td>
                  <td className={Number(item.delta_units) >= 0 ? "amount-positive" : "amount-negative"}>{Number(item.delta_units) >= 0 ? "+" : ""}{item.delta_units}</td>
                  <td className="mono">{item.reference_id || "—"}</td>
                  <td>{item.note || "—"}</td>
                  <td>{formatRelative(item.created_at)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </TableState>
      </div>
      <Pagination page={page} total={total} perPage={25} onChange={setPage} />
      <Dialog
        open={open}
        title="调整硬币"
        onClose={() => setOpen(false)}
        footer={
          <button
            className="btn btn-primary"
            onClick={() =>
              api
                .post(`/admin/api/coins/users/${form.user}`, form)
                .then(() => {
                  toast("已调整")
                  setOpen(false)
                  load()
                })
                .catch((err: Error) => toast(err.message, "err"))
            }
          >
            确认
          </button>
        }
      >
        <Field label="用户 ID">
          <input value={form.user} onChange={(event) => setForm({ ...form, user: event.target.value })} />
        </Field>
        <Field label="变动（单位）">
          <input type="number" value={form.delta_units} onChange={(event) => setForm({ ...form, delta_units: Number(event.target.value) })} />
        </Field>
        <Field label="原因">
          <input value={form.reason} onChange={(event) => setForm({ ...form, reason: event.target.value })} />
        </Field>
      </Dialog>
    </>
  )
}

export function MessagesPage() {
  const { items, total, q, setQ, page, setPage, error, loading, load } = useRows("/admin/api/messages")
  const [open, setOpen] = useState(false)
  const [form, setForm] = useState({ user_ids: "", title: "", body: "" })
  return (
    <>
      <PageHeader title="系统消息" hint="发送给指定用户。写清标题和正文。">
        <SearchForm value={q} onChange={setQ} onSubmit={() => { setPage(1); load(q, 1) }} placeholder="搜索消息" />
        <button className="btn btn-primary" type="button" onClick={() => setOpen(true)}>
          发送消息
        </button>
      </PageHeader>
      <div className="table-wrap">
        <TableState loading={loading} error={error} onRetry={() => void load()} isEmpty={!items.length} empty={q ? "没有匹配的消息" : "没有消息"}>
          <table>
            <thead>
              <tr>
                <th>消息</th>
                <th>用户</th>
                <th>类型</th>
                <th>状态</th>
                <th>创建时间</th>
                <th>过期时间</th>
              </tr>
            </thead>
            <tbody>
              {items.map((item, index) => (
                <tr key={item.id || index}>
                  <td><strong>{item.title || "无标题"}</strong><small className="message-cell">{item.body || "—"}</small></td>
                  <td><strong>{item.username || "—"}</strong><small className="mono">{item.user_id || "—"}</small></td>
                  <td>{item.kind || "—"}</td>
                  <td><Status value={item.read_at ? "read" : "pending"} label={item.read_at ? "已读" : "未读"} /></td>
                  <td>{formatRelative(item.created_at)}</td>
                  <td>{formatRelative(item.expires_at)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </TableState>
      </div>
      <Pagination page={page} total={total} perPage={25} onChange={setPage} />
      <Dialog
        open={open}
        title="发送系统消息"
        onClose={() => setOpen(false)}
        footer={
          <button
            className="btn btn-primary"
            onClick={() =>
              api
                .post("/admin/api/messages", { user_ids: form.user_ids.split(/[,\s]+/).filter(Boolean), title: form.title, body: form.body })
                .then(() => {
                  toast("已发送")
                  setOpen(false)
                  load()
                })
                .catch((err: Error) => toast(err.message, "err"))
            }
          >
            发送
          </button>
        }
      >
        <Field label="用户 ID（逗号分隔）">
          <input value={form.user_ids} onChange={(event) => setForm({ ...form, user_ids: event.target.value })} />
        </Field>
        <Field label="标题">
          <input value={form.title} onChange={(event) => setForm({ ...form, title: event.target.value })} />
        </Field>
        <Field label="正文">
          <textarea value={form.body} onChange={(event) => setForm({ ...form, body: event.target.value })} />
        </Field>
      </Dialog>
    </>
  )
}

export function HomeComposerPage() {
  const [banners, setBanners] = useState<Row[]>([])
  const [sections, setSections] = useState<Row[]>([])
  const [open, setOpen] = useState(false)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState("")
  const [form, setForm] = useState({ title: "", subtitle: "", type: "resource", resource_id: "", blog_slug: "", link_url: "", enabled: true })
  const load = async () => {
    setLoading(true)
    setError("")
    try {
      const data = await api.get<Row>("/admin/api/home")
      setBanners(data.banners || [])
      setSections(data.sections || [])
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setLoading(false)
    }
  }
  useEffect(() => {
    void load()
  }, [])
  return (
    <>
      <PageHeader title="首页编排" hint="Banner 和分区用对话框编辑，列表里只做排序和删除。">
        <button className="btn btn-primary" type="button" onClick={() => setOpen(true)}>
          新建 Banner
        </button>
      </PageHeader>
      {error ? <div className="error page-error">{error}</div> : null}
      <div className="page-body stack">
        {loading ? <Empty>加载中</Empty> : null}
        {!loading && !banners.length && !sections.length ? <Empty>还没有首页内容</Empty> : null}
        {!loading && banners.map((banner) => (
          <div className="file" key={banner.id}>
            <div>
              <strong>{banner.title || "无标题"}</strong>
              <small>{banner.type || "—"} · {banner.enabled ? "启用" : "停用"} · 第 {banner.position ?? "—"} 项</small>
              {banner.subtitle ? <p className="hint">{banner.subtitle}</p> : null}
            </div>
            <div className="row-actions">
              <button className="btn" type="button" aria-label={`上移 ${banner.title || "Banner"}`} onClick={() => api.post(`/admin/api/home/banners/${banner.id}/move`, { delta: -1 }).then(load).catch((err: Error) => setError(err.message))}>
                上移
              </button>
              <button className="btn btn-danger" type="button" onClick={() => api.post(`/admin/api/home/banners/${banner.id}/delete`).then(load).catch((err: Error) => setError(err.message))}>
                删除
              </button>
            </div>
          </div>
        ))}
        {!loading && sections.map((block) => (
          <div className="panel" key={block.section?.id}>
            <div className="section-head"><div><h3>{block.section?.name || "未命名分区"}</h3><p className="hint">{block.section?.description || "—"}</p></div><span className="section-count">{block.cards?.length || 0} 项</span></div>
            {(block.cards || []).map((card: Row) => (
              <div key={card.id} className="file">
                <span>{card.type || "—"}</span>
                <span className="mono">{card.resource_id || card.blog_slug || "—"}</span>
              </div>
            ))}
            {!block.cards?.length ? <Empty>这个分区还没有内容</Empty> : null}
          </div>
        ))}
      </div>
      <Dialog
        open={open}
        title="新建 Banner"
        onClose={() => setOpen(false)}
        footer={
          <button
            className="btn btn-primary"
            onClick={() =>
              api
                .post("/admin/api/home/banners", form)
                .then(() => {
                  toast("已创建")
                  setOpen(false)
                  load()
                })
                .catch((err: Error) => toast(err.message, "err"))
            }
          >
            创建
          </button>
        }
      >
        <Field label="标题">
          <input value={form.title} onChange={(event) => setForm({ ...form, title: event.target.value })} />
        </Field>
        <Field label="副标题">
          <input value={form.subtitle} onChange={(event) => setForm({ ...form, subtitle: event.target.value })} />
        </Field>
      </Dialog>
    </>
  )
}

export function BlogPage() {
  const { items, total, q, setQ, page, setPage, error, loading, load } = useRows("/admin/api/blog")
  const { slug } = useParams()
  const navigate = useNavigate()
  const [post, setPost] = useState({ slug: "", title: "", subtitle: "", author: "", body: "", type: "announcement", published: false })
  const [postError, setPostError] = useState("")
  useEffect(() => {
    if (!slug) {
      setPost({ slug: "", title: "", subtitle: "", author: "", body: "", type: "announcement", published: false })
      setPostError("")
      return
    }
    api.get<Row>(`/admin/api/blog/${slug}`).then((value) => setPost((current) => ({ ...current, ...value }))).catch((err: Error) => setPostError(err.message))
  }, [slug])
  return (
    <>
      <PageHeader title="Blog" hint="管理公告和说明文章，选择左侧条目后编辑。">
        <SearchForm value={q} onChange={setQ} onSubmit={() => { setPage(1); load(q, 1) }} placeholder="搜索文章" />
        <button className="btn btn-primary" type="button" onClick={() => navigate("/blog")}>新建文章</button>
      </PageHeader>
      <div className="split">
        <div className="queue">
          <TableState loading={loading} error={error} onRetry={() => void load()} isEmpty={!items.length} empty={q ? "没有匹配的文章" : "没有文章"}>
            {items.map((item) => (
              <a key={item.slug} className={`queue-item ${item.slug === slug ? "selected" : ""}`} href={`/admin/blog/${item.slug}`}>
                <div className="title">{item.title}</div>
                <div className="meta"><span>{item.author || "未署名"}</span><span>{item.published ? "已发布" : "草稿"}</span><span>{formatRelative(item.updated_at)}</span></div>
                <div className="hint">{item.slug}</div>
              </a>
            ))}
          </TableState>
        </div>
        <div className="detail">
          {postError ? <div className="error">{postError}</div> : null}
          {!slug && !post.title ? <div className="detail-empty">新建文章，填写右侧表单</div> : null}
          <form
            className="stack"
            onSubmit={(event: FormEvent) => {
              event.preventDefault()
              api
                .post(post.slug ? `/admin/api/blog/${post.slug}` : "/admin/api/blog", post)
                .then(() => { toast("已保存"); setPostError(""); load() })
                .catch((err: Error) => toast(err.message, "err"))
            }}
          >
            <Field label="Slug">
              <input value={post.slug} onChange={(event) => setPost({ ...post, slug: event.target.value })} />
            </Field>
            <Field label="标题">
              <input value={post.title} onChange={(event) => setPost({ ...post, title: event.target.value })} />
            </Field>
            <Field label="副标题">
              <input value={post.subtitle} onChange={(event) => setPost({ ...post, subtitle: event.target.value })} />
            </Field>
            <Field label="作者">
              <input value={post.author} onChange={(event) => setPost({ ...post, author: event.target.value })} />
            </Field>
            <Field label="类型">
              <select value={post.type} onChange={(event) => setPost({ ...post, type: event.target.value })}><option value="announcement">公告</option><option value="guide">指南</option><option value="article">文章</option></select>
            </Field>
            <Field label="正文">
              <textarea value={post.body} onChange={(event) => setPost({ ...post, body: event.target.value })} />
            </Field>
            <label className="check-field">
              <input type="checkbox" checked={post.published} onChange={(event) => setPost({ ...post, published: event.target.checked })} /> 发布
            </label>
            <button className="btn btn-primary" type="submit">
              保存
            </button>
          </form>
        </div>
      </div>
    </>
  )
}

export function AnnouncementsPage() {
  const { items, total, q, setQ, page, setPage, error, loading, load } = useRows("/admin/api/announcements")
  const [open, setOpen] = useState(false)
  const [removing, setRemoving] = useState<Row | null>(null)
  const [form, setForm] = useState({ title: "", body: "" })
  return (
    <>
      <PageHeader title="公告" hint="发布全站公告。删除需确认。">
        <button className="btn btn-primary" type="button" onClick={() => setOpen(true)}>
          发布公告
        </button>
      </PageHeader>
      <div className="table-wrap">
        <TableState loading={loading} error={error} onRetry={() => void load()} isEmpty={!items.length} empty={q ? "没有匹配的公告" : "没有公告"}>
          <table>
            <thead>
              <tr>
                <th>公告</th>
                <th>发布人</th>
                <th>发布时间</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {items.map((item) => (
                <tr key={item.id}>
                  <td><strong>{item.title || "无标题"}</strong><small className="message-cell">{item.body || "—"}</small></td>
                  <td>{item.creator || item.created_by || "—"}</td>
                  <td>{formatRelative(item.published_at)}</td>
                  <td><button className="btn btn-danger" type="button" onClick={() => setRemoving(item)}>删除</button></td>
                </tr>
              ))}
            </tbody>
          </table>
        </TableState>
      </div>
      <Dialog
        open={open}
        title="发布公告"
        onClose={() => setOpen(false)}
        footer={
          <button
            className="btn btn-primary"
            onClick={() =>
              api
                .post("/admin/api/announcements", form)
                .then(() => {
                  toast("已发布")
                  setOpen(false)
                  load()
                })
                .catch((err: Error) => toast(err.message, "err"))
            }
          >
            发布
          </button>
        }
      >
        <Field label="标题">
          <input value={form.title} onChange={(event) => setForm({ ...form, title: event.target.value })} />
        </Field>
        <Field label="正文">
          <textarea value={form.body} onChange={(event) => setForm({ ...form, body: event.target.value })} />
        </Field>
      </Dialog>
      <Dialog
        open={!!removing}
        title="删除公告"
        hint="删除后不会再向客户端展示"
        onClose={() => setRemoving(null)}
        footer={<><button className="btn" type="button" onClick={() => setRemoving(null)}>取消</button><button className="btn btn-danger" type="button" onClick={() => removing && api.post(`/admin/api/announcements/${removing.id}/delete`).then(() => { toast("已删除"); setRemoving(null); load() }).catch((err: Error) => toast(err.message, "err"))}>确认删除</button></>}
      >
        <p className="summary">{removing?.title || "无标题"}</p>
      </Dialog>
    </>
  )
}

export function ReleasesPage() {
  const { items, total, q, setQ, page, setPage, error, loading, load } = useRows("/admin/api/releases")
  const [open, setOpen] = useState(false)
  const [form, setForm] = useState({ version: "", channel: "stable", platform: "all", arch: "all", download_url: "", notes_zh: "" })
  return (
    <>
      <PageHeader title="客户端版本" hint="发布新版本用对话框。">
        <button className="btn btn-primary" type="button" onClick={() => setOpen(true)}>
          发布版本
        </button>
      </PageHeader>
      <div className="table-wrap">
        <TableState loading={loading} error={error} onRetry={() => void load()} isEmpty={!items.length} empty={q ? "没有匹配的版本" : "没有发布版本"}>
          <table>
            <thead>
              <tr>
                <th>版本</th>
                <th>通道</th>
                <th>平台 / 架构</th>
                <th>最低版本</th>
                <th>状态</th>
                <th>发布时间</th>
              </tr>
            </thead>
            <tbody>
              {items.map((item, index) => (
                <tr key={item.id || index}>
                  <td><strong>{item.version || "—"}</strong><small className="mono">{item.id || "—"}</small></td>
                  <td>{item.channel || "—"}</td>
                  <td>{[item.platform, item.arch].filter(Boolean).join(" / ") || "—"}</td>
                  <td>{item.minimum_version || "—"}</td>
                  <td><Status value={item.state || (item.enabled ? "published" : "disabled")} /></td>
                  <td>{formatRelative(item.published_at || item.updated_at)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </TableState>
      </div>
      <Dialog
        open={open}
        title="发布版本"
        onClose={() => setOpen(false)}
        footer={
          <button
            className="btn btn-primary"
            onClick={() =>
              api
                .post("/admin/api/releases", form)
                .then(() => {
                  toast("已发布")
                  setOpen(false)
                  load()
                })
                .catch((err: Error) => toast(err.message, "err"))
            }
          >
            发布
          </button>
        }
      >
        <Field label="版本">
          <input value={form.version} onChange={(event) => setForm({ ...form, version: event.target.value })} />
        </Field>
        <Field label="下载地址">
          <input value={form.download_url} onChange={(event) => setForm({ ...form, download_url: event.target.value })} />
        </Field>
        <Field label="中文说明">
          <textarea value={form.notes_zh} onChange={(event) => setForm({ ...form, notes_zh: event.target.value })} />
        </Field>
      </Dialog>
    </>
  )
}

export function SystemPage() {
  const location = useLocation()
  const path = location.pathname
  const map: Record<string, SystemConfig> = {
    "/oauth/events": { title: "OAuth 事件", hint: "查看授权事件、结果和来源", path: "/admin/api/oauth/events", searchPlaceholder: "事件、应用或用户", detailPath: (row) => row.id ? `/admin/api/oauth/events/${encodeURIComponent(String(row.id))}` : "", columns: [{ key: "event_type", label: "事件" }, { key: "result", label: "结果", format: "status" }, { key: "platform", label: "平台" }] },
    "/oauth/states": { title: "OAuth States", hint: "查看未完成的授权状态", path: "/admin/api/oauth/states", searchPlaceholder: "State ID、应用或用户", detailPath: (row) => row.id ? `/admin/api/oauth/states/${encodeURIComponent(String(row.id))}` : "", columns: [{ key: "id", label: "ID" }, { key: "status", label: "状态", format: "status" }, { key: "app_id", label: "应用" }, { key: "created_at", label: "创建时间", format: "date" }, { key: "expires_at", label: "过期时间", format: "date" }] },
    "/oauth/tickets": { title: "登录 Tickets", hint: "查看登录票据及其状态", path: "/admin/api/oauth/tickets", searchPlaceholder: "Ticket ID、应用或用户", detailPath: (row) => row.id ? `/admin/api/oauth/tickets/${encodeURIComponent(String(row.id))}` : "", columns: [{ key: "id", label: "ID" }, { key: "status", label: "状态", format: "status" }, { key: "app_id", label: "应用" }, { key: "created_at", label: "创建时间", format: "date" }, { key: "expires_at", label: "过期时间", format: "date" }] },
    "/clients": { title: "客户端统计", hint: "按应用和平台查看访问统计", path: "/admin/api/clients", searchPlaceholder: "应用、版本或平台", columns: [{ key: "app_id", label: "应用" }, { key: "platform", label: "平台" }, { key: "app_version", label: "版本" }, { key: "request_count", label: "请求数" }, { key: "success_count", label: "成功" }, { key: "failure_count", label: "失败" }, { key: "last_seen", label: "最近活动", format: "date" }] },
    "/storage/blobs": { title: "Blob 与副本", hint: "查看本地文件、副本状态和资源引用", path: "/admin/api/blobs", searchPlaceholder: "SHA256、媒体类型或副本状态", detailPath: (row) => row.sha256 ? `/admin/api/blobs/${encodeURIComponent(String(row.sha256))}` : "", columns: [{ key: "sha256", label: "SHA256" }, { key: "size_bytes", label: "大小", format: "bytes" }, { key: "media_type", label: "类型" }, { key: "local_available", label: "本地", format: "boolean" }, { key: "r2_state", label: "副本", format: "status" }, { key: "referenced", label: "已引用", format: "boolean" }, { key: "created_at", label: "创建时间", format: "date" }] },
    "/health": { title: "运行状态", hint: "查看服务依赖和当前延迟", path: "/admin/api/health", columns: [{ key: "db", label: "数据库", format: "status" }, { key: "latency", label: "延迟" }, { key: "version", label: "版本" }] },
    "/audit": { title: "审计日志", hint: "按动作、操作者和结果查询", path: "/admin/api/audit", searchPlaceholder: "动作、操作者或目标", detailPath: (row) => row.id ? `/admin/api/audit/${encodeURIComponent(String(row.id))}` : "", columns: [{ key: "action", label: "动作" }, { key: "username", label: "操作者" }, { key: "result", label: "结果", format: "status" }, { key: "target", label: "目标" }, { key: "ip", label: "IP" }, { key: "created_at", label: "时间", format: "date" }] },
    "/settings": { title: "设置", hint: "查看当前服务端配置", path: "/admin/api/settings", columns: [{ key: "bandbbs_client_id", label: "BandBBS" }, { key: "github_client_id", label: "GitHub" }, { key: "public_url", label: "地址" }] },
  }
  const config = map[path] || map["/audit"]
  const { items, total, payload, q, setQ, page, setPage, error, loading, load } = useRows(config.path)
  const [cleanup, setCleanup] = useState<Row | null>(null)
  const [open, setOpen] = useState<Row | null>(null)
  const [detail, setDetail] = useState<Row | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)
  const [detailError, setDetailError] = useState("")
  const requeue = path === "/storage/blobs"
  const detailPath = open && config.detailPath ? config.detailPath(open) : ""

  useEffect(() => {
    setOpen(null)
    setDetail(null)
    setDetailError("")
  }, [path])

  useEffect(() => {
    if (!open) {
      setDetail(null)
      setDetailError("")
      setDetailLoading(false)
      return
    }
    if (!detailPath) {
      setDetail(open)
      setDetailError("")
      setDetailLoading(false)
      return
    }
    let active = true
    setDetail(open)
    setDetailError("")
    setDetailLoading(true)
    api
      .get<Row>(detailPath)
      .then((value) => {
        if (active) setDetail(value)
      })
      .catch((err: Error) => {
        if (active) setDetailError(err.message)
      })
      .finally(() => {
        if (active) setDetailLoading(false)
      })
    return () => {
      active = false
    }
  }, [open, detailPath])
  return (
    <>
      <PageHeader title={config.title} hint={config.hint}>
        {config.searchPlaceholder ? <SearchForm value={q} onChange={setQ} onSubmit={() => { setPage(1); load(q, 1) }} placeholder={config.searchPlaceholder} /> : null}
        {path === "/health" && (
          <button
            className="btn"
            type="button"
            onClick={() => api.post("/admin/cleanup/preview").then((data) => setCleanup(data as Row)).catch((err: Error) => toast(err.message, "err"))}
          >
            预览清理
          </button>
        )}
      </PageHeader>
      <div className="table-wrap system-table-wrap">
        <TableState loading={loading} error={error} onRetry={() => void load()} isEmpty={!items.length} empty={q ? "没有匹配的数据" : "没有数据"}>
          <table>
            <thead>
              <tr>
                {config.columns.map((column) => (
                  <th key={column.key}>{column.label}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {items.map((row, index) => (
                <tr key={String(row.id || row.sha256 || index)} className={config.detailPath ? "clickable" : undefined} onClick={config.detailPath ? () => setOpen(row) : undefined}>
                  {config.columns.map((column) => (
                    <td key={column.key}>{systemCell(row, column)}</td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </TableState>
      </div>
      {path === "/health" && payload.diagnostics ? <HealthDiagnostics value={payload.diagnostics} /> : null}
      <Pagination page={page} total={total} perPage={25} onChange={setPage} />
      <Dialog
        open={!!cleanup}
        title="清理过期数据"
        hint={cleanup?.confirmation}
        onClose={() => setCleanup(null)}
        footer={
          <button
            className="btn btn-danger"
            onClick={() =>
              api
                .post("/admin/api/cleanup", { token: cleanup?.token, confirmation: cleanup?.confirmation })
                .then(() => {
                  toast("清理完成")
                  setCleanup(null)
                })
                .catch((err: Error) => toast(err.message, "err"))
            }
          >
            确认清理
          </button>
        }
      >
        <pre className="summary">{JSON.stringify(cleanup?.preview, null, 2)}</pre>
      </Dialog>
      <Dialog
        open={Boolean(config.detailPath && open)}
        title={open?.sha256 ? "Blob 详情" : `${config.title}详情`}
        wide
        onClose={() => setOpen(null)}
        footer={
          <>
            {requeue && open?.sha256 && (
              <button
                className="btn"
                type="button"
                onClick={() =>
                  api
                    .post(`/admin/api/blobs/${open.sha256}/requeue`)
                    .then(() => {
                      toast("已重新入队")
                      setOpen(null)
                      load()
                    })
                    .catch((err: Error) => toast(err.message, "err"))
                }
              >
                重试副本
              </button>
            )}
            <button className="btn" type="button" onClick={() => setOpen(null)}>
              关闭
            </button>
          </>
        }
      >
        {detailLoading ? <p className="hint">加载详情…</p> : null}
        {detailError ? <div className="error">{detailError}</div> : null}
        <FieldList row={detailRecord(detail)} prefer={config.columns.map((column) => column.key)} />
        <details>
          <summary>原始数据</summary>
          <pre className="summary">{JSON.stringify(detail || open, null, 2)}</pre>
        </details>
      </Dialog>
    </>
  )
}
