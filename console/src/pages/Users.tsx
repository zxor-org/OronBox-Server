import { useEffect, useState } from "react"
import { useNavigate, useParams } from "react-router"
import { api, setUserState } from "../api"
import { Dialog, Empty, Field, PageHeader, Pagination, SearchForm, Status, toast } from "../ui"

type User = {
  id: string
  username: string
  role: string
  resource_count: number
  banned?: boolean
  frozen?: boolean
  ban_reason?: string
}

export function UsersPage() {
  const { id } = useParams()
  const navigate = useNavigate()
  const [items, setItems] = useState<User[]>([])
  const [total, setTotal] = useState(0)
  const [q, setQ] = useState("")
  const [page, setPage] = useState(1)
  const [error, setError] = useState("")
  const [reason, setReason] = useState("")
  const [action, setAction] = useState<{ id: string; action: string; label: string } | null>(null)

  const load = (search = q, next = page) =>
    api
      .list<User>("/admin/api/users", { q: search, page: next, per_page: 25 })
      .then((data) => {
        setItems(data.items || [])
        setTotal(data.total || 0)
      })
      .catch((err: Error) => setError(err.message))

  useEffect(() => {
    load(q, page)
  }, [page])

  useEffect(() => {
    if (!id) return
    const found = items.find((item) => item.id === id)
    if (found) return
    api
      .get<{ user?: User }>(`/admin/api/users/${id}`)
      .then((data) => {
        const user = data.user || (data as User)
        if (user?.id) setItems((current) => (current.some((item) => item.id === user.id) ? current : [user, ...current]))
      })
      .catch((err: Error) => setError(err.message))
  }, [id])

  const current = items.find((item) => item.id === id)

  const run = async () => {
    if (!action) return
    try {
      const role = action.action === "set_role" ? (current?.role === "reviewer" ? "user" : "reviewer") : ""
      await setUserState(action.id, action.action, reason, role)
      toast("已更新")
      setAction(null)
      setReason("")
      await load()
    } catch (err) {
      setError((err as Error).message)
    }
  }

  return (
    <>
      <PageHeader title="用户" hint="点行打开操作。封禁、冻结、改角色都要填理由。">
        <SearchForm
          value={q}
          onChange={setQ}
          onSubmit={() => {
            setPage(1)
            load(q, 1)
          }}
          placeholder="搜索用户名或 BandBBS ID"
        />
      </PageHeader>
      {error && <div className="error">{error}</div>}
      <div className="table-wrap">
        {items.length === 0 && <Empty>没有用户</Empty>}
        {items.length > 0 && (
          <table>
            <thead>
              <tr>
                <th>用户</th>
                <th>角色</th>
                <th>资源</th>
                <th>状态</th>
              </tr>
            </thead>
            <tbody>
              {items.map((item) => (
                <tr key={item.id} className={`clickable ${item.id === id ? "selected" : ""}`} onClick={() => navigate(`/users/${item.id}`)}>
                  <td>{item.username}</td>
                  <td>{item.role}</td>
                  <td>{item.resource_count}</td>
                  <td>
                    <Status value={item.banned ? "banned" : item.frozen ? "frozen" : "visible"} label={item.banned ? "已封禁" : item.frozen ? "创作已冻结" : "正常"} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
      <Pagination page={page} total={total} perPage={25} onChange={setPage} />
      {current && (
        <div className="page-body">
          <div className="panel">
            <h3>{current.username}</h3>
            <p className="hint">
              {current.role} · 资源 {current.resource_count}
              {current.ban_reason ? ` · ${current.ban_reason}` : ""}
            </p>
            <div className="row-actions">
              <button className="btn btn-danger" type="button" onClick={() => setAction({ id: current.id, action: current.banned ? "unban" : "ban", label: current.banned ? "解封" : "封禁" })}>
                {current.banned ? "解封" : "封禁"}
              </button>
              <button className="btn" type="button" onClick={() => setAction({ id: current.id, action: current.frozen ? "unfreeze_creator" : "freeze_creator", label: current.frozen ? "解冻创作" : "冻结创作" })}>
                {current.frozen ? "解冻创作" : "冻结创作"}
              </button>
              <button className="btn" type="button" onClick={() => setAction({ id: current.id, action: "set_role", label: current.role === "reviewer" ? "取消审核员" : "设为审核员" })}>
                {current.role === "reviewer" ? "取消审核员" : "设为审核员"}
              </button>
              <button
                className="btn"
                type="button"
                onClick={() =>
                  api
                    .post(`/admin/api/users/${current.id}/sessions`, { all: true })
                    .then(() => toast("已踢掉全部会话"))
                    .catch((err: Error) => toast(err.message, "err"))
                }
              >
                踢掉登录
              </button>
            </div>
          </div>
        </div>
      )}
      <Dialog
        open={!!action}
        title={action?.label || "确认"}
        hint="这条操作会立刻生效，请写清原因。"
        onClose={() => setAction(null)}
        footer={
          <>
            <button className="btn" type="button" onClick={() => setAction(null)}>
              取消
            </button>
            <button className="btn btn-primary" type="button" disabled={!reason.trim() && action?.action !== "unban" && action?.action !== "unfreeze_creator"} onClick={run}>
              确认
            </button>
          </>
        }
      >
        <Field label="理由">
          <textarea value={reason} onChange={(event) => setReason(event.target.value)} placeholder="给创作者看的说明" />
        </Field>
      </Dialog>
    </>
  )
}
