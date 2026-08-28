import { useState } from "react"
import { api } from "../api"
import { Dialog, Field, PageHeader, TableState, formatRelative, toast } from "../ui"
import { type Row, useRows } from "./system-helpers"

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