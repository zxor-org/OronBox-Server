import { FormEvent, useEffect, useState } from "react"
import { useNavigate, useParams } from "react-router"
import { api, getResource, saveResourceDraft, submitResourceDraft, upload } from "../api"
import { Dialog, Empty, Field, PageHeader, Status, Tabs, formatRelative, paidLabel, stateLabel, toast } from "../ui"

type Resource = Record<string, any>

export function ResourcePage() {
  const { id } = useParams()
  const navigate = useNavigate()
  const [data, setData] = useState<Resource | null>(null)
  const [tab, setTab] = useState("edit")
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)
  const [name, setName] = useState("")
  const [summary, setSummary] = useState("")
  const [paidType, setPaidType] = useState("free")
  const [attributes, setAttributes] = useState<string[]>([])
  const [stateOpen, setStateOpen] = useState("")
  const [reason, setReason] = useState("")
  const [governance, setGovernance] = useState({ author_name: "", source_url: "", license_name: "", authorization_note: "", collection_id: "", collection_position: 0 })

  const load = () => {
    if (!id) return
    getResource(id)
      .then((value) => {
        const resource = value as Resource
        setData(resource)
        setName(resource.name || "")
        setSummary(resource.summary || "")
        setPaidType(resource.paid_type || "free")
        setAttributes(resource.attributes || [])
        setGovernance({
          author_name: resource.governance?.author_name || "",
          source_url: resource.governance?.source_url || "",
          license_name: resource.governance?.license_name || "",
          authorization_note: resource.governance?.authorization_note || "",
          collection_id: resource.governance?.collection_id || "",
          collection_position: resource.governance?.collection_position || 0,
        })
      })
      .catch((err: Error) => setError(err.message))
  }

  useEffect(() => {
    load()
  }, [id])

  if (!id) return <Empty>缺少资源</Empty>
  if (!data) return <Empty>{error || "加载中…"}</Empty>

  const save = async (submit = false) => {
    setBusy(true)
    setError("")
    try {
      const saved = await saveResourceDraft(id, {
        name,
        summary,
        paid_type: paidType,
        revision_id: data.revision_id,
        attributes,
        links: data.links || [],
        publication_plan: data.publication_plan,
      })
      if (submit) {
        await submitResourceDraft(id, saved.revision_id || data.revision_id)
        toast("已提交审核")
        navigate("/review")
        return
      }
      toast("草稿已保存")
      load()
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const sendFile = async (path: string, file: File, extra: Record<string, string> = {}) => {
    const form = new FormData()
    form.set("file", file)
    Object.entries(extra).forEach(([key, value]) => form.set(key, value))
    await upload(path, form)
    toast("已上传")
    load()
  }

  return (
    <>
      <PageHeader title={data.name || data.slug} hint={`${data.owner} · ${data.slug} · ${data.kind}`}>
        <Status value={data.moderation} label={stateLabel[data.moderation] || data.moderation} />
        {data.can_submit && (
          <button className="btn btn-primary" disabled={busy} onClick={() => save(true)}>
            提交审核
          </button>
        )}
        <button className="btn" type="button" onClick={() => setStateOpen("suspend")}>
          下架
        </button>
      </PageHeader>
      <Tabs
        value={tab}
        onChange={setTab}
        items={[
          { id: "edit", label: "资料" },
          { id: "assets", label: "媒体与安装包" },
          { id: "governance", label: "治理" },
          { id: "history", label: "历史" },
        ]}
      />
      <div className="page-body">
        {tab === "edit" && (
          <form
            className="stack"
            onSubmit={(event: FormEvent) => {
              event.preventDefault()
              save(false)
            }}
          >
            <Field label="名称">
              <input value={name} onChange={(event) => setName(event.target.value)} />
            </Field>
            <Field label="简介">
              <textarea value={summary} onChange={(event) => setSummary(event.target.value)} />
            </Field>
            <Field label="付费">
              <select value={paidType} onChange={(event) => setPaidType(event.target.value)}>
                <option value="free">免费</option>
                <option value="paid">付费</option>
                <option value="force_paid">强制付费</option>
              </select>
            </Field>
            {!!data.attribute_catalog?.length && (
              <div className="choice-grid">
                {data.attribute_catalog.map((item: { id: string; name_zh: string }) => (
                  <label key={item.id}>
                    <input type="checkbox" checked={attributes.includes(item.id)} onChange={(event) => setAttributes(event.target.checked ? [...attributes, item.id] : attributes.filter((value) => value !== item.id))} />
                    {item.name_zh || item.id}
                  </label>
                ))}
              </div>
            )}
            {data.pending && <p className="hint">这一版正在审核，保存会改待审内容。</p>}
            {error && <div className="error">{error}</div>}
            <button className="btn" disabled={busy || !data.editable} type="submit">
              保存草稿
            </button>
          </form>
        )}
        {tab === "assets" && (
          <div className="stack">
            <div className="panel">
              <h3>图片</h3>
              <div className="gallery">
                {(data.media || []).map((media: { id: string; url: string; role: string }) => (
                  <img key={media.id || media.url} src={media.url} alt={media.role} />
                ))}
              </div>
              {data.editable && (
                <input
                  type="file"
                  accept="image/*"
                  onChange={(event) => {
                    const file = event.target.files?.[0]
                    if (file) sendFile(`/admin/resources/${id}/draft/${data.revision_id}/media`, file, { role: "preview" })
                  }}
                />
              )}
            </div>
            <div className="panel">
              <h3>安装包</h3>
              {(data.artifacts || []).length === 0 && <Empty>还没有安装包</Empty>}
              {(data.artifacts || []).map((artifact: { id: string; name: string; version: string; devices?: string[]; url: string }) => (
                <div className="file" key={artifact.id}>
                  <div>
                    <div>{artifact.name}</div>
                    <small>
                      {artifact.version} {artifact.devices?.join("、")}
                    </small>
                  </div>
                  <a className="btn" href={artifact.url}>
                    下载
                  </a>
                </div>
              ))}
              {data.editable && (
                <input
                  type="file"
                  onChange={(event) => {
                    const file = event.target.files?.[0]
                    if (file) sendFile(`/admin/resources/${id}/draft/${data.revision_id}/artifacts`, file)
                  }}
                />
              )}
            </div>
          </div>
        )}
        {tab === "governance" && (
          <form
            className="stack"
            onSubmit={async (event) => {
              event.preventDefault()
              try {
                await api.post(`/admin/api/resources/${id}/draft/${data.revision_id}/governance`, governance)
                toast("治理信息已保存")
              } catch (err) {
                toast((err as Error).message, "err")
              }
            }}
          >
            <Field label="作者">
              <input value={governance.author_name} onChange={(event) => setGovernance({ ...governance, author_name: event.target.value })} />
            </Field>
            <Field label="来源">
              <input value={governance.source_url} onChange={(event) => setGovernance({ ...governance, source_url: event.target.value })} />
            </Field>
            <Field label="许可证">
              <input value={governance.license_name} onChange={(event) => setGovernance({ ...governance, license_name: event.target.value })} />
            </Field>
            <Field label="授权说明">
              <textarea value={governance.authorization_note} onChange={(event) => setGovernance({ ...governance, authorization_note: event.target.value })} />
            </Field>
            <button className="btn" type="submit">
              保存治理信息
            </button>
          </form>
        )}
        {tab === "history" && (
          <div className="stack">
            {(data.revisions || []).map((revision: { id: string; number: number; name: string; state: string; created_at: string }) => (
              <div className="file" key={revision.id}>
                <div>
                  <div>
                    #{revision.number} {revision.name}
                  </div>
                  <small>
                    {revision.state} · {formatRelative(revision.created_at)}
                  </small>
                </div>
                <button
                  className="btn"
                  type="button"
                  onClick={() =>
                    api.post(`/admin/api/resources/${id}/revisions/${revision.id}/rollback`).then(() => {
                      toast("已生成回滚草稿")
                      load()
                    })
                  }
                >
                  回滚到此版
                </button>
              </div>
            ))}
          </div>
        )}
      </div>
      <Dialog
        open={!!stateOpen}
        title="变更资源状态"
        hint="下架和冻结需要写原因。"
        onClose={() => setStateOpen("")}
        footer={
          <>
            <button className="btn" type="button" onClick={() => setStateOpen("")}>
              取消
            </button>
            <button
              className="btn btn-danger"
              type="button"
              disabled={!reason.trim()}
              onClick={() =>
                api
                  .post(`/admin/api/resources/${id}/state`, { action: stateOpen, reason })
                  .then(() => {
                    toast("状态已更新")
                    setStateOpen("")
                    load()
                  })
                  .catch((err: Error) => toast(err.message, "err"))
              }
            >
              确认
            </button>
          </>
        }
      >
        <Field label="原因">
          <textarea value={reason} onChange={(event) => setReason(event.target.value)} />
        </Field>
      </Dialog>
    </>
  )
}
