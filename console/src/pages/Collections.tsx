import { FormEvent, useEffect, useState } from "react"
import { useNavigate, useParams } from "react-router"
import { api } from "../api"
import { Field, PageHeader, Pagination, SearchForm, Status, TableState, formatRelative, toast } from "../ui"
import { type Row, useRows } from "./system-helpers"

export function CollectionsPage() {
  const { id } = useParams()
  const navigate = useNavigate()
  const { items, total, q, setQ, page, setPage, error, loading, load } = useRows("/admin/api/collections")
  const [detail, setDetail] = useState<Row | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)
  const [detailError, setDetailError] = useState("")
  const [name, setName] = useState("")
  const [summary, setSummary] = useState("")
  useEffect(() => {
    if (!id) {
      setDetail(null)
      setDetailError("")
      setDetailLoading(false)
      return
    }
    let active = true
    setDetail(null)
    setDetailError("")
    setDetailLoading(true)
    api.get<Row>(`/admin/api/collections/${encodeURIComponent(id)}`).then((value) => {
      if (!active) return
      setDetail(value)
      setName(value.collection?.latest_revision_name || value.collection?.name || "")
      setSummary(value.collection?.latest_revision_summary || "")
    }).catch((err: Error) => {
      if (active) setDetailError(err.message)
    }).finally(() => {
      if (active) setDetailLoading(false)
    })
    return () => {
      active = false
    }
  }, [id])
  return (
    <>
      <PageHeader title="合集" hint="点进合集后可以改名称和成员，提交后进入合集审核。">
        <SearchForm value={q} onChange={setQ} onSubmit={() => { setPage(1); load(q, 1) }} placeholder="搜索合集" />
      </PageHeader>
      <div className="table-wrap">
        <TableState loading={loading} error={error} onRetry={() => void load()} isEmpty={!items.length} empty={q ? "没有匹配的合集" : "没有合集"}>
          <table>
            <thead>
              <tr>
                <th>名称</th>
                <th>作者</th>
                <th>类型</th>
                <th>成员</th>
                <th>状态</th>
                <th>更新</th>
              </tr>
            </thead>
            <tbody>
              {items.map((item) => (
                <tr key={item.id} className="clickable" onClick={() => navigate(`/collections/${item.id}`)}>
                  <td><strong>{item.name || item.slug}</strong><small>{item.slug}</small></td>
                  <td>{item.owner || item.owner_id || "—"}</td>
                  <td>{item.kind || item.platform || "—"}</td>
                  <td>{item.member_count ?? "—"}</td>
                  <td><Status value={item.state} /></td>
                  <td>{formatRelative(item.updated_at)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </TableState>
      </div>
      <Pagination page={page} total={total} perPage={25} onChange={setPage} />
      {id && detailLoading && !detail ? <div className="panel-surface detail-placeholder">加载合集详情</div> : null}
      {id && detailError && !detail ? <div className="panel-surface detail-placeholder error">{detailError}</div> : null}
      {detail && (
        <div className="page-body">
          <form
            className="stack"
            onSubmit={(event: FormEvent) => {
              event.preventDefault()
              api
                .post(`/admin/api/collections/${encodeURIComponent(String(id))}`, { name, summary, resource_ids: (detail.members || []).map((member: Row) => member.id) })
                .then(() => toast("已保存并进入审核"))
                .catch((err: Error) => toast(err.message, "err"))
            }}
          >
            <Field label="名称">
              <input value={name} onChange={(event) => setName(event.target.value)} />
            </Field>
            <Field label="简介">
              <textarea value={summary} onChange={(event) => setSummary(event.target.value)} />
            </Field>
            <button className="btn btn-primary" type="submit">
              保存并提交审核
            </button>
          </form>
        </div>
      )}
    </>
  )
}