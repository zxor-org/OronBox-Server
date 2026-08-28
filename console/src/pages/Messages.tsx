import { useState } from "react"
import { api } from "../api"
import { Dialog, Field, PageHeader, Pagination, SearchForm, Status, TableState, formatRelative, toast } from "../ui"
import { useRows } from "./system-helpers"

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