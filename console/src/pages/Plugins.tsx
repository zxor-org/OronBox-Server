import { useEffect, useState } from "react"
import { api, reviewPlugin } from "../api"
import { Dialog, Empty, Field, PageHeader, Pagination, SearchForm, Status, TableState, formatBytes, formatRelative, toast } from "../ui"

type Plugin = {
  id: string
  name: string
  state: string
  version?: string
  author?: string
  pending_version_id?: string
  description?: string
  runtime?: string
  package_size?: number
  updated_at?: string
}

export function PluginsPage() {
  const [items, setItems] = useState<Plugin[]>([])
  const [total, setTotal] = useState(0)
  const [q, setQ] = useState("")
  const [page, setPage] = useState(1)
  const [error, setError] = useState("")
  const [loading, setLoading] = useState(true)
  const [rejecting, setRejecting] = useState<Plugin | null>(null)
  const [note, setNote] = useState("")
  const [managing, setManaging] = useState<Plugin | null>(null)
  const [metaForm, setMetaForm] = useState({ name: "", author: "", description: "" })
  const [stateTarget, setStateTarget] = useState<Plugin | null>(null)
  const [stateAction, setStateAction] = useState("delisted")
  const [stateReason, setStateReason] = useState("")
  const [busy, setBusy] = useState(false)

  const load = (search = q, next = page) => {
    setLoading(true)
    setError("")
    return api
      .list<Plugin>("/admin/api/plugins", { q: search, page: next, per_page: 25 })
      .then((data) => {
        setItems(data.items || [])
        setTotal(data.total || 0)
      })
      .catch((err: Error) => setError(err.message))
      .finally(() => setLoading(false))
  }

  useEffect(() => {
    load(q, page)
  }, [page])

  const openManage = (item: Plugin) => {
    setManaging(item)
    setMetaForm({ name: item.name || "", author: item.author || "", description: item.description || "" })
  }

  const saveMetadata = () => {
    if (!managing) return
    setBusy(true)
    api
      .post(`/admin/api/plugins/${encodeURIComponent(managing.id)}/metadata`, metaForm)
      .then(() => {
        toast("已提交信息修订")
        setManaging(null)
        load()
      })
      .catch((err: Error) => toast(err.message, "err"))
      .finally(() => setBusy(false))
  }

  const changeState = () => {
    if (!stateTarget) return
    setBusy(true)
    api
      .post(`/admin/api/plugins/${encodeURIComponent(stateTarget.id)}/state`, { state: stateAction, reason: stateReason })
      .then(() => {
        toast("已更新")
        setStateTarget(null)
        setStateReason("")
        load()
      })
      .catch((err: Error) => toast(err.message, "err"))
      .finally(() => setBusy(false))
  }

  return (
    <>
      <PageHeader title="插件" hint="待审版本可以直接通过。退回必须写理由。">
        <SearchForm
          value={q}
          onChange={setQ}
          onSubmit={() => {
            setPage(1)
            load(q, 1)
          }}
          placeholder="搜索插件"
        />
      </PageHeader>
      <div className="table-wrap">
        <TableState loading={loading} error={error} onRetry={() => void load()} isEmpty={!items.length} empty={q ? "没有匹配的插件" : "没有插件"}>
          <table>
            <thead>
              <tr>
                <th>名称</th>
                <th>作者</th>
                <th>运行时</th>
                <th>包大小</th>
                <th>状态</th>
                <th>更新</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {items.map((item) => (
                <tr key={item.id}>
                  <td>
                    <div>{item.name}</div>
                    {item.description ? <small className="hint">{item.description}</small> : null}
                  </td>
                  <td>{item.author || "—"}</td>
                  <td>{item.runtime || "—"}</td>
                  <td>{item.package_size ? formatBytes(Number(item.package_size)) : "—"}</td>
                  <td>
                    <Status value={item.state} />
                  </td>
                  <td>{formatRelative(item.updated_at)}</td>
                  <td>
                    {item.pending_version_id ? (
                      <div className="row-actions">
                        <button
                          className="btn btn-primary"
                          type="button"
                          onClick={() =>
                            reviewPlugin(item.id, "approve")
                              .then(() => {
                                toast("已通过")
                                load()
                              })
                              .catch((err: Error) => setError(err.message))
                          }
                        >
                          通过
                        </button>
                        <button className="btn btn-danger" type="button" onClick={() => setRejecting(item)}>
                          退回
                        </button>
                      </div>
                    ) : (
                      <div className="row-actions">
                        <button className="btn" type="button" onClick={() => openManage(item)}>编辑信息</button>
                        {item.state === "listed" ? (
                          <button className="btn" type="button" onClick={() => { setStateTarget(item); setStateAction("delisted"); setStateReason("") }}>下架</button>
                        ) : item.state === "delisted" ? (
                          <button className="btn" type="button" onClick={() => { setStateTarget(item); setStateAction("listed"); setStateReason("") }}>重新上架</button>
                        ) : null}
                      </div>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </TableState>
      </div>
      <Pagination page={page} total={total} perPage={25} onChange={setPage} />
      <Dialog
        open={!!rejecting}
        title="退回插件"
        hint="作者会看到这段话。"
        onClose={() => setRejecting(null)}
        footer={
          <>
            <button className="btn" type="button" onClick={() => setRejecting(null)}>
              取消
            </button>
            <button
              className="btn btn-danger"
              disabled={!note.trim()}
              onClick={async () => {
                if (!rejecting) return
                try {
                  await reviewPlugin(rejecting.id, "reject", note)
                  toast("已退回")
                  setRejecting(null)
                  setNote("")
                  await load()
                } catch (err) {
                  setError((err as Error).message)
                }
              }}
            >
              退回
            </button>
          </>
        }
      >
        <Field label="理由">
          <textarea value={note} onChange={(event) => setNote(event.target.value)} />
        </Field>
      </Dialog>
      <Dialog
        open={!!managing}
        title={`编辑 ${managing?.name || "插件"} 信息`}
        hint="提交后进入待审版本，通过后生效。"
        onClose={() => setManaging(null)}
        footer={
          <button className="btn btn-primary" type="button" disabled={busy} onClick={saveMetadata}>
            提交修订
          </button>
        }
      >
        <Field label="名称">
          <input value={metaForm.name} onChange={(event) => setMetaForm({ ...metaForm, name: event.target.value })} />
        </Field>
        <Field label="作者">
          <input value={metaForm.author} onChange={(event) => setMetaForm({ ...metaForm, author: event.target.value })} />
        </Field>
        <Field label="描述">
          <textarea value={metaForm.description} onChange={(event) => setMetaForm({ ...metaForm, description: event.target.value })} />
        </Field>
      </Dialog>
      <Dialog
        open={!!stateTarget}
        title={stateAction === "delisted" ? "下架插件" : "重新上架插件"}
        hint={stateAction === "delisted" ? "下架后用户将无法发现该插件。" : "重新上架该插件。"}
        onClose={() => setStateTarget(null)}
        footer={
          <button className={`btn ${stateAction === "delisted" ? "btn-danger" : "btn-primary"}`} type="button" disabled={busy} onClick={changeState}>
            确认
          </button>
        }
      >
        <Field label="原因（可选）">
          <textarea value={stateReason} onChange={(event) => setStateReason(event.target.value)} />
        </Field>
      </Dialog>
    </>
  )
}
