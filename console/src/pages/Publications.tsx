import { useEffect, useState } from "react"
import { useOutletContext } from "react-router"
import { api, retryPublication, type Session } from "../api"
import { Empty, PageHeader, Pagination, SearchForm, Status, toast } from "../ui"

type Item = { id: string; name: string; target: string; state: string; error: string }

export function PublicationsPage() {
  const session = useOutletContext<Session>()
  const [items, setItems] = useState<Item[]>([])
  const [total, setTotal] = useState(0)
  const [q, setQ] = useState("")
  const [page, setPage] = useState(1)
  const [error, setError] = useState("")

  const load = (search = q, next = page) =>
    api
      .list<Item>("/admin/api/publications", { q: search, page: next, per_page: 25 })
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
      {error && <div className="error">{error}</div>}
      <div className="table-wrap">
        {items.length === 0 && <Empty>没有发布任务</Empty>}
        {items.length > 0 && (
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
                  <td>{item.target}</td>
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
        )}
      </div>
      <Pagination page={page} total={total} perPage={25} onChange={setPage} />
    </>
  )
}
