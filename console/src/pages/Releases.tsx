import { useState } from "react"
import { api } from "../api"
import { Dialog, Empty, Field, FieldList, PageHeader, Pagination, SearchForm, Status, TableState, formatRelative, toast } from "../ui"
import { type Row, useRows } from "./system-helpers"

export function ReleasesPage() {
  const { items, total, q, setQ, page, setPage, error, loading, load } = useRows("/admin/api/releases")
  const [open, setOpen] = useState(false)
  const [form, setForm] = useState({ version: "", channel: "stable", platform: "all", arch: "all", minimum_version: "", download_url: "", notes_zh: "", notes_en: "" })
  const [selected, setSelected] = useState<Row | null>(null)
  const [detail, setDetail] = useState<Row | null>(null)
  const [detailError, setDetailError] = useState("")
  const [editing, setEditing] = useState(false)
  const [editForm, setEditForm] = useState({ minimum_version: "", notes_zh: "", notes_en: "" })
  const [busy, setBusy] = useState(false)

  const openDetail = (item: Row) => {
    setSelected(item)
    setDetail(item)
    setDetailError("")
    setEditing(false)
    api
      .get<Row>(`/admin/api/releases/${encodeURIComponent(String(item.id))}`)
      .then((value) => {
        setDetail(value)
        setEditForm({ minimum_version: value.minimum_version || "", notes_zh: value.notes_zh || "", notes_en: value.notes_en || "" })
      })
      .catch((err: Error) => setDetailError(err.message))
  }

  const submit = () => {
    api
      .post("/admin/api/releases", form)
      .then(() => {
        toast("已发布")
        setOpen(false)
        load()
      })
      .catch((err: Error) => toast(err.message, "err"))
  }

  const saveNotes = () => {
    if (!selected) return
    setBusy(true)
    api
      .post(`/admin/api/releases/${encodeURIComponent(String(selected.id))}/notes`, editForm)
      .then(() => {
        toast("已保存")
        setEditing(false)
        openDetail(selected)
      })
      .catch((err: Error) => toast(err.message, "err"))
      .finally(() => setBusy(false))
  }

  const setState = (action: string) => {
    if (!selected) return
    setBusy(true)
    api
      .post(`/admin/api/releases/${encodeURIComponent(String(selected.id))}/state`, { action })
      .then(() => {
        toast("已更新")
        openDetail(selected)
        load()
      })
      .catch((err: Error) => toast(err.message, "err"))
      .finally(() => setBusy(false))
  }

  return (
    <>
      <PageHeader title="客户端版本" hint="发布新版本用对话框；点进任意版本可查看历史详情、编辑中英文说明或调整状态。">
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
                <tr key={item.id || index} className="clickable" onClick={() => openDetail(item)}>
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
      <Pagination page={page} total={total} perPage={25} onChange={setPage} />
      <Dialog
        open={open}
        title="发布版本"
        onClose={() => setOpen(false)}
        footer={
          <button className="btn btn-primary" type="button" onClick={submit}>
            发布
          </button>
        }
      >
        <div className="form-grid">
          <Field label="版本（SemVer）">
            <input value={form.version} onChange={(event) => setForm({ ...form, version: event.target.value })} placeholder="例如 1.4.0" />
          </Field>
          <Field label="最低版本">
            <input value={form.minimum_version} onChange={(event) => setForm({ ...form, minimum_version: event.target.value })} placeholder="留空表示不限制" />
          </Field>
          <Field label="通道">
            <select value={form.channel} onChange={(event) => setForm({ ...form, channel: event.target.value })}>
              <option value="stable">stable</option>
              <option value="beta">beta</option>
              <option value="nightly">nightly</option>
            </select>
          </Field>
          <Field label="平台">
            <select value={form.platform} onChange={(event) => setForm({ ...form, platform: event.target.value })}>
              <option value="all">all</option>
              <option value="vela_os">vela_os</option>
              <option value="zepp_os">zepp_os</option>
            </select>
          </Field>
          <Field label="架构">
            <input value={form.arch} onChange={(event) => setForm({ ...form, arch: event.target.value })} placeholder="例如 all、arm64" />
          </Field>
        </div>
        <Field label="下载地址">
          <input value={form.download_url} onChange={(event) => setForm({ ...form, download_url: event.target.value })} />
        </Field>
        <Field label="中文说明">
          <textarea value={form.notes_zh} onChange={(event) => setForm({ ...form, notes_zh: event.target.value })} />
        </Field>
        <Field label="English notes">
          <textarea value={form.notes_en} onChange={(event) => setForm({ ...form, notes_en: event.target.value })} />
        </Field>
      </Dialog>
      <Dialog
        open={!!selected}
        title={detail ? `版本 ${detail.version}` : "版本详情"}
        wide
        onClose={() => setSelected(null)}
        footer={
          detail && !detail.revoked_at ? (
            <div className="actions">
              {!detail.enabled ? <button className="btn" type="button" disabled={busy} onClick={() => setState("enable")}>启用</button> : null}
              {detail.enabled ? <button className="btn" type="button" disabled={busy} onClick={() => setState("disable")}>停用</button> : null}
              <button className="btn btn-danger" type="button" disabled={busy} onClick={() => setState("revoke")}>撤销</button>
            </div>
          ) : (
            <span className="hint">已撤销的版本不可再调整</span>
          )
        }
      >
        {detailError ? <div className="table-state error">{detailError}</div> : null}
        {detail ? (
          <div className="stack">
            <FieldList
              row={detail}
              prefer={["version", "state", "channel", "platform", "arch", "minimum_version", "download_url", "published_at", "updated_at", "creator"]}
            />
            <div className="kv">
              <div><dt>中文说明</dt><dd className="notes">{detail.notes_zh || "—"}</dd></div>
              <div><dt>English notes</dt><dd className="notes">{detail.notes_en || "—"}</dd></div>
            </div>
            {!detail.revoked_at ? (
              <div>
                <button className="btn" type="button" disabled={busy} onClick={() => setEditing(true)}>
                  编辑说明与最低版本
                </button>
              </div>
            ) : null}
          </div>
        ) : (
          <Empty>加载中…</Empty>
        )}
      </Dialog>
      <Dialog
        open={editing}
        title={selected ? `编辑 ${selected.version} 说明` : "编辑说明"}
        onClose={() => setEditing(false)}
        footer={
          <button className="btn btn-primary" type="button" disabled={busy} onClick={saveNotes}>
            保存
          </button>
        }
      >
        <Field label="最低版本">
          <input value={editForm.minimum_version} onChange={(event) => setEditForm({ ...editForm, minimum_version: event.target.value })} />
        </Field>
        <Field label="中文说明">
          <textarea value={editForm.notes_zh} onChange={(event) => setEditForm({ ...editForm, notes_zh: event.target.value })} />
        </Field>
        <Field label="English notes">
          <textarea value={editForm.notes_en} onChange={(event) => setEditForm({ ...editForm, notes_en: event.target.value })} />
        </Field>
      </Dialog>
    </>
  )
}