import { useEffect, useState } from "react"
import { api, reviewPlugin } from "../api"
import { Dialog, Empty, Field, PageHeader, Pagination, SearchForm, Status, toast } from "../ui"

type Plugin = {
  id: string
  name: string
  state: string
  version?: string
  author?: string
  pending_version_id?: string
  description?: string
}

export function PluginsPage() {
  const [items, setItems] = useState<Plugin[]>([])
  const [total, setTotal] = useState(0)
  const [q, setQ] = useState("")
  const [page, setPage] = useState(1)
  const [error, setError] = useState("")
  const [rejecting, setRejecting] = useState<Plugin | null>(null)
  const [note, setNote] = useState("")

  const load = (search = q, next = page) =>
    api
      .list<Plugin>("/admin/api/plugins", { q: search, page: next, per_page: 25 })
      .then((data) => {
        setItems(data.items || [])
        setTotal(data.total || 0)
      })
      .catch((err: Error) => setError(err.message))

  useEffect(() => {
    load(q, page)
  }, [page])

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
      {error && <div className="error">{error}</div>}
      <div className="table-wrap">
        {items.length === 0 && <Empty>没有插件</Empty>}
        {items.length > 0 && (
          <table>
            <thead>
              <tr>
                <th>名称</th>
                <th>作者</th>
                <th>状态</th>
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
                  <td>
                    <Status value={item.state} />
                  </td>
                  <td>
                    {item.pending_version_id && (
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
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
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
    </>
  )
}
