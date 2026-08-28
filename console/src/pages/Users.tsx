import { useEffect, useRef, useState, type ReactNode } from "react"
import { useNavigate, useParams } from "react-router"
import { api, setUserState } from "../api"
import { Dialog, Empty, Field, PageHeader, Pagination, SearchForm, Status, TableState, formatRelative, toast } from "../ui"

type User = {
  id: string
  username: string
  avatar_url?: string
  bandbbs_user_id?: number
  role: string
  resource_count: number
  ticket_count?: number
  banned?: boolean
  banned_at?: string
  frozen?: boolean
  creator_frozen_at?: string
  ban_reason?: string
  created_at?: string
  updated_at?: string
  last_seen_at?: string
}

type Page<T> = {
  items: T[]
  total: number
  page: number
  per_page: number
  total_pages: number
}

type UserResource = {
  id: string
  slug: string
  name: string
  kind: string
  platform: string
  moderation_state: string
  revision_state: string
  download_count: number
  revision_no: number
  created_at: string
  updated_at: string
}

type UserComment = {
  id: string
  resource_id: string
  resource_name: string
  body: string
  moderation_state: string
  parent_id?: string
  created_at: string
  edited_at?: string
  deleted_at?: string
}

type UserTicket = {
  id: string
  kind: string
  subject: string
  message: string
  target_source: string
  target_id: string
  target_url: string
  status: string
  resolution: string
  created_at: string
  updated_at: string
  closed_at?: string
}

type UserMessage = {
  id: string
  kind: string
  title: string
  body: string
  ref: string
  read_at?: string
  created_at: string
  expires_at: string
}

type UserLedgerEntry = {
  id: string
  kind: string
  reference_type: string
  reference_id: string
  note: string
  actor_user_id: string
  delta_units: number
  created_at: string
}

type UserSession = {
  id: string
  app_id: string
  app_version: string
  platform: string
  ip: string
  user_agent: string
  access_expires_at: string
  refresh_expires_at: string
  created_at: string
  last_seen_at: string
}

type UserAuditEntry = {
  id: number
  action: string
  result: string
  ip: string
  user_agent: string
  metadata: string
  created_at: string
}

type UserDetail = {
  user: User
  resources: Page<UserResource>
  comments: Page<UserComment>
  tickets: Page<UserTicket>
  messages: Page<UserMessage>
  coin: { balance_units: number; balance: number; voting_frozen_at?: string; voting_frozen_reason?: string }
  ledger: Page<UserLedgerEntry>
  sessions: Page<UserSession>
  audit: Page<UserAuditEntry>
}

type UserAction = { id: string; action: string; label: string; needsReason: boolean }
type UserTab = "overview" | "resources" | "comments" | "tickets" | "messages" | "ledger" | "sessions" | "audit"

const roleLabels: Record<string, string> = { admin: "管理员", reviewer: "审核员", user: "用户" }
const kindLabels: Record<string, string> = { watchface: "表盘", app: "应用", quickapp: "快应用" }

function roleLabel(role: string) {
  return roleLabels[role] || role || "未知"
}

function kindLabel(kind: string) {
  return kindLabels[kind] || kind || "—"
}

function dateTime(value?: string) {
  if (!value) return "—"
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString("zh-CN", { hour12: false })
}

function number(value?: number) {
  return typeof value === "number" ? value.toLocaleString("zh-CN") : "—"
}

function userInitial(username?: string) {
  return (username || "?").slice(0, 1).toUpperCase()
}

function isUserBanned(user: User) {
  return Boolean(user.banned || user.banned_at)
}

function isCreatorFrozen(user: User) {
  return Boolean(user.frozen || user.creator_frozen_at)
}

async function copyValue(value: string, label: string) {
  try {
    await navigator.clipboard.writeText(value)
    toast(`${label}已复制`)
  } catch {
    toast("复制失败", "err")
  }
}

