import { useState } from "react"
import { api } from "../api"
import { Dialog, Field, PageHeader, Pagination, SearchForm, Status, TableState, toast } from "../ui"
import { type Row, useRows } from "./system-helpers"

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