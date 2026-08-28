import { useEffect, useState } from "react"
import { decideComment, listComments, type CommentItem } from "../api"
import { Dialog, Empty, Field, PageHeader, Pagination, SearchForm, Status, TableState, toast } from "../ui"

export function CommentsPage() {
  const [items, setItems] = useState<CommentItem[]>([])
  const [total, setTotal] = useState(0)
  const [q, setQ] = useState("")
  const [page, setPage] = useState(1)
  const [state, setState] = useState("review")
  const [error, setError] = useState("")
  const [loading, setLoading] = useState(true)
  const [hiding, setHiding] = useState<CommentItem | null>(null)
  const [note, setNote] = useState("")

  const load = (search = q, next = page, nextState = state) => {
    setLoading(true)
    setError("")
    return listComments(nextState, search, next)
      .then((data) => {
        setItems(data.items || [])
        setTotal(data.total || 0)
      })
      .catch((err: Error) => setError(err.message))
      .finally(() => setLoading(false))
  }

  useEffect(() => {
    load(q, page, state)
  }, [page, state])

  const approve = async (item: CommentItem) => {
    try {
      await decideComment(item.id, "approve")
      toast("已通过")
      await load()
    } catch (err) {
      setError((err as Error).message)
    }
  }

  return (
    <>
      <PageHeader title="评论审核" hint="点通过立刻放行。隐藏必须填写理由。">
        <SearchForm
          value={q}
          onChange={setQ}
          onSubmit={() => {
            setPage(1)
            load(q, 1, state)
          }}
          placeholder="搜索评论或用户"
        />
      </PageHeader>
      <div className="toolbar">
        <select
          value={state}
          onChange={(event) => {
            setState(event.target.value)
            setPage(1)
          }}
        >
          <option value="review">待审</option>
          <option value="visible">已通过</option>
          <option value="hidden">已隐藏</option>
        </select>
      </div>
      <div className="table-wrap">
        <TableState loading={loading} error={error} onRetry={() => void load()} isEmpty={!items.length} empty={q ? "没有匹配的评论" : "没有评论"}>
          <table>
            <thead>
              <tr>
                <th>用户</th>
                <th>内容</th>
                <th>状态</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {items.map((item) => (
                <tr key={item.id}>
                  <td>{item.username}</td>
                  <td>{item.body}</td>
                  <td>
                    <Status value={item.state} />
                  </td>
                  <td>
                    <div className="row-actions">
                      {item.state !== "visible" && (
                        <button className="btn btn-primary" type="button" onClick={() => approve(item)}>
                          通过
                        </button>
                      )}
                      {item.state !== "hidden" && (
                        <button className="btn btn-danger" type="button" onClick={() => setHiding(item)}>
                          隐藏
                        </button>
                      )}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </TableState>
      </div>
      <Pagination page={page} total={total} perPage={25} onChange={setPage} />
      <Dialog
        open={!!hiding}
        title="隐藏评论"
        hint="创作者和用户会看到这条理由。"
        onClose={() => setHiding(null)}
        footer={
          <>
            <button className="btn" type="button" onClick={() => setHiding(null)}>
              取消
            </button>
            <button
              className="btn btn-danger"
              type="button"
              disabled={!note.trim()}
              onClick={async () => {
                if (!hiding) return
                try {
                  await decideComment(hiding.id, "hide", note)
                  toast("已隐藏")
                  setHiding(null)
                  setNote("")
                  await load()
                } catch (err) {
                  setError((err as Error).message)
                }
              }}
            >
              隐藏
            </button>
          </>
        }
      >
        <p className="summary">{hiding?.body}</p>
        <Field label="理由">
          <textarea value={note} onChange={(event) => setNote(event.target.value)} />
        </Field>
      </Dialog>
    </>
  )
}