function UserState({ user }: { user: User }) {
  if (isUserBanned(user)) return <Status value="banned" label="已封禁" />
  if (isCreatorFrozen(user)) return <Status value="frozen" label="创作已冻结" />
  return <Status value="visible" label="正常" />
}

function Fact({ label, children, mono = false }: { label: string; children: ReactNode; mono?: boolean }) {
  return (
    <div className="user-fact">
      <dt>{label}</dt>
      <dd className={mono ? "mono" : ""}>{children}</dd>
    </div>
  )
}

function SectionHeader({ title, count, note }: { title: string; count?: number; note?: string }) {
  return (
    <div className="section-head">
      <div>
        <h3>{title}</h3>
        {note ? <p className="hint">{note}</p> : null}
      </div>
      {typeof count === "number" ? <span className="section-count">{number(count)}</span> : null}
    </div>
  )
}

export function UsersPage() {
  const { id } = useParams()
  const navigate = useNavigate()
  const [items, setItems] = useState<User[]>([])
  const [total, setTotal] = useState(0)
  const [q, setQ] = useState("")
  const [page, setPage] = useState(1)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState("")
  const [detail, setDetail] = useState<UserDetail | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)
  const [detailError, setDetailError] = useState("")
  const [reason, setReason] = useState("")
  const [action, setAction] = useState<UserAction | null>(null)
  const [sessionToRevoke, setSessionToRevoke] = useState<UserSession | null>(null)
  const [revokingAll, setRevokingAll] = useState(false)
  const [tab, setTab] = useState<UserTab>("overview")
  const listRequest = useRef(0)
  const detailRequest = useRef(0)

  const load = async (search = q, next = page) => {
    const requestID = ++listRequest.current
    setLoading(true)
    setError("")
    try {
      const data = await api.list<User>("/admin/api/users", { q: search, page: next, per_page: 25 })
      if (requestID !== listRequest.current) return
      setItems(data.items || [])
      setTotal(data.total || 0)
    } catch (err) {
      if (requestID === listRequest.current) setError((err as Error).message)
    } finally {
      if (requestID === listRequest.current) setLoading(false)
    }
  }

  const loadDetail = async (userID = id) => {
    const requestID = ++detailRequest.current
    if (!userID) {
      setDetail(null)
      setDetailError("")
      setDetailLoading(false)
      return
    }
    setDetailLoading(true)
    setDetailError("")
    try {
      const value = await api.get<UserDetail>(`/admin/api/users/${encodeURIComponent(userID)}`)
      if (requestID === detailRequest.current) setDetail(value)
    } catch (err) {
      if (requestID === detailRequest.current) {
        setDetail(null)
        setDetailError((err as Error).message)
      }
    } finally {
      if (requestID === detailRequest.current) setDetailLoading(false)
    }
  }

  useEffect(() => {
    void load(q, page)
  }, [page])

  useEffect(() => {
    setTab("overview")
    setDetail(null)
    setDetailError("")
    void loadDetail()
  }, [id])

  useEffect(() => {
    if (!detail?.user || items.some((item) => item.id === detail.user.id)) return
    setItems((current) => [detail.user, ...current])
  }, [detail, items])

  const user = detail?.user || items.find((item) => item.id === id)
  const hasSelectedUser = Boolean(id || user)

  const refresh = async () => {
    await Promise.all([load(q, page), loadDetail()])
  }

  const run = async () => {
    if (!action) return
    try {
      const role = action.action === "set_role" ? (user?.role === "reviewer" ? "user" : "reviewer") : ""
      await setUserState(action.id, action.action, reason, role)
      toast("用户状态已更新")
      setAction(null)
      setReason("")
      await refresh()
    } catch (err) {
      setError((err as Error).message)
    }
  }

  const revokeSession = async () => {
    if (!sessionToRevoke || !id) return
    try {
      await api.post(`/admin/api/users/${encodeURIComponent(id)}/sessions`, { session_id: sessionToRevoke.id })
      toast("会话已撤销")
      setSessionToRevoke(null)
      await loadDetail()
    } catch (err) {
      toast((err as Error).message, "err")
    }
  }

  const revokeAllSessions = async () => {
    if (!id) return
    setRevokingAll(true)
    try {
      await api.post(`/admin/api/users/${encodeURIComponent(id)}/sessions`, { all: true })
      toast("全部会话已撤销")
      await loadDetail()
    } catch (err) {
      toast((err as Error).message, "err")
    } finally {
      setRevokingAll(false)
    }
  }

  const tabs: { id: UserTab; label: string; count?: number }[] = [
    { id: "overview", label: "概览" },
    { id: "resources", label: "资源", count: detail?.resources.total },
    { id: "comments", label: "评论", count: detail?.comments.total },
    { id: "tickets", label: "工单", count: detail?.tickets.total },
    { id: "messages", label: "消息", count: detail?.messages.total },
    { id: "ledger", label: "硬币台账", count: detail?.ledger.total },
    { id: "sessions", label: "会话", count: detail?.sessions.total },
    { id: "audit", label: "审计", count: detail?.audit.total },
  ]

  return (
    <>
      <PageHeader title="用户" hint="查看账号、内容、会话和治理记录">
        <SearchForm
          value={q}
          onChange={setQ}
          onSubmit={() => {
            setPage(1)
            void load(q, 1)
          }}
          placeholder="搜索用户名、UUID 或 BandBBS ID"
        />
      </PageHeader>
      <div className={`users-layout ${hasSelectedUser ? "has-detail" : ""}`}>
        <section className="users-list panel-surface">
          <div className="list-heading">
            <div>
              <h2>用户列表</h2>
              <p className="hint">共 {number(total)} 个账号，点击一行查看详情</p>
            </div>
            <span className="list-page">第 {page} 页</span>
          </div>
          <div className="table-wrap users-table-wrap">
            <TableState loading={loading} error={error} onRetry={() => void load()} isEmpty={!items.length} empty={q ? "没有匹配的用户" : "没有用户"}>
              <table className="users-table">
                <thead>
                  <tr>
                    <th>用户</th>
                    <th>UUID</th>
                    <th>BandBBS</th>
                    <th>角色</th>
                    <th>资源</th>
                    <th>工单</th>
                    <th>状态</th>
                    <th>注册时间</th>
                    <th>最近活动</th>
                  </tr>
                </thead>
                <tbody>
                  {items.map((item) => (
                    <tr key={item.id} className={`clickable ${item.id === id ? "selected" : ""}`} onClick={() => navigate(`/users/${encodeURIComponent(item.id)}`)}>
                      <td>
                        <div className="user-identity">
                          {item.avatar_url ? <img src={item.avatar_url} alt="" /> : <span className="avatar-fallback">{userInitial(item.username)}</span>}
                          <span>
                            <strong>{item.username || "未命名用户"}</strong>
                            <small>{item.id.slice(0, 8)}…</small>
                          </span>
                        </div>
                      </td>
                      <td className="mono truncate-cell" title={item.id}>{item.id}</td>
                      <td>{item.bandbbs_user_id || "—"}</td>
                      <td>{roleLabel(item.role)}</td>
                      <td>{number(item.resource_count)}</td>
                      <td>{number(item.ticket_count)}</td>
                      <td><UserState user={item} /></td>
                      <td>{formatRelative(item.created_at)}</td>
                      <td>{formatRelative(item.last_seen_at)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </TableState>
          </div>
          <Pagination page={page} total={total} perPage={25} onChange={setPage} />
        </section>

        {hasSelectedUser ? (
          <aside className="users-detail">
            {detailLoading && !detail ? <div className="panel-surface detail-placeholder">加载用户详情</div> : null}
            {detailError ? <div className="panel-surface detail-placeholder error">{detailError}</div> : null}
            {detail && user && (
              <div className="user-detail-card panel-surface">
                <div className="user-detail-head">
                  <div className="user-detail-identity">
                    {user.avatar_url ? <img src={user.avatar_url} alt="" /> : <span className="avatar-fallback large">{userInitial(user.username)}</span>}
                    <div>
                      <h2>{user.username || "未命名用户"}</h2>
                      <p>{roleLabel(user.role)} · 注册于 {dateTime(user.created_at)}</p>
                    </div>
                  </div>
                  <UserState user={user} />
                </div>
                <div className="identity-row">
                  <span><strong>UUID</strong> <span className="mono">{user.id}</span></span>
                  <button className="icon-btn" type="button" title="复制 UUID" aria-label="复制 UUID" onClick={() => void copyValue(user.id, "UUID")}>复制</button>
                </div>
                <dl className="user-facts">
                  <Fact label="BandBBS ID" mono>{user.bandbbs_user_id || "—"}</Fact>
                  <Fact label="角色">{roleLabel(user.role)}</Fact>
                  <Fact label="资源">{number(user.resource_count)}</Fact>
                  <Fact label="工单">{number(user.ticket_count)}</Fact>
                  <Fact label="注册时间">{dateTime(user.created_at)}</Fact>
                  <Fact label="最近活动">{dateTime(user.last_seen_at)}</Fact>
                  <Fact label="更新时间">{dateTime(user.updated_at)}</Fact>
                  <Fact label="封禁时间">{dateTime(user.banned_at)}</Fact>
                  <Fact label="创作冻结">{dateTime(user.creator_frozen_at)}</Fact>
                </dl>
                {user.ban_reason ? <div className="notice danger-notice"><strong>治理理由</strong><span>{user.ban_reason}</span></div> : null}
                <div className="user-actions">
                  <button className={isUserBanned(user) ? "btn" : "btn btn-danger"} type="button" onClick={() => setAction({ id: user.id, action: isUserBanned(user) ? "unban" : "ban", label: isUserBanned(user) ? "解封用户" : "封禁用户", needsReason: !isUserBanned(user) })}>
                    {isUserBanned(user) ? "解封用户" : "封禁用户"}
                  </button>
                  <button className="btn" type="button" onClick={() => setAction({ id: user.id, action: isCreatorFrozen(user) ? "unfreeze_creator" : "freeze_creator", label: isCreatorFrozen(user) ? "解除创作冻结" : "冻结创作", needsReason: !isCreatorFrozen(user) })}>
                    {isCreatorFrozen(user) ? "解除创作冻结" : "冻结创作"}
                  </button>
                  <button className="btn" type="button" onClick={() => setAction({ id: user.id, action: "set_role", label: user.role === "reviewer" ? "取消审核员" : "设为审核员", needsReason: true })}>
                    {user.role === "reviewer" ? "取消审核员" : "设为审核员"}
                  </button>
                  <button className="btn" type="button" disabled={revokingAll || !detail.sessions.total} onClick={() => void revokeAllSessions()}>
                    {revokingAll ? "撤销中" : "撤销全部会话"}
                  </button>
                </div>

                <nav className="detail-tabs" aria-label="用户详情分组">
                  {tabs.map((item) => (
                    <button key={item.id} className={tab === item.id ? "active" : ""} type="button" onClick={() => setTab(item.id)}>
                      {item.label}
                      {typeof item.count === "number" ? <span>{item.count}</span> : null}
                    </button>
                  ))}
                </nav>

                {tab === "overview" && (
                  <div className="detail-section-grid">
                    <section className="detail-section">
                      <SectionHeader title="硬币账户" />
                      <div className="metric-grid compact">
                        <div className="metric"><span>余额</span><strong>{number(detail.coin?.balance)} 枚</strong></div>
                        <div className="metric"><span>最小单位</span><strong>{number(detail.coin?.balance_units)}</strong></div>
                      </div>
                      {detail.coin?.voting_frozen_at ? <p className="hint">投票已冻结：{detail.coin.voting_frozen_reason || "未填写理由"}</p> : <p className="hint">投票状态正常</p>}
                    </section>
                    <section className="detail-section">
                      <SectionHeader title="近期活动" />
                      <dl className="kv compact-kv">
                        <div><dt>最近会话</dt><dd>{detail.sessions.items[0] ? dateTime(detail.sessions.items[0].last_seen_at) : "—"}</dd></div>
                        <div><dt>最近工单</dt><dd>{detail.tickets.items[0] ? dateTime(detail.tickets.items[0].updated_at) : "—"}</dd></div>
                        <div><dt>最近审计</dt><dd>{detail.audit.items[0] ? dateTime(detail.audit.items[0].created_at) : "—"}</dd></div>
                      </dl>
                    </section>
                  </div>
                )}

                {tab === "resources" && (
                  <section className="detail-section">
                    <SectionHeader title="资源" count={detail.resources.total} note="显示最近更新的资源" />
                    <div className="mini-table-wrap"><table className="mini-table"><thead><tr><th>名称</th><th>类型</th><th>状态</th><th>下载</th><th>更新</th></tr></thead><tbody>
                      {detail.resources.items.map((item) => <tr key={item.id} className="clickable" onClick={() => navigate(`/resources/${encodeURIComponent(item.id)}`)}><td><strong>{item.name || item.slug}</strong><small>{item.slug}</small></td><td>{kindLabel(item.kind)}</td><td><Status value={item.moderation_state} /></td><td>{number(item.download_count)}</td><td>{formatRelative(item.updated_at)}</td></tr>)}
                    </tbody></table></div>
                    {!detail.resources.items.length && <Empty>没有资源</Empty>}
                  </section>
                )}

                {tab === "comments" && (
                  <section className="detail-section">
                    <SectionHeader title="评论" count={detail.comments.total} />
                    <div className="mini-table-wrap"><table className="mini-table"><thead><tr><th>内容</th><th>资源</th><th>状态</th><th>时间</th></tr></thead><tbody>
                      {detail.comments.items.map((item) => <tr key={item.id}><td className="message-cell">{item.body}</td><td>{item.resource_name || item.resource_id}</td><td><Status value={item.moderation_state} /></td><td>{formatRelative(item.created_at)}</td></tr>)}
                    </tbody></table></div>
                    {!detail.comments.items.length && <Empty>没有评论</Empty>}
                  </section>
                )}

                {tab === "tickets" && (
                  <section className="detail-section">
                    <SectionHeader title="工单" count={detail.tickets.total} />
                    <div className="mini-table-wrap"><table className="mini-table"><thead><tr><th>主题</th><th>类型</th><th>状态</th><th>更新时间</th></tr></thead><tbody>
                      {detail.tickets.items.map((item) => <tr key={item.id}><td><strong>{item.subject || "无主题"}</strong><small>{item.message}</small></td><td>{item.kind || "—"}</td><td><Status value={item.status} /></td><td>{formatRelative(item.updated_at)}</td></tr>)}
                    </tbody></table></div>
                    {!detail.tickets.items.length && <Empty>没有工单</Empty>}
                  </section>
                )}

                {tab === "messages" && (
                  <section className="detail-section">
                    <SectionHeader title="消息" count={detail.messages.total} />
                    <div className="mini-table-wrap"><table className="mini-table"><thead><tr><th>消息</th><th>类型</th><th>状态</th><th>时间</th></tr></thead><tbody>
                      {detail.messages.items.map((item) => <tr key={item.id}><td><strong>{item.title || item.kind}</strong><small>{item.body}</small></td><td>{item.kind}</td><td>{item.read_at ? "已读" : "未读"}</td><td>{formatRelative(item.created_at)}</td></tr>)}
                    </tbody></table></div>
                    {!detail.messages.items.length && <Empty>没有消息</Empty>}
                  </section>
                )}

                {tab === "ledger" && (
                  <section className="detail-section">
                    <SectionHeader title="硬币台账" count={detail.ledger.total} />
                    <div className="mini-table-wrap"><table className="mini-table"><thead><tr><th>变动</th><th>类型</th><th>关联对象</th><th>备注</th><th>时间</th></tr></thead><tbody>
                      {detail.ledger.items.map((item) => <tr key={item.id}><td className={item.delta_units >= 0 ? "amount-positive" : "amount-negative"}>{item.delta_units >= 0 ? "+" : ""}{item.delta_units}</td><td>{item.kind}</td><td className="mono">{item.reference_id || "—"}</td><td>{item.note || "—"}</td><td>{formatRelative(item.created_at)}</td></tr>)}
                    </tbody></table></div>
                    {!detail.ledger.items.length && <Empty>没有台账记录</Empty>}
                  </section>
                )}

                {tab === "sessions" && (
                  <section className="detail-section">
                    <SectionHeader title="活动会话" count={detail.sessions.total} note="仅显示仍有效的会话" />
                    <div className="mini-table-wrap"><table className="mini-table"><thead><tr><th>客户端</th><th>平台</th><th>网络</th><th>最近活动</th><th></th></tr></thead><tbody>
                      {detail.sessions.items.map((item) => <tr key={item.id}><td><strong>{item.app_id || "未知客户端"}</strong><small>{item.app_version || "—"}</small></td><td>{item.platform || "—"}</td><td><span className="mono">{item.ip || "—"}</span><small>{item.user_agent}</small></td><td>{formatRelative(item.last_seen_at)}</td><td><button className="btn btn-danger small-btn" type="button" onClick={() => setSessionToRevoke(item)}>撤销</button></td></tr>)}
                    </tbody></table></div>
                    {!detail.sessions.items.length && <Empty>没有有效会话</Empty>}
                  </section>
                )}

                {tab === "audit" && (
                  <section className="detail-section">
                    <SectionHeader title="审计记录" count={detail.audit.total} />
                    <div className="mini-table-wrap"><table className="mini-table"><thead><tr><th>动作</th><th>结果</th><th>操作者网络</th><th>时间</th><th>详情</th></tr></thead><tbody>
                      {detail.audit.items.map((item) => <tr key={item.id}><td>{item.action}</td><td><Status value={item.result} /></td><td><span className="mono">{item.ip || "—"}</span><small>{item.user_agent}</small></td><td>{formatRelative(item.created_at)}</td><td><details><summary>查看</summary><pre className="summary">{item.metadata || "—"}</pre></details></td></tr>)}
                    </tbody></table></div>
                    {!detail.audit.items.length && <Empty>没有审计记录</Empty>}
                  </section>
                )}
              </div>
            )}
          </aside>
        ) : null}
      </div>

      <Dialog
        open={!!action}
        title={action?.label || "确认操作"}
        hint={action?.needsReason ? "这项操作会立即生效，请填写原因" : "确认执行这项操作"}
        onClose={() => setAction(null)}
        footer={<><button className="btn" type="button" onClick={() => setAction(null)}>取消</button><button className="btn btn-danger" type="button" disabled={!!action?.needsReason && !reason.trim()} onClick={() => void run()}>确认</button></>}
      >
        {action?.needsReason ? <Field label="原因"><textarea value={reason} onChange={(event) => setReason(event.target.value)} placeholder="填写本次操作的原因" /></Field> : <p className="summary">确认执行这项操作</p>}
      </Dialog>

      <Dialog
        open={!!sessionToRevoke}
        title="撤销会话"
        hint="撤销后该客户端需要重新登录"
        onClose={() => setSessionToRevoke(null)}
        footer={<><button className="btn" type="button" onClick={() => setSessionToRevoke(null)}>取消</button><button className="btn btn-danger" type="button" onClick={() => void revokeSession()}>确认撤销</button></>}
      >
        <dl className="kv">
          <div><dt>客户端</dt><dd>{sessionToRevoke?.app_id || "—"}</dd></div>
          <div><dt>平台</dt><dd>{sessionToRevoke?.platform || "—"}</dd></div>
          <div><dt>最近活动</dt><dd>{dateTime(sessionToRevoke?.last_seen_at)}</dd></div>
        </dl>
      </Dialog>
    </>
  )
}
