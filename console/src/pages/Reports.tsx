import { useState } from "react"
import { api } from "../api"
import { Dialog, Field, FieldList, PageHeader, Pagination, SearchForm, Status, TableState, formatRelative, toast } from "../ui"
import { type Row, useRows } from "./system-helpers"

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