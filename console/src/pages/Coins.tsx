import { useEffect, useState } from "react"
import { api } from "../api"
import { Dialog, Field, PageHeader, Pagination, SearchForm, TableState, formatRelative, toast } from "../ui"
import { type Row, useRows } from "./system-helpers"

export function CoinsPage() {
  const { items, total, q, setQ, page, setPage, error, loading, load } = useRows("/admin/api/coins/ledger")
  const [open, setOpen] = useState(false)
  const [form, setForm] = useState({ user: "", delta_units: 0, reason: "", action: "adjust" })
  const [stats, setStats] = useState<Row | null>(null)
  const [invalidating, setInvalidating] = useState(false)
  const [invalidateForm, setInvalidateForm] = useState({ resource_id: "", user_id: "", reason: "" })
  useEffect(() => {
    api
      .get<Row>("/admin/api/coins")
      .then((data) => setStats(data.stats || null))
      .catch(() => setStats(null))
  }, [])
  const statCards = [
    { label: "已发放", value: stats?.issued_units },
    { label: "已消费", value: stats?.spent_units },
    { label: "创作者奖励", value: stats?.rewarded_units },
    { label: "活跃投票用户", value: stats?.active_voters },
    { label: "冻结投票", value: stats?.frozen_voters },
  ]
  return (
    <>
      <PageHeader title="硬币" hint="发放和冻结走对话框，台账只读。作废投票需填写原因。">
        <SearchForm value={q} onChange={setQ} onSubmit={() => { setPage(1); load(q, 1) }} placeholder="搜索用户" />
        <button className="btn" type="button" onClick={() => setInvalidating(true)}>作废投票</button>
        <button className="btn btn-primary" type="button" onClick={() => setOpen(true)}>调整余额</button>
      </PageHeader>
      {stats ? (
        <div className="stats analytics-totals">
          {statCards.map((card) => (
            <div className="stat" key={card.label}>
              <div className="stat-head"><div className="label">{card.label}</div></div>
              <div className="num">{typeof card.value === "number" ? card.value.toLocaleString("zh-CN") : "—"}</div>
            </div>
          ))}
        </div>
      ) : null}
      <div className="table-wrap">
        <TableState loading={loading} error={error} onRetry={() => void load()} isEmpty={!items.length} empty={q ? "没有匹配的台账记录" : "没有台账记录"}>
          <table>
            <thead>
              <tr>
                <th>用户</th>
                <th>类型</th>
                <th>变动</th>
                <th>关联对象</th>
                <th>原因</th>
                <th>时间</th>
              </tr>
            </thead>
            <tbody>
              {items.map((item, index) => (
                <tr key={item.id || index}>
                  <td><strong>{item.username || "—"}</strong><small className="mono">{item.user_id || "—"}</small></td>
                  <td>{item.kind || "—"}</td>
                  <td className={Number(item.delta_units) >= 0 ? "amount-positive" : "amount-negative"}>{Number(item.delta_units) >= 0 ? "+" : ""}{item.delta_units}</td>
                  <td className="mono">{item.reference_id || "—"}</td>
                  <td>{item.note || "—"}</td>
                  <td>{formatRelative(item.created_at)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </TableState>
      </div>
      <Pagination page={page} total={total} perPage={25} onChange={setPage} />
      <Dialog
        open={open}
        title="调整硬币"
        onClose={() => setOpen(false)}
        footer={
          <button
            className="btn btn-primary"
            onClick={() =>
              api
                .post(`/admin/api/coins/users/${form.user}`, form)
                .then(() => {
                  toast("已调整")
                  setOpen(false)
                  load()
                })
                .catch((err: Error) => toast(err.message, "err"))
            }
          >
            确认
          </button>
        }
      >
        <Field label="用户 ID">
          <input value={form.user} onChange={(event) => setForm({ ...form, user: event.target.value })} />
        </Field>
        <Field label="变动（单位）">
          <input type="number" value={form.delta_units} onChange={(event) => setForm({ ...form, delta_units: Number(event.target.value) })} />
        </Field>
        <Field label="原因">
          <input value={form.reason} onChange={(event) => setForm({ ...form, reason: event.target.value })} />
        </Field>
      </Dialog>
      <Dialog
        open={invalidating}
        title="作废硬币投票"
        hint="撤销一次资源投票并退回硬币，必须填写原因。"
        onClose={() => setInvalidating(false)}
        footer={
          <button
            className="btn btn-danger"
            disabled={!invalidateForm.resource_id.trim() || !invalidateForm.reason.trim()}
            onClick={() =>
              api
                .post("/admin/api/coins/invalidate", invalidateForm)
                .then(() => {
                  toast("已作废")
                  setInvalidating(false)
                  setInvalidateForm({ resource_id: "", user_id: "", reason: "" })
                  load()
                })
                .catch((err: Error) => toast(err.message, "err"))
            }
          >
            作废
          </button>
        }
      >
        <Field label="资源 ID">
          <input value={invalidateForm.resource_id} onChange={(event) => setInvalidateForm({ ...invalidateForm, resource_id: event.target.value })} />
        </Field>
        <Field label="用户 ID">
          <input value={invalidateForm.user_id} onChange={(event) => setInvalidateForm({ ...invalidateForm, user_id: event.target.value })} />
        </Field>
        <Field label="原因">
          <textarea value={invalidateForm.reason} onChange={(event) => setInvalidateForm({ ...invalidateForm, reason: event.target.value })} />
        </Field>
      </Dialog>
    </>
  )
}