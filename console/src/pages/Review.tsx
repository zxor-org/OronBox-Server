import { useEffect, useState } from "react"
import { useNavigate, useParams } from "react-router"
import { bulkReviews, decideReview, getReview, listReviews, saveReviewChecklist } from "../api"
import { Dialog, Empty, Field, PageHeader, Status, formatRelative, kindLabel, paidLabel, toast } from "../ui"

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
  reviewer?: string
}

type Detail = {
  review: { id: string; state: string; owner: string; kind: string; revision_number: number; items?: string[]; curation_grade?: string; resource_id?: string }
  current: {
    name: string
    summary: string
    paid_type: string
    attributes?: string[]
    media?: { sha256: string; url: string; role: string }[]
    artifacts?: { name: string; version: string; devices?: string[]; url: string }[]
  } | null
  diff?: { has_base: boolean; fields?: { label: string; before: string; after: string }[] }
  events?: { id: string; event: string; actor: string; note: string; created_at: string }[]
  checklist_catalog?: string[]
}

export function ReviewPage() {
  const { id } = useParams()
  const navigate = useNavigate()
  const [items, setItems] = useState<Item[]>([])
  const [detail, setDetail] = useState<Detail | null>(null)
  const [kind, setKind] = useState("")
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

  const load = () =>
    listReviews("pending", kind, { sort })
      .then((data) => setItems((data.items || []) as Item[]))
      .catch((err: Error) => setError(err.message))

  useEffect(() => {
    load()
  }, [kind, sort])

  useEffect(() => {
    if (!id && items[0]) navigate(`/review/${items[0].id}`, { replace: true })
  }, [id, items, navigate])

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
      <PageHeader title="待审核" hint="从队列处理提交。退回和批量操作使用对话框。">
        <select className="search" value={kind} onChange={(event) => setKind(event.target.value)}>
          <option value="">全部类型</option>
          <option value="watchface">表盘</option>
          <option value="quickapp">快应用</option>
        </select>
        <select className="search" value={sort} onChange={(event) => setSort(event.target.value)}>
          <option value="sla">按超时</option>
          <option value="priority">按优先级</option>
          <option value="updated_desc">最近更新</option>
        </select>
        <button className="btn" type="button" disabled={!selected.length} onClick={() => setBulkOpen(true)}>
          批量 {selected.length || ""}
        </button>
      </PageHeader>
      <div className="split">
        <div className="queue">
          {items.length === 0 && <Empty>没有待审核的版本</Empty>}
          {items.map((item) => (
            <button key={item.id} type="button" className={`queue-item ${item.id === id ? "selected" : ""}`} onClick={() => navigate(`/review/${item.id}`)}>
              <div className="title">
                <input
                  type="checkbox"
                  checked={selected.includes(item.id)}
                  onClick={(event) => event.stopPropagation()}
                  onChange={(event) => setSelected(event.target.checked ? [...selected, item.id] : selected.filter((value) => value !== item.id))}
                />{" "}
                {item.name || "未命名"}
              </div>
              <div className="meta">
                <span>{item.owner}</span>
                <span>{kindLabel[item.kind] || item.kind}</span>
                <span>#{item.revision_number}</span>
                {item.priority_label ? <span className="chip">{item.priority_label}</span> : null}
              </div>
              <div className={`hint ${item.overdue ? "overdue" : ""}`}>
                {item.overdue ? "已超时" : item.waiting || formatRelative(item.updated_at)}
                {item.reports ? ` · 举报 ${item.reports}` : ""}
                {item.reviewer ? ` · ${item.reviewer}` : ""}
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
                {` · 第 ${detail.review.revision_number} 版 `}
                <Status value={detail.review.state} />
              </div>
              {detail.current?.summary && <p className="summary">{detail.current.summary}</p>}
              {!!detail.current?.media?.length && (
                <div className="gallery">
                  {detail.current.media.map((media) => (
                    <img key={media.sha256} src={media.url} alt={media.role} onClick={() => setPreview(media.url)} />
                  ))}
                </div>
              )}
              {detail.diff?.has_base && !!detail.diff.fields?.length && (
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
              )}
              {!!detail.current?.artifacts?.length && (
                <div className="files">
                  {detail.current.artifacts.map((file) => (
                    <div className="file" key={file.url}>
                      <div>
                        <div>{file.name}</div>
                        <small>
                          {file.version} {file.devices?.length ? `· ${file.devices.join("、")}` : ""}
                        </small>
                      </div>
                      <a className="btn" href={file.url}>
                        下载
                      </a>
                    </div>
                  ))}
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
                      <strong>{event.event}</strong> · {event.actor} · {formatRelative(event.created_at)}
                      {event.note ? <div className="hint">{event.note}</div> : null}
                    </li>
                  ))}
                </ol>
              )}
              {error && <div className="error">{error}</div>}
              {detail.review.state === "pending" && (
                <div className="actions sticky-actions">
                  <button className="btn btn-primary" disabled={busy} onClick={() => decide("approve")}>
                    通过
                  </button>
                  <button className="btn btn-danger" disabled={busy} onClick={() => setRejectOpen(true)}>
                    退回
                  </button>
                  {detail.review.resource_id && (
                    <button className="btn" type="button" onClick={() => navigate(`/resources/${detail.review.resource_id}`)}>
                      打开资源
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
      <Dialog open={bulkOpen} title="批量处理" hint={`已选 ${selected.length} 条。`} onClose={() => setBulkOpen(false)} footer={<button className="btn" type="button" onClick={() => setBulkOpen(false)}>完成</button>}>
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
      </Dialog>
      <Dialog open={!!preview} title="预览" wide onClose={() => setPreview("")}>
        {preview ? <img src={preview} alt="" style={{ width: "100%", borderRadius: 8 }} /> : null}
      </Dialog>
    </>
  )
}
