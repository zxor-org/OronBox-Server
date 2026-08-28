import { useEffect, useState } from "react"
import { api } from "../api"
import { Dialog, Empty, Field, PageHeader, TableState, toast } from "../ui"

type Member = { id: string; name: string; slug: string; owner: string; representative?: boolean }
type Pending = {
  id: string
  slug: string
  pending_revision?: { id: string; name: string; summary: string }
  members?: Member[]
  representative_name?: string
}

export function CollectionReviewPage() {
  const [items, setItems] = useState<Pending[]>([])
  const [selected, setSelected] = useState("")
  const [error, setError] = useState("")
  const [loading, setLoading] = useState(true)
  const [detailLoading, setDetailLoading] = useState(false)
  const [detailError, setDetailError] = useState("")
  const [busy, setBusy] = useState(false)
  const [note, setNote] = useState("")
  const [rejecting, setRejecting] = useState(false)
  const [detail, setDetail] = useState<Record<string, any> | null>(null)

  const load = async () => {
    setLoading(true)
    setError("")
    try {
      const data = await api.get<{ collections?: Pending[]; items?: Pending[] }>("/admin/api/collections/review")
      const next = data.collections || data.items || []
      setItems(next)
      setSelected((current) => {
        if (current && next.some((item) => item.id === current)) return current
        return next[0]?.id || ""
      })
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void load()
  }, [])

  useEffect(() => {
    if (!selected) {
      setDetail(null)
      setDetailError("")
      setDetailLoading(false)
      return
    }
    setDetail(null)
    setDetailError("")
    setDetailLoading(true)
    let active = true
    api
      .get<Record<string, any>>(`/admin/api/collections/${selected}`)
      .then((value) => {
        if (active) setDetail(value)
      })
      .catch((err: Error) => {
        if (active) setDetailError(err.message)
      })
      .finally(() => {
        if (active) setDetailLoading(false)
      })
    return () => {
      active = false
    }
  }, [selected])

  const current = items.find((item) => item.id === selected)
  const revisions = detail?.revisions || detail?.Revisions || []
  const pendingRevision = revisions.find((revision: { state?: string; State?: string }) => {
    const state = revision.state || revision.State
    return state === "submitted" || state === "pending"
  }) || revisions[0]
  const members = pendingRevision?.members || pendingRevision?.Members || detail?.members || detail?.Members || current?.members || []

  const decide = async (revisionId: string, approve: boolean) => {
    if (!approve && !note.trim()) {
      setError("退回必须填写理由")
      return
    }
    setBusy(true)
    setError("")
    try {
      await api.post(`/admin/api/collections/review/${revisionId}`, { approve, note })
      toast(approve ? "已通过" : "已退回")
      setNote("")
      setRejecting(false)
      await load()
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const revisionId = pendingRevision?.id || current?.pending_revision?.id

  return (
    <>
      <PageHeader title="合集审核" hint="合集通过后成员和代表作才会生效" />
      <div className="split">
        <div className="queue">
          <TableState loading={loading} error={error} onRetry={() => void load()} isEmpty={!items.length} empty="没有待审合集">
            {items.map((item) => (
              <button
                key={item.id}
                type="button"
                className={`queue-item ${item.id === selected ? "selected" : ""}`}
                onClick={() => setSelected(item.id)}
              >
                <div className="title">{item.pending_revision?.name || item.slug}</div>
                <div className="meta">{item.pending_revision ? "有待审版本" : item.slug}</div>
              </button>
            ))}
          </TableState>
        </div>
        <div className="detail">
          {!current && <div className="detail-empty">从左边选一条</div>}
          {current && detailLoading && !detail && <Empty>加载中</Empty>}
          {current && detailError && !detail && <div className="error detail-placeholder">{detailError}</div>}
          {current && detail && (
            <>
              <h2 className="detail-title">{pendingRevision?.name || current.slug}</h2>
              {pendingRevision?.summary && <p className="summary">{pendingRevision.summary}</p>}
              <div className="files">
                {members.map((member: { ID?: string; id?: string; CurrentRevisionName?: string; current_revision_name?: string; name?: string; Owner?: string; owner?: string; Slug?: string; slug?: string }) => (
                  <div className="file" key={member.id || member.ID}>
                    <div>
                      <div>{member.current_revision_name || member.CurrentRevisionName || member.name || member.slug || member.Slug}</div>
                      <small>
                        {member.owner || member.Owner} · {member.slug || member.Slug}
                      </small>
                    </div>
                  </div>
                ))}
                {members.length === 0 && <Empty>没有成员</Empty>}
              </div>
              {error && <div className="error">{error}</div>}
              <div className="actions">
                {revisionId && (
                  <>
                    <button className="btn btn-primary" type="button" disabled={busy} onClick={() => void decide(revisionId, true)}>
                      通过
                    </button>
                    <button className="btn btn-danger" type="button" disabled={busy} onClick={() => setRejecting(true)}>
                      退回
                    </button>
                  </>
                )}
              </div>
            </>
          )}
        </div>
      </div>
      <Dialog
        open={rejecting}
        title="退回这一版"
        hint="作者会看到这段话"
        onClose={() => setRejecting(false)}
        footer={
          <>
            <button className="btn" type="button" onClick={() => setRejecting(false)}>
              取消
            </button>
            <button className="btn btn-danger" disabled={busy || !note.trim() || !revisionId} onClick={() => revisionId && void decide(revisionId, false)}>
              退回
            </button>
          </>
        }
      >
        <Field label="理由">
          <textarea value={note} onChange={(event) => setNote(event.target.value)} placeholder="需要改什么" />
        </Field>
      </Dialog>
    </>
  )
}
