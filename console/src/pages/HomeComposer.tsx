import { useEffect, useState } from "react"
import { api } from "../api"
import { Dialog, Empty, Field, PageHeader, toast } from "../ui"
import { type Row } from "./system-helpers"

export function HomeComposerPage() {
  const [banners, setBanners] = useState<Row[]>([])
  const [sections, setSections] = useState<Row[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState("")
  const [bannerOpen, setBannerOpen] = useState(false)
  const [editingBanner, setEditingBanner] = useState<Row | null>(null)
  const [bannerForm, setBannerForm] = useState({ type: "resource", title: "", subtitle: "", resource_id: "", blog_slug: "", link_url: "", enabled: true })
  const [sectionOpen, setSectionOpen] = useState(false)
  const [sectionForm, setSectionForm] = useState({ name: "", description: "", enabled: true })
  const [cardTarget, setCardTarget] = useState("")
  const [cardOpen, setCardOpen] = useState(false)
  const [cardForm, setCardForm] = useState({ type: "resource", resource_id: "", blog_slug: "" })
  const [busy, setBusy] = useState(false)

  const load = async () => {
    setLoading(true)
    setError("")
    try {
      const data = await api.get<Row>("/admin/api/home")
      setBanners(data.banners || [])
      setSections(data.sections || [])
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setLoading(false)
    }
  }
  useEffect(() => {
    void load()
  }, [])

  const run = (promise: Promise<unknown>, message: string) => {
    setBusy(true)
    return promise
      .then(() => {
        toast(message)
        return load()
      })
      .catch((err: Error) => toast(err.message, "err"))
      .finally(() => setBusy(false))
  }

  const openBannerCreate = () => {
    setEditingBanner(null)
    setBannerForm({ type: "resource", title: "", subtitle: "", resource_id: "", blog_slug: "", link_url: "", enabled: true })
    setBannerOpen(true)
  }
  const openBannerEdit = (banner: Row) => {
    setEditingBanner(banner)
    setBannerForm({ type: banner.type || "resource", title: banner.title || "", subtitle: banner.subtitle || "", resource_id: banner.resource_id || "", blog_slug: banner.blog_slug || "", link_url: banner.link_url || "", enabled: banner.enabled !== false })
    setBannerOpen(true)
  }
  const saveBanner = () => {
    const path = editingBanner ? `/admin/api/home/banners/${editingBanner.id}` : "/admin/api/home/banners"
    run(api.post(path, bannerForm), editingBanner ? "已保存" : "已创建").then(() => {
      setBannerOpen(false)
      setEditingBanner(null)
    })
  }

  const saveSection = () => {
    run(api.post("/admin/api/home/sections", sectionForm), "已创建分区").then(() => {
      setSectionOpen(false)
      setSectionForm({ name: "", description: "", enabled: true })
    })
  }

  const openCardAdd = (sectionID: string) => {
    setCardTarget(sectionID)
    setCardForm({ type: "resource", resource_id: "", blog_slug: "" })
    setCardOpen(true)
  }
  const saveCard = () => {
    run(api.post("/admin/api/home/cards", { ...cardForm, section_id: cardTarget }), "已添加卡片").then(() => setCardOpen(false))
  }

  const moveCard = (card: Row, delta: number) => {
    const sectionID = card.section_id || cardTarget
    run(api.post(`/admin/api/home/cards/${card.id}/move${sectionID ? `?section_id=${encodeURIComponent(String(sectionID))}` : ""}`, { delta }), "已调整顺序")
  }

  const bannerTarget = (banner: Row) => (banner.type === "resource" ? banner.resource_id : banner.type === "blog" ? banner.blog_slug : banner.link_url)

  return (
    <>
      <PageHeader title="首页编排" hint="首页 = Banner + 分区卡片，排序和删除即时生效。">
        <div className="actions">
          <button className="btn" type="button" disabled={busy} onClick={() => setSectionOpen(true)}>新建分区</button>
          <button className="btn btn-primary" type="button" disabled={busy} onClick={openBannerCreate}>新建 Banner</button>
        </div>
      </PageHeader>
      {error ? <div className="error page-error">{error}</div> : null}
      <div className="page-body stack">
        {loading ? <Empty>加载中</Empty> : null}
        {!loading && !banners.length && !sections.length ? <Empty>还没有首页内容</Empty> : null}

        {!loading && banners.map((banner) => (
          <div className="file" key={banner.id}>
            <div>
              <strong>{banner.title || "无标题"}</strong>
              <small>
                {banner.type === "resource" ? "资源" : banner.type === "blog" ? "Blog" : "链接"} · {banner.enabled ? "启用" : "停用"} · 第 {banner.position ?? "—"} 项 · {bannerTarget(banner) || "无目标"}
              </small>
              {banner.subtitle ? <p className="hint">{banner.subtitle}</p> : null}
            </div>
            <div className="row-actions">
              <button className="btn" type="button" disabled={busy} onClick={() => run(api.post(`/admin/api/home/banners/${banner.id}/move`, { delta: -1 }), "已上移")}>上移</button>
              <button className="btn" type="button" disabled={busy} onClick={() => run(api.post(`/admin/api/home/banners/${banner.id}/move`, { delta: 1 }), "已下移")}>下移</button>
              <button className="btn" type="button" disabled={busy} onClick={() => openBannerEdit(banner)}>编辑</button>
              <button className="btn btn-danger" type="button" disabled={busy} onClick={() => run(api.post(`/admin/api/home/banners/${banner.id}/delete`), "已删除")}>删除</button>
            </div>
          </div>
        ))}

        {!loading && sections.map((block) => (
          <div className="panel" key={block.section?.id}>
            <div className="section-head">
              <div>
                <h3>{block.section?.name || "未命名分区"}</h3>
                <p className="hint">{block.section?.description || "—"} · {block.section?.enabled ? "启用" : "停用"} · 第 {block.section?.position ?? "—"} 项</p>
              </div>
              <div className="row-actions">
                <span className="section-count">{block.cards?.length || 0} 项</span>
                <button className="btn" type="button" disabled={busy} onClick={() => run(api.post(`/admin/api/home/sections/${block.section.id}/move`, { delta: -1 }), "已上移")}>上移</button>
                <button className="btn" type="button" disabled={busy} onClick={() => run(api.post(`/admin/api/home/sections/${block.section.id}/move`, { delta: 1 }), "已下移")}>下移</button>
                <button className="btn btn-danger" type="button" disabled={busy} onClick={() => run(api.post(`/admin/api/home/sections/${block.section.id}/delete`), "已删除分区")}>删除</button>
              </div>
            </div>
            <div className="stack">
              {(block.cards || []).map((card: Row) => (
                <div key={card.id} className="file">
                  <span>{card.type === "resource" ? "资源" : "Blog"} · <span className="mono">{card.resource_id || card.blog_slug || "—"}</span> · 第 {card.position ?? "—"} 项</span>
                  <div className="row-actions">
                    <button className="btn" type="button" disabled={busy} onClick={() => moveCard(card, -1)}>上移</button>
                    <button className="btn" type="button" disabled={busy} onClick={() => moveCard(card, 1)}>下移</button>
                    <button className="btn btn-danger" type="button" disabled={busy} onClick={() => run(api.post(`/admin/api/home/cards/${card.id}/delete`), "已删除")}>删除</button>
                  </div>
                </div>
              ))}
              <div>
                <button className="btn" type="button" disabled={busy} onClick={() => openCardAdd(block.section.id)}>添加卡片</button>
              </div>
            </div>
          </div>
        ))}
      </div>

      <Dialog
        open={bannerOpen}
        title={editingBanner ? "编辑 Banner" : "新建 Banner"}
        onClose={() => setBannerOpen(false)}
        footer={
          <button className="btn btn-primary" type="button" disabled={busy} onClick={saveBanner}>
            保存
          </button>
        }
      >
        <Field label="类型">
          <select value={bannerForm.type} onChange={(event) => setBannerForm({ ...bannerForm, type: event.target.value })}>
            <option value="resource">资源</option>
            <option value="blog">Blog 文章</option>
            <option value="link">外链</option>
          </select>
        </Field>
        <Field label="标题">
          <input value={bannerForm.title} onChange={(event) => setBannerForm({ ...bannerForm, title: event.target.value })} />
        </Field>
        <Field label="副标题">
          <input value={bannerForm.subtitle} onChange={(event) => setBannerForm({ ...bannerForm, subtitle: event.target.value })} />
        </Field>
        {bannerForm.type === "resource" ? (
          <Field label="资源 ID">
            <input value={bannerForm.resource_id} onChange={(event) => setBannerForm({ ...bannerForm, resource_id: event.target.value })} placeholder="资源 UUID" />
          </Field>
        ) : null}
        {bannerForm.type === "blog" ? (
          <Field label="Blog 地址">
            <input value={bannerForm.blog_slug} onChange={(event) => setBannerForm({ ...bannerForm, blog_slug: event.target.value })} placeholder="文章 slug" />
          </Field>
        ) : null}
        {bannerForm.type === "link" ? (
          <Field label="链接地址">
            <input value={bannerForm.link_url} onChange={(event) => setBannerForm({ ...bannerForm, link_url: event.target.value })} placeholder="https://…" />
          </Field>
        ) : null}
        <label className="check-field">
          <input type="checkbox" checked={bannerForm.enabled} onChange={(event) => setBannerForm({ ...bannerForm, enabled: event.target.checked })} />
          启用
        </label>
      </Dialog>

      <Dialog
        open={sectionOpen}
        title="新建分区"
        onClose={() => setSectionOpen(false)}
        footer={
          <button className="btn btn-primary" type="button" disabled={busy} onClick={saveSection}>
            创建
          </button>
        }
      >
        <Field label="名称">
          <input value={sectionForm.name} onChange={(event) => setSectionForm({ ...sectionForm, name: event.target.value })} />
        </Field>
        <Field label="描述">
          <input value={sectionForm.description} onChange={(event) => setSectionForm({ ...sectionForm, description: event.target.value })} />
        </Field>
        <label className="check-field">
          <input type="checkbox" checked={sectionForm.enabled} onChange={(event) => setSectionForm({ ...sectionForm, enabled: event.target.checked })} />
          启用
        </label>
      </Dialog>

      <Dialog
        open={cardOpen}
        title="添加卡片"
        onClose={() => setCardOpen(false)}
        footer={
          <button className="btn btn-primary" type="button" disabled={busy} onClick={saveCard}>
            添加
          </button>
        }
      >
        <Field label="类型">
          <select value={cardForm.type} onChange={(event) => setCardForm({ ...cardForm, type: event.target.value })}>
            <option value="resource">资源</option>
            <option value="blog">Blog 文章</option>
          </select>
        </Field>
        {cardForm.type === "resource" ? (
          <Field label="资源 ID">
            <input value={cardForm.resource_id} onChange={(event) => setCardForm({ ...cardForm, resource_id: event.target.value })} placeholder="资源 UUID" />
          </Field>
        ) : (
          <Field label="Blog 地址">
            <input value={cardForm.blog_slug} onChange={(event) => setCardForm({ ...cardForm, blog_slug: event.target.value })} placeholder="文章 slug" />
          </Field>
        )}
      </Dialog>
    </>
  )
}