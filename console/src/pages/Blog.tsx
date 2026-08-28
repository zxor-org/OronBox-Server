import { FormEvent, useEffect, useState } from "react"
import { useNavigate, useParams } from "react-router"
import { api } from "../api"
import { Dialog, Field, PageHeader, SearchForm, TableState, formatRelative, toast } from "../ui"
import { type Row, useRows } from "./system-helpers"

export function BlogPage() {
  const { items, total, q, setQ, page, setPage, error, loading, load } = useRows("/admin/api/blog")
  const { slug } = useParams()
  const navigate = useNavigate()
  const [post, setPost] = useState({ slug: "", title: "", subtitle: "", author: "", body: "", type: "announcement", published: false })
  const [postError, setPostError] = useState("")
  const [deleting, setDeleting] = useState(false)
  useEffect(() => {
    if (!slug) {
      setPost({ slug: "", title: "", subtitle: "", author: "", body: "", type: "announcement", published: false })
      setPostError("")
      return
    }
    api.get<Row>(`/admin/api/blog/${slug}`).then((value) => setPost((current) => ({ ...current, ...value }))).catch((err: Error) => setPostError(err.message))
  }, [slug])
  return (
    <>
      <PageHeader title="Blog" hint="管理公告和说明文章，选择左侧条目后编辑。">
        <SearchForm value={q} onChange={setQ} onSubmit={() => { setPage(1); load(q, 1) }} placeholder="搜索文章" />
        <button className="btn btn-primary" type="button" onClick={() => navigate("/blog")}>新建文章</button>
      </PageHeader>
      <div className="split">
        <div className="queue">
          <TableState loading={loading} error={error} onRetry={() => void load()} isEmpty={!items.length} empty={q ? "没有匹配的文章" : "没有文章"}>
            {items.map((item) => (
              <a key={item.slug} className={`queue-item ${item.slug === slug ? "selected" : ""}`} href={`/admin/blog/${item.slug}`}>
                <div className="title">{item.title}</div>
                <div className="meta"><span>{item.author || "未署名"}</span><span>{item.published ? "已发布" : "草稿"}</span><span>{formatRelative(item.updated_at)}</span></div>
                <div className="hint">{item.slug}</div>
              </a>
            ))}
          </TableState>
        </div>
        <div className="detail">
          {postError ? <div className="error">{postError}</div> : null}
          {!slug && !post.title ? <div className="detail-empty">新建文章，填写右侧表单</div> : null}
          <form
            className="stack"
            onSubmit={(event: FormEvent) => {
              event.preventDefault()
              api
                .post(post.slug ? `/admin/api/blog/${post.slug}` : "/admin/api/blog", post)
                .then(() => { toast("已保存"); setPostError(""); load() })
                .catch((err: Error) => toast(err.message, "err"))
            }}
          >
            <Field label="Slug">
              <input value={post.slug} onChange={(event) => setPost({ ...post, slug: event.target.value })} />
            </Field>
            <Field label="标题">
              <input value={post.title} onChange={(event) => setPost({ ...post, title: event.target.value })} />
            </Field>
            <Field label="副标题">
              <input value={post.subtitle} onChange={(event) => setPost({ ...post, subtitle: event.target.value })} />
            </Field>
            <Field label="作者">
              <input value={post.author} onChange={(event) => setPost({ ...post, author: event.target.value })} />
            </Field>
            <Field label="类型">
              <select value={post.type} onChange={(event) => setPost({ ...post, type: event.target.value })}><option value="announcement">公告</option><option value="guide">指南</option><option value="article">文章</option></select>
            </Field>
            <Field label="正文">
              <textarea value={post.body} onChange={(event) => setPost({ ...post, body: event.target.value })} />
            </Field>
            <label className="check-field">
              <input type="checkbox" checked={post.published} onChange={(event) => setPost({ ...post, published: event.target.checked })} /> 发布
            </label>
            <button className="btn btn-primary" type="submit">
              保存
            </button>
            {post.slug ? (
              <button className="btn btn-danger" type="button" onClick={() => setDeleting(true)}>
                删除
              </button>
            ) : null}
          </form>
          <Dialog
            open={deleting}
            title="删除文章"
            hint={`确定删除「${post.title}」？删除后不可恢复。`}
            onClose={() => setDeleting(false)}
            footer={
              <button
                className="btn btn-danger"
                type="button"
                onClick={() =>
                  api
                    .post(`/admin/api/blog/${encodeURIComponent(post.slug)}/delete`)
                    .then(() => {
                      toast("已删除")
                      setDeleting(false)
                      setPost({ slug: "", title: "", subtitle: "", author: "", body: "", type: "announcement", published: false })
                      navigate("/blog")
                      load()
                    })
                    .catch((err: Error) => toast(err.message, "err"))
                }
              >
                删除
              </button>
            }
          >
            <p className="summary">{post.title}</p>
          </Dialog>
        </div>
      </div>
    </>
  )
}