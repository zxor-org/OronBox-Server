import { useEffect, useState } from "react"
import { useNavigate, useParams } from "react-router"
import { CheckCircleIcon, ProhibitIcon, FolderOpenIcon } from "@phosphor-icons/react"
import { api, bulkReviews, decideReview, getReview, listReviews, saveReviewChecklist } from "../api"
import {
  Dialog,
  Empty,
  Field,
  PageHeader,
  PublicationCards,
  SearchForm,
  Status,
  TargetChips,
  eventLabel,
  formatBytes,
  formatRelative,
  kindLabel,
  mediaRoleLabel,
  paidLabel,
  toast,
} from "../ui"

type Item = {
  id: string
  name: string
  owner: string
  kind: string
  revision_number: number
  updated_at: string
  overdue?: boolean
  waiting?: string
  priority_label?: string
  reports?: number
  owner_rejections?: number
  reviewer?: string
  targets?: string[]
  state?: string
}

type Artifact = {
  name: string
  version: string
  devices?: string[]
  device_bindings?: { id: string; display_name: string; codename: string }[]
  url: string
  package_id?: string
  package_format?: string
  size?: number
  sha256?: string
  analysis?: unknown
}

type Detail = {
  review: {
    id: string
    state: string
    owner: string
    kind: string
    revision_number: number
    items?: string[]
    curation_grade?: string
    resource_id?: string
    targets?: string[]
    reviewer?: string
  }
  current: {
    name: string
    summary: string
    paid_type: string
    attributes?: string[]
    publication_plan?: unknown
    media?: { sha256: string; url: string; role: string; width?: number; height?: number }[]
    artifacts?: Artifact[]
  } | null
  diff?: { has_base: boolean; fields?: { label: string; before: string; after: string }[] }
  events?: { id: string; event: string; actor: string; note: string; created_at: string }[]
  checklist_catalog?: string[]
}

type Reviewer = { id: string; username: string; role: string }

