import { useEffect, useState } from "react"
import { api } from "../api"
import { Dialog, Empty, Field, PageHeader, toast } from "../ui"

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
  const [note, setNote] = useState("")
  const [rejecting, setRejecting] = useState(false)
  const [detail, setDetail] = useState<Record<string, any> | null>(null)

  const load = () =>
    api
      .get<{ collections?: Pending[]; items?: Pending[] }>("/admin/api/collections/review")
      .then((data) => {
        const next = data.collections || data.items || []
        setItems(next)
        setSelected((current) => {
          if (current && next.some((item) => item.id === current)) return current
          return next[0]?.id || ""
        })
      })
      .catch((err: Error) => setError(err.message))

  useEffect(() => {
    load()
  }, [])

  useEffect(() => {
    if (!selected) {
      setDetail(null)
      return
    }
    api.get<Record<string, any>>(`/admin/api/collections/${selected}`).then(setDetail).catch((err: Error) => setError(err.message))
  }, [selected])

  const current = items.find((item) => item.id === selected)
  const pendingRevision = (detail?.Revisions || []).find((revision: { State?: string }) => revision.State === "submitted" || revision.State === "pending") || detail?.Revisions?.[0]
  const members = pendingRevision?.Members || detail?.Members || current?.members || []

  const decide = async (revisionId: string, approve: boolean) => {
    if (!approve && !note.trim()) {
      setError("退回必须填写理由")
      return
    }
    try {
      await api.post(`/admin/api/collections/review/${revisionId}`, { approve, note })
      toast(approve ? "已通过" : "已退回")
      setNote("")
      setRejecting(false)
      await load()
    } catch (err) {
      setError((err as Error).message)
    }
  }

  return (
    <>
      <PageHeader hint="合集通过后成员和代表作才会生效。" />
      <div className="split">
        <div className="queue">
          {items.length === 0 && <Empty>没有待审合集</Empty>}
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
        </div>
        <div className="detail">
          {!current && <div className="detail-empty">从左边选一条</div>}
          {current && (
            <>
              <h2 className="detail-title">{current.pending_revision?.name || current.slug}</h2>
              {current.pending_revision?.summary && <p className="summary">{current.pending_revision.summary}</p>}
              <div className="files">
                {members.map((member: { ID?: string; id?: string; CurrentRevisionName?: string; name?: string; Owner?: string; owner?: string; Slug?: string; slug?: string }) => (
                  <div className="file" key={member.ID || member.id}>
                    <div>
                      <div>{member.CurrentRevisionName || member.name || member.Slug || member.slug}</div>
                      <small>
                        {member.Owner || member.owner} · {member.Slug || member.slug}
                      </small>
                    </div>
                  </div>
                ))}
                {members.length === 0 && <Empty>没有成员</Empty>}
              </div>
              {error && <div className="error">{error}</div>}
              <div className="actions">
                {current.pending_revision?.id && (
                  <>
                    <button className="btn btn-primary" type="button" onClick={() => decide(current.pending_revision!.id, true)}>
                      通过
                    </button>
                    <button className="btn btn-danger" type="button" onClick={() => setRejecting(true)}>
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
            <button className="btn btn-danger" disabled={!note.trim()} onClick={() => current?.pending_revision && decide(current.pending_revision.id, false)}>
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
