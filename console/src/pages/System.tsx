import { FormEvent, useEffect, useState } from "react"
import { useLocation, useNavigate, useParams } from "react-router"
import { api } from "../api"
import { Dialog, Empty, Field, FieldList, PageHeader, Pagination, SearchForm, Status, formatRelative, toast } from "../ui"

type Row = Record<string, any>

function useRows(path: string, extra: Record<string, string | number | undefined> = {}) {
  const [items, setItems] = useState<Row[]>([])
  const [total, setTotal] = useState(0)
  const [q, setQ] = useState("")
  const [page, setPage] = useState(1)
  const [error, setError] = useState("")
  const load = (search = q, next = page) =>
    api
      .list<Row>(path, { q: search, page: next, per_page: 25, ...extra })
      .then((data) => {
        setItems(data.items || [])
        setTotal(data.total || 0)
      })
      .catch((err: Error) => setError(err.message))
  useEffect(() => {
    load(q, page)
  }, [path, page])
  return { items, total, q, setQ, page, setPage, error, load }
}

export function CollectionsPage() {
  const { id } = useParams()
  const navigate = useNavigate()
  const { items, total, q, setQ, page, setPage, error, load } = useRows("/admin/api/collections")
  const [detail, setDetail] = useState<Row | null>(null)
  const [name, setName] = useState("")
  const [summary, setSummary] = useState("")
  useEffect(() => {
    if (!id) return
    api.get<Row>(`/admin/api/collections/${id}`).then((value) => {
      setDetail(value)
      setName(value.collection?.latest_revision_name || value.collection?.name || "")
      setSummary(value.collection?.latest_revision_summary || "")
    })
  }, [id])
  return (
    <>
      <PageHeader title="合集" hint="点进合集后可以改名称和成员，提交后进入合集审核。">
        <SearchForm value={q} onChange={setQ} onSubmit={() => { setPage(1); load(q, 1) }} placeholder="搜索合集" />
      </PageHeader>
      {error && <div className="error">{error}</div>}
      <div className="table-wrap">
        <table>
          <thead>
            <tr>
              <th>名称</th>
              <th>作者</th>
              <th>状态</th>
            </tr>
          </thead>
          <tbody>
            {items.map((item) => (
              <tr key={item.id} className="clickable" onClick={() => navigate(`/collections/${item.id}`)}>
                <td>
                  {item.name}
                </td>
                <td>{item.owner}</td>
                <td>
                  <Status value={item.state} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <Pagination page={page} total={total} perPage={25} onChange={setPage} />
      {detail && (
        <div className="page-body">
          <form
            className="stack"
            onSubmit={(event: FormEvent) => {
              event.preventDefault()
              api
                .post(`/admin/api/collections/${id}`, { name, summary, resource_ids: (detail.members || []).map((member: Row) => member.id) })
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
  const { items, total, q, setQ, page, setPage, error, load } = useRows("/admin/api/tickets")
  const [current, setCurrent] = useState<Row | null>(null)
  const [reply, setReply] = useState("")
  return (
    <>
      <PageHeader title="举报与反馈" hint="点开工单后回复。回复走对话框，避免在表格里误发。">
        <SearchForm value={q} onChange={setQ} onSubmit={() => { setPage(1); load(q, 1) }} placeholder="搜索工单" />
      </PageHeader>
      {error && <div className="error">{error}</div>}
      <div className="table-wrap">
        <table>
          <thead>
            <tr>
              <th>用户</th>
              <th>标题</th>
              <th>状态</th>
            </tr>
          </thead>
          <tbody>
            {items.map((item) => (
              <tr key={item.id} className="clickable" onClick={() => setCurrent(item)}>
                <td>{item.username || "—"}</td>
                <td>
                  <div>{item.subject || item.kind}</div>
                  <small>{item.message}</small>
                </td>
                <td>
                  <Status value={item.status} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <Pagination page={page} total={total} perPage={25} onChange={setPage} />
      <Dialog
        open={!!current}
        title={current?.subject || "工单"}
        hint={current?.username}
        onClose={() => setCurrent(null)}
        footer={
          <>
            <button className="btn" type="button" onClick={() => setCurrent(null)}>
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
        <p className="summary">{current?.message}</p>
        <Field label="回复">
          <textarea value={reply} onChange={(event) => setReply(event.target.value)} />
        </Field>
      </Dialog>
    </>
  )
}

export function DevicesPage() {
  const { items, total, q, setQ, page, setPage, error, load } = useRows("/admin/api/devices")
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
      {error && <div className="error">{error}</div>}
      <div className="table-wrap">
        <table>
          <thead>
            <tr>
              <th>名称</th>
              <th>代号</th>
              <th>平台</th>
            </tr>
          </thead>
          <tbody>
            {items.map((item) => (
              <tr key={item.id} className="clickable" onClick={() => { setForm({ ...form, ...item, display_name: item.name || item.display_name, id: item.id }); setOpen(true) }}>
                <td>{item.name}</td>
                <td>{item.codename}</td>
                <td>{item.platform}</td>
              </tr>
            ))}
          </tbody>
        </table>
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
      </Dialog>
    </>
  )
}

export function CoinsPage() {
  const { items, total, q, setQ, page, setPage, error, load } = useRows("/admin/api/coins/ledger")
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
      {error && <div className="error">{error}</div>}
      <div className="table-wrap">
        <table>
          <thead>
            <tr>
              <th>用户</th>
              <th>类型</th>
              <th>变动</th>
              <th>原因</th>
            </tr>
          </thead>
          <tbody>
            {items.map((item, index) => (
              <tr key={item.id || index}>
                <td>{item.username}</td>
                <td>{item.kind}</td>
                <td>{item.delta_units}</td>
                <td>{item.note}</td>
              </tr>
            ))}
          </tbody>
        </table>
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
  const { items, total, q, setQ, page, setPage, error, load } = useRows("/admin/api/messages")
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
      {error && <div className="error">{error}</div>}
      <div className="table-wrap">
        <table>
          <thead>
            <tr>
              <th>标题</th>
              <th>用户</th>
            </tr>
          </thead>
          <tbody>
            {items.map((item, index) => (
              <tr key={item.id || index}>
                <td>{item.title}</td>
                <td>{item.username}</td>
              </tr>
            ))}
          </tbody>
        </table>
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
  const [form, setForm] = useState({ title: "", subtitle: "", type: "resource", resource_id: "", blog_slug: "", link_url: "", enabled: true })
  const load = () =>
    api.get<Row>("/admin/api/home").then((data) => {
      setBanners(data.banners || [])
      setSections(data.sections || [])
    })
  useEffect(() => {
    load()
  }, [])
  return (
    <>
      <PageHeader title="首页编排" hint="Banner 和分区用对话框编辑，列表里只做排序和删除。">
        <button className="btn btn-primary" type="button" onClick={() => setOpen(true)}>
          新建 Banner
        </button>
      </PageHeader>
      <div className="page-body stack">
        {banners.map((banner) => (
          <div className="file" key={banner.id}>
            <div>
              <div>{banner.title}</div>
              <small>{banner.type}</small>
            </div>
            <div className="row-actions">
              <button className="btn" type="button" onClick={() => api.post(`/admin/api/home/banners/${banner.id}/move`, { delta: -1 }).then(load)}>
                上移
              </button>
              <button className="btn" type="button" onClick={() => api.post(`/admin/api/home/banners/${banner.id}/delete`).then(load)}>
                删除
              </button>
            </div>
          </div>
        ))}
        {sections.map((block) => (
          <div className="panel" key={block.section?.id}>
            <h3>{block.section?.name}</h3>
            {(block.cards || []).map((card: Row) => (
              <div key={card.id} className="hint">
                {card.type} {card.resource_id || card.blog_slug}
              </div>
            ))}
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
  const { items, total, q, setQ, page, setPage, error, load } = useRows("/admin/api/blog")
  const { slug } = useParams()
  const [post, setPost] = useState({ slug: "", title: "", subtitle: "", author: "", body: "", type: "announcement", published: false })
  useEffect(() => {
    if (!slug) return
    api.get<Row>(`/admin/api/blog/${slug}`).then((value) => setPost({ ...post, ...value }))
  }, [slug])
  return (
    <>
      <PageHeader title="Blog" hint="文章编辑在右侧表单，发布前先保存。">
        <SearchForm value={q} onChange={setQ} onSubmit={() => { setPage(1); load(q, 1) }} placeholder="搜索文章" />
      </PageHeader>
      {error && <div className="error">{error}</div>}
      <div className="split">
        <div className="queue">
          {items.map((item) => (
            <a key={item.slug} className="queue-item" href={`/admin/blog/${item.slug}`}>
              <div className="title">{item.title}</div>
              <div className="hint">{item.slug}</div>
            </a>
          ))}
        </div>
        <div className="detail">
          <form
            className="stack"
            onSubmit={(event: FormEvent) => {
              event.preventDefault()
              api
                .post(post.slug ? `/admin/api/blog/${post.slug}` : "/admin/api/blog", post)
                .then(() => toast("已保存"))
                .catch((err: Error) => toast(err.message, "err"))
            }}
          >
            <Field label="Slug">
              <input value={post.slug} onChange={(event) => setPost({ ...post, slug: event.target.value })} />
            </Field>
            <Field label="标题">
              <input value={post.title} onChange={(event) => setPost({ ...post, title: event.target.value })} />
            </Field>
            <Field label="正文">
              <textarea value={post.body} onChange={(event) => setPost({ ...post, body: event.target.value })} />
            </Field>
            <label>
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
  const { items, total, q, setQ, page, setPage, error, load } = useRows("/admin/api/announcements")
  const [open, setOpen] = useState(false)
  const [form, setForm] = useState({ title: "", body: "" })
  return (
    <>
      <PageHeader title="公告" hint="发布全站公告。删除需确认。">
        <button className="btn btn-primary" type="button" onClick={() => setOpen(true)}>
          发布公告
        </button>
      </PageHeader>
      {error && <div className="error">{error}</div>}
      <div className="table-wrap">
        <table>
          <thead>
            <tr>
              <th>标题</th>
              <th>时间</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {items.map((item) => (
              <tr key={item.id}>
                <td>{item.title}</td>
                <td>{formatRelative(item.published_at)}</td>
                <td>
                  <button className="btn btn-danger" type="button" onClick={() => api.post(`/admin/api/announcements/${item.id}/delete`).then(() => load())}>
                    删除
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <Dialog
        open={open}
        title="发布公告"
        onClose={() => setOpen(false)}
        footer={
          <button
            className="btn btn-primary"
            onClick={() =>
              api.post("/admin/api/announcements", form).then(() => {
                toast("已发布")
                setOpen(false)
                load()
              })
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
    </>
  )
}

export function ReleasesPage() {
  const { items, total, q, setQ, page, setPage, error, load } = useRows("/admin/api/releases")
  const [open, setOpen] = useState(false)
  const [form, setForm] = useState({ version: "", channel: "stable", platform: "all", arch: "all", download_url: "", notes_zh: "" })
  return (
    <>
      <PageHeader title="客户端版本" hint="发布新版本用对话框。">
        <button className="btn btn-primary" type="button" onClick={() => setOpen(true)}>
          发布版本
        </button>
      </PageHeader>
      {error && <div className="error">{error}</div>}
      <div className="table-wrap">
        <table>
          <thead>
            <tr>
              <th>版本</th>
              <th>通道</th>
            </tr>
          </thead>
          <tbody>
            {items.map((item, index) => (
              <tr key={item.id || index}>
                <td>{item.version}</td>
                <td>{item.channel}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <Dialog
        open={open}
        title="发布版本"
        onClose={() => setOpen(false)}
        footer={
          <button
            className="btn btn-primary"
            onClick={() =>
              api.post("/admin/api/releases", form).then(() => {
                toast("已发布")
                setOpen(false)
                load()
              })
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
  const map: Record<string, { title: string; path: string; columns: { key: string; label: string }[] }> = {
    "/oauth/events": { title: "OAuth 事件", path: "/admin/api/oauth/events", columns: [{ key: "event_type", label: "事件" }, { key: "result", label: "结果" }, { key: "platform", label: "平台" }] },
    "/oauth/states": { title: "OAuth States", path: "/admin/api/oauth/states", columns: [{ key: "id", label: "ID" }, { key: "app_id", label: "应用" }] },
    "/oauth/tickets": { title: "登录 Tickets", path: "/admin/api/oauth/tickets", columns: [{ key: "id", label: "ID" }, { key: "status", label: "状态" }] },
    "/clients": { title: "客户端统计", path: "/admin/api/clients", columns: [{ key: "app_id", label: "应用" }, { key: "platform", label: "平台" }] },
    "/storage/blobs": { title: "Blob 与副本", path: "/admin/api/blobs", columns: [{ key: "sha256", label: "SHA256" }, { key: "media_type", label: "类型" }, { key: "r2_state", label: "副本" }] },
    "/health": { title: "运行状态", path: "/admin/api/health", columns: [{ key: "db", label: "数据库" }, { key: "latency", label: "延迟" }] },
    "/audit": { title: "审计日志", path: "/admin/api/audit", columns: [{ key: "action", label: "动作" }, { key: "username", label: "操作者" }, { key: "result", label: "结果" }] },
    "/settings": { title: "设置", path: "/admin/api/settings", columns: [{ key: "bandbbs_client_id", label: "BandBBS" }, { key: "github_client_id", label: "GitHub" }, { key: "public_url", label: "地址" }] },
  }
  const config = map[path] || map["/audit"]
  const { items, total, q, setQ, page, setPage, error, load } = useRows(config.path)
  const [cleanup, setCleanup] = useState<Row | null>(null)
  const [open, setOpen] = useState<Row | null>(null)
  const requeue = path === "/storage/blobs"
  return (
    <>
      <PageHeader title={config.title} hint="诊断页以表格为主，危险操作（清理）使用对话框。">
        <SearchForm value={q} onChange={setQ} onSubmit={() => { setPage(1); load(q, 1) }} placeholder="搜索" />
        {path === "/health" && (
          <button
            className="btn"
            type="button"
            onClick={() => api.post("/admin/cleanup/preview").then((data) => setCleanup(data as Row))}
          >
            预览清理
          </button>
        )}
      </PageHeader>
      {error && <div className="error">{error}</div>}
      <div className="table-wrap">
        {items.length === 0 && <Empty>没有数据</Empty>}
        {items.length > 0 && (
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
                <tr key={String(row.id || row.sha256 || index)} className="clickable" onClick={() => setOpen(row)}>
                  {config.columns.map((column) => (
                    <td key={column.key}>{String(row[column.key] ?? "—")}</td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
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
              api.post("/admin/api/cleanup", { token: cleanup?.token, confirmation: cleanup?.confirmation }).then(() => {
                toast("清理完成")
                setCleanup(null)
              })
            }
          >
            确认清理
          </button>
        }
      >
        <pre className="summary">{JSON.stringify(cleanup?.preview, null, 2)}</pre>
      </Dialog>
      <Dialog
        open={!!open}
        title="详情"
        wide
        onClose={() => setOpen(null)}
        footer={
          <>
            {requeue && open?.sha256 && (
              <button
                className="btn"
                type="button"
                onClick={() =>
                  api.post(`/admin/api/blobs/${open.sha256}/requeue`).then(() => {
                    toast("已重新入队")
                    setOpen(null)
                    load()
                  })
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
        <FieldList row={open} prefer={config.columns.map((column) => column.key)} />
        <details>
          <summary>原始数据</summary>
          <pre className="summary">{JSON.stringify(open, null, 2)}</pre>
        </details>
      </Dialog>
    </>
  )
}
