import { useEffect, useState } from "react"
import { useOutletContext } from "react-router"
import { api, retryPublication, type Session } from "../api"
import { Empty, PageHeader, Pagination, SearchForm, Status, TableState, targetLabel, toast } from "../ui"

type Item = { id: string; name: string; target: string; state: string; error: string }

export function PublicationsPage() {
  const session = useOutletContext<Session>()
  const [items, setItems] = useState<Item[]>([])
  const [total, setTotal] = useState(0)
  const [q, setQ] = useState("")
  const [page, setPage] = useState(1)
  const [target, setTarget] = useState("")
  const [state, setState] = useState("")
  const [error, setError] = useState("")
  const [loading, setLoading] = useState(true)

  const load = (search = q, next = page) => {
    setLoading(true)
    setError("")
    return api
      .list<Item>("/admin/api/publications", { q: search, page: next, per_page: 25, target, state })
      .then((data) => {
        setItems(data.items || [])
        setTotal(data.total || 0)
      })
      .catch((err: Error) => setError(err.message))
      .finally(() => setLoading(false))
  }

  useEffect(() => {
    load(q, page)
  }, [page, target, state])

  return (
    <>
      <PageHeader title="发布任务" hint="失败的任务可以重试。点重试会重新入队。">
        <SearchForm
          value={q}
          onChange={setQ}
          onSubmit={() => {
            setPage(1)
            load(q, 1)
          }}
          placeholder="搜索资源或目标"
        />
        {session.role === "admin" && (
          <button
            className="btn"
            type="button"
            onClick={() =>
              api
                .post("/admin/api/publications/retry-failed")
                .then(() => {
                  toast("已重试失败任务")
                  load()
                })
                .catch((err: Error) => setError(err.message))
            }
          >
            重试全部失败
          </button>
        )}
      </PageHeader>
      <div className="toolbar">
        <select value={target} onChange={(event) => { setTarget(event.target.value); setPage(1) }}>
          <option value="">全部目标</option>
          <option value="oronbox">OronBox</option>
          <option value="bandbbs">米坛</option>
          <option value="astrobox">AstroBox</option>
        </select>
        <select value={state} onChange={(event) => { setState(event.target.value); setPage(1) }}>
          <option value="">全部状态</option>
          <option value="pending">排队中</option>
          <option value="running">执行中</option>
          <option value="reviewing">外部审核中</option>
          <option value="failed">失败</option>
          <option value="published">已发布</option>
        </select>
      </div>
      <div className="table-wrap">
        <TableState loading={loading} error={error} onRetry={() => void load()} isEmpty={!items.length} empty={q ? "没有匹配的发布任务" : "没有发布任务"}>
          <table>
            <thead>
              <tr>
                <th>资源</th>
                <th>目标</th>
                <th>状态</th>
                <th>错误</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {items.map((item) => (
                <tr key={item.id}>
                  <td>{item.name}</td>
                  <td>{targetLabel(item.target)}</td>
                  <td>
                    <Status value={item.state} />
                  </td>
                  <td>{item.error || "—"}</td>
                  <td>
                    {item.state === "failed" && session.role === "admin" && (
                      <button
                        className="btn"
                        type="button"
                        onClick={() =>
                          retryPublication(item.id)
                            .then(() => {
                              toast("已重试")
                              load()
                            })
                            .catch((err: Error) => setError(err.message))
                        }
                      >
                        重试
                      </button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </TableState>
      </div>
      <Pagination page={page} total={total} perPage={25} onChange={setPage} />
    </>
  )
}