export function ReviewPage() {
  const { id } = useParams()
  const navigate = useNavigate()
  const [items, setItems] = useState<Item[]>([])
  const [detail, setDetail] = useState<Detail | null>(null)
  const [q, setQ] = useState("")
  const [kind, setKind] = useState("")
  const [target, setTarget] = useState("")
  const [state, setState] = useState("pending")
  const [sort, setSort] = useState("sla")
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)
  const [rejectOpen, setRejectOpen] = useState(false)
  const [bulkOpen, setBulkOpen] = useState(false)
  const [note, setNote] = useState("")
  const [grade, setGrade] = useState("standard")
  const [checked, setChecked] = useState<string[]>([])
  const [selected, setSelected] = useState<string[]>([])
  const [preview, setPreview] = useState("")
  const [reviewers, setReviewers] = useState<Reviewer[]>([])
  const [bulkNote, setBulkNote] = useState("")
  const [analysis, setAnalysis] = useState<Artifact | null>(null)

  const load = () =>
    listReviews(state, kind, { sort, target, q })
      .then((data) => setItems((data.items || []) as Item[]))
      .catch((err: Error) => setError(err.message))

  useEffect(() => {
    load()
  }, [kind, sort, target, state])

  useEffect(() => {
    api.get<{ reviewers?: Reviewer[] }>("/admin/api/catalog").then((data) => setReviewers(data.reviewers || [])).catch(() => null)
  }, [])

  useEffect(() => {
    if (!id && items[0] && state === "pending") navigate(`/review/${items[0].id}`, { replace: true })
  }, [id, items, navigate, state])

  useEffect(() => {
    if (!id) {
      setDetail(null)
      return
    }
    getReview(id)
      .then((data) => {
        const value = data as Detail
        setDetail(value)
        setGrade(value.review.curation_grade === "featured" ? "featured" : "standard")
        setChecked(value.review.items?.length ? value.review.items : value.checklist_catalog || [])
      })
      .catch((err: Error) => setError(err.message))
  }, [id])

  const catalog = detail?.checklist_catalog || ["图片合规", "安装包可安装", "描述与实际功能一致", "设备适配正确", "发布计划完整"]

  const decide = async (decision: "approve" | "reject") => {
    if (!id || !detail) return
    setBusy(true)
    setError("")
    try {
      await decideReview(id, decision, note, grade, checked, detail.current?.attributes || [])
      toast(decision === "approve" ? "已通过" : "已退回")
      setRejectOpen(false)
      setNote("")
      const next = items.filter((item) => item.id !== id)
      setItems(next)
      navigate(next[0] ? `/review/${next[0].id}` : "/review")
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <PageHeader hint="按超时排队。先看发布目标、图片和安装包，再决定通过或退回。">
        <SearchForm
          value={q}
          onChange={setQ}
          onSubmit={() => load()}
          placeholder="资源、创作者或审核 ID"
        />
        <button className="btn" type="button" disabled={!selected.length} onClick={() => setBulkOpen(true)}>
          批量 {selected.length || ""}
        </button>
      </PageHeader>
      <div className="toolbar">
        <select value={state} onChange={(event) => setState(event.target.value)}>
          <option value="pending">待审核</option>
          <option value="approved">已通过</option>
          <option value="rejected">已退回</option>
          <option value="superseded">已替代</option>
          <option value="">全部状态</option>
        </select>
        <select value={kind} onChange={(event) => setKind(event.target.value)}>
          <option value="">全部类型</option>
          <option value="watchface">表盘</option>
          <option value="quickapp">快应用</option>
        </select>
        <select value={target} onChange={(event) => setTarget(event.target.value)}>
          <option value="">全部目标</option>
          <option value="oronbox">OronBox</option>
          <option value="bandbbs">米坛</option>
          <option value="astrobox">AstroBox</option>
        </select>
        <select value={sort} onChange={(event) => setSort(event.target.value)}>
          <option value="sla">按超时</option>
          <option value="priority">按优先级</option>
          <option value="updated_desc">最近更新</option>
        </select>
      </div>
      <div className="split">
        <div className="queue">
          {items.length === 0 && <Empty>没有符合筛选的审核单</Empty>}
          {items.map((item) => (
            <button key={item.id} type="button" className={`queue-item ${item.id === id ? "selected" : ""}`} onClick={() => navigate(`/review/${item.id}`)}>
              <div className="title">
                <input
                  type="checkbox"
                  checked={selected.includes(item.id)}
                  onClick={(event) => event.stopPropagation()}
                  onChange={(event) => setSelected(event.target.checked ? [...selected, item.id] : selected.filter((value) => value !== item.id))}
                />
                <span>{item.name || "未命名"}</span>
              </div>
              <div className="meta">
                <span>{item.owner}</span>
                <span>{kindLabel[item.kind] || item.kind}</span>
                <span>#{item.revision_number}</span>
                {item.priority_label ? <span className="chip">{item.priority_label}</span> : null}
              </div>
              <TargetChips targets={item.targets} />
              <div className={`hint ${item.overdue ? "overdue" : ""}`}>
                {item.overdue ? "已超时" : item.waiting || formatRelative(item.updated_at)}
                {item.reports ? ` · 举报 ${item.reports}` : ""}
                {item.owner_rejections ? ` · 退回过 ${item.owner_rejections}` : ""}
                {item.reviewer ? ` · ${item.reviewer}` : " · 未分配"}
              </div>
            </button>
          ))}
        </div>
        <div className="detail">
          {!id && <div className="detail-empty">从左边选一条</div>}
          {id && detail && (
            <>
              <h2 className="detail-title">{detail.current?.name || "未命名"}</h2>
              <div className="detail-meta">
                {detail.review.owner} · {kindLabel[detail.review.kind] || detail.review.kind}
                {detail.current?.paid_type ? ` · ${paidLabel[detail.current.paid_type] || detail.current.paid_type}` : ""}
                {` · 第 ${detail.review.revision_number} 版`}
                {detail.review.reviewer ? ` · 审核员 ${detail.review.reviewer}` : " · 未分配"}
                {" "}
                <Status value={detail.review.state} />
              </div>
              {detail.current?.summary && <p className="summary">{detail.current.summary}</p>}
              <PublicationCards plan={detail.current?.publication_plan} targets={detail.review.targets} />
              <div className="panel">
                <h3>预览与媒体</h3>
                <p className="hint">点图看原图。没有图片时先确认是不是规范要求的漏传。</p>
                {detail.current?.media?.length ? (
                  <div className="gallery">
                    {detail.current.media.map((media) => (
                      <figure key={media.sha256} className="media-card">
                        <img src={media.url} alt={mediaRoleLabel[media.role] || media.role} onClick={() => setPreview(media.url)} />
                        <figcaption>
                          {mediaRoleLabel[media.role] || media.role}
                          {media.width && media.height ? ` · ${media.width}×${media.height}` : ""}
                        </figcaption>
                      </figure>
                    ))}
                  </div>
                ) : (
                  <Empty>本次提交没有任何图片</Empty>
                )}
              </div>
              <div className="panel">
                <h3>资源文件</h3>
                <p className="hint">先确认包能下，再看绑定了哪些设备。</p>
                {detail.current?.artifacts?.length ? (
                  <div className="files">
                    {detail.current.artifacts.map((file) => (
                      <div className="file" key={file.url || file.sha256}>
                        <div>
                          <div>{file.name}</div>
                          <small>
                            {file.version}
                            {file.package_format ? ` · ${file.package_format}` : ""}
                            {file.size ? ` · ${formatBytes(file.size)}` : ""}
                          </small>
                          <div className="chip-row">
                            {(file.device_bindings || []).length
                              ? file.device_bindings!.map((device) => (
                                  <span className="chip" key={device.id}>
                                    {device.display_name || device.codename}
                                  </span>
                                ))
                              : (file.devices || []).map((device) => (
                                  <span className="chip" key={device}>
                                    {device}
                                  </span>
                                ))}
                            {!file.device_bindings?.length && !file.devices?.length ? <span className="chip overdue">未绑定设备</span> : null}
                          </div>
                        </div>
                        <div className="row-actions">
                          {file.analysis ? (
                            <button className="btn" type="button" onClick={() => setAnalysis(file)}>
                              分析
                            </button>
                          ) : null}
                          <a className="btn" href={`${file.url}?download=1`}>
                            下载
                          </a>
                        </div>
                      </div>
                    ))}
                  </div>
                ) : (
                  <Empty>本次提交没有资源文件</Empty>
                )}
              </div>
              {detail.diff?.has_base && !!detail.diff.fields?.length && (
                <div className="panel">
                  <h3>相对上一版的变更</h3>
                  <div className="files">
                    {detail.diff.fields.map((field) => (
                      <div className="file" key={field.label}>
                        <div>
                          <div>{field.label}</div>
                          <small>
                            {field.before || "（空）"} → {field.after || "（空）"}
                          </small>
                        </div>
                      </div>
                    ))}
                  </div>
                </div>
              )}
              <div className="panel">
                <h3>审核清单</h3>
                <div className="choice-grid">
                  {catalog.map((item) => (
                    <label key={item}>
                      <input type="checkbox" checked={checked.includes(item)} onChange={(event) => setChecked(event.target.checked ? [...checked, item] : checked.filter((value) => value !== item))} />
                      {item}
                    </label>
                  ))}
                </div>
                <div className="row-actions" style={{ marginTop: 12 }}>
                  <button
                    className="btn"
                    type="button"
                    onClick={() =>
                      saveReviewChecklist(id, checked)
                        .then(() => toast("清单已保存"))
                        .catch((err: Error) => toast(err.message, "err"))
                    }
                  >
                    保存进度
                  </button>
                  <select value={grade} onChange={(event) => setGrade(event.target.value)}>
                    <option value="standard">普通</option>
                    <option value="featured">精选</option>
                  </select>
                </div>
              </div>
              {!!detail.events?.length && (
                <ol className="timeline">
                  {detail.events.map((event) => (
                    <li key={event.id}>
                      <strong>{eventLabel[event.event] || event.event}</strong> · {event.actor} · {formatRelative(event.created_at)}
                      {event.note ? <div className="hint">{event.note}</div> : null}
                    </li>
                  ))}
                </ol>
              )}
              {error && <div className="error">{error}</div>}
              {detail.review.state === "pending" && (
                <div className="actions sticky-actions">
                  <button className="btn btn-primary" disabled={busy} onClick={() => decide("approve")}>
                    <CheckCircleIcon size={16} />
                    批准发布
                  </button>
                  <button className="btn btn-danger" disabled={busy} onClick={() => setRejectOpen(true)}>
                    <ProhibitIcon size={16} />
                    退回修改
                  </button>
                  {detail.review.resource_id && (
                    <button className="btn" type="button" onClick={() => navigate(`/resources/${detail.review.resource_id}`)}>
                      <FolderOpenIcon size={16} />
                      资源工作区
                    </button>
                  )}
                </div>
              )}
            </>
          )}
        </div>
      </div>
      <Dialog
        open={rejectOpen}
        title="退回这一版"
        hint="作者会看到这段话。"
        onClose={() => setRejectOpen(false)}
        footer={
          <>
            <button className="btn" type="button" onClick={() => setRejectOpen(false)}>
              取消
            </button>
            <button className="btn btn-danger" disabled={busy || !note.trim()} onClick={() => decide("reject")}>
              退回
            </button>
          </>
        }
      >
        <textarea value={note} onChange={(event) => setNote(event.target.value)} placeholder="需要改什么" />
      </Dialog>
      <Dialog open={bulkOpen} title="批量处理" hint={`已选 ${selected.length} 条，全有或全无。`} onClose={() => setBulkOpen(false)} footer={<button className="btn" type="button" onClick={() => setBulkOpen(false)}>完成</button>}>
        <Field label="分配给">
          <select
            onChange={async (event) => {
              try {
                await bulkReviews({ action: "assign", ids: selected, reviewer_id: event.target.value })
                toast(event.target.value ? "已分配" : "已取消分配")
                load()
              } catch (err) {
                toast((err as Error).message, "err")
              }
            }}
          >
            <option value="">选择审核员后立刻生效</option>
            {reviewers.map((reviewer) => (
              <option key={reviewer.id} value={reviewer.id}>
                {reviewer.username}（{reviewer.role}）
              </option>
            ))}
          </select>
        </Field>
        <Field label="优先级">
          <select
            onChange={async (event) => {
              if (!event.target.value) return
              try {
                await bulkReviews({ action: "priority", ids: selected, priority: Number(event.target.value) })
                toast("优先级已更新")
                load()
              } catch (err) {
                toast((err as Error).message, "err")
              }
            }}
          >
            <option value="">选择后立即生效</option>
            <option value="0">普通</option>
            <option value="1">关注</option>
            <option value="2">高</option>
            <option value="3">紧急</option>
          </select>
        </Field>
        <Field label="批量退回理由">
          <textarea value={bulkNote} onChange={(event) => setBulkNote(event.target.value)} placeholder="批量退回必填" />
        </Field>
        <div className="row-actions">
          <button
            className="btn btn-primary"
            type="button"
            onClick={() =>
              bulkReviews({ action: "approve", ids: selected, grade })
                .then(() => {
                  toast("已批量通过")
                  setSelected([])
                  load()
                })
                .catch((err: Error) => toast(err.message, "err"))
            }
          >
            批量通过
          </button>
          <button
            className="btn btn-danger"
            type="button"
            disabled={!bulkNote.trim()}
            onClick={() =>
              bulkReviews({ action: "reject", ids: selected, note: bulkNote })
                .then(() => {
                  toast("已批量退回")
                  setSelected([])
                  setBulkNote("")
                  load()
                })
                .catch((err: Error) => toast(err.message, "err"))
            }
          >
            批量退回
          </button>
        </div>
      </Dialog>
      <Dialog open={!!preview} title="预览" wide onClose={() => setPreview("")}>
        {preview ? <img src={preview} alt="" style={{ width: "100%", borderRadius: 8 }} /> : null}
      </Dialog>
      <Dialog open={!!analysis} title="服务端分析" wide onClose={() => setAnalysis(null)}>
        <pre className="summary">{JSON.stringify(analysis?.analysis, null, 2)}</pre>
      </Dialog>
    </>
  )
}
