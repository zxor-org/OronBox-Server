import { useEffect, useState } from "react"
import { useLocation } from "react-router"
import { api } from "../api"
import { Dialog, Empty, Field, FieldList, PageHeader, Pagination, SearchForm, TableState, Status, toast } from "../ui"
import { type Row, type SystemColumn, detailRecord, systemCell, useRows, HealthDiagnostics } from "./system-helpers"

type SystemConfig = {
  title: string
  hint: string
  path: string
  columns: SystemColumn[]
  searchPlaceholder?: string
  detailPath?: (row: Row) => string
}

export function SystemPage() {
  const location = useLocation()
  const path = location.pathname
  const map: Record<string, SystemConfig> = {
    "/oauth/events": { title: "OAuth 事件", hint: "查看授权事件、结果和来源", path: "/admin/api/oauth/events", searchPlaceholder: "事件、应用或用户", detailPath: (row) => row.id ? `/admin/api/oauth/events/${encodeURIComponent(String(row.id))}` : "", columns: [{ key: "event_type", label: "事件" }, { key: "result", label: "结果", format: "status" }, { key: "platform", label: "平台" }] },
    "/oauth/states": { title: "OAuth States", hint: "查看未完成的授权状态", path: "/admin/api/oauth/states", searchPlaceholder: "State ID、应用或用户", detailPath: (row) => row.id ? `/admin/api/oauth/states/${encodeURIComponent(String(row.id))}` : "", columns: [{ key: "id", label: "ID" }, { key: "status", label: "状态", format: "status" }, { key: "app_id", label: "应用" }, { key: "created_at", label: "创建时间", format: "date" }, { key: "expires_at", label: "过期时间", format: "date" }] },
    "/oauth/tickets": { title: "登录 Tickets", hint: "查看登录票据及其状态", path: "/admin/api/oauth/tickets", searchPlaceholder: "Ticket ID、应用或用户", detailPath: (row) => row.id ? `/admin/api/oauth/tickets/${encodeURIComponent(String(row.id))}` : "", columns: [{ key: "id", label: "ID" }, { key: "status", label: "状态", format: "status" }, { key: "app_id", label: "应用" }, { key: "created_at", label: "创建时间", format: "date" }, { key: "expires_at", label: "过期时间", format: "date" }] },
    "/clients": { title: "客户端统计", hint: "按应用和平台查看访问统计", path: "/admin/api/clients", searchPlaceholder: "应用、版本或平台", columns: [{ key: "app_id", label: "应用" }, { key: "platform", label: "平台" }, { key: "app_version", label: "版本" }, { key: "request_count", label: "请求数" }, { key: "success_count", label: "成功" }, { key: "failure_count", label: "失败" }, { key: "last_seen", label: "最近活动", format: "date" }] },
    "/storage/blobs": { title: "Blob 与副本", hint: "查看本地文件、副本状态和资源引用", path: "/admin/api/blobs", searchPlaceholder: "SHA256、媒体类型或副本状态", detailPath: (row) => row.sha256 ? `/admin/api/blobs/${encodeURIComponent(String(row.sha256))}` : "", columns: [{ key: "sha256", label: "SHA256" }, { key: "size_bytes", label: "大小", format: "bytes" }, { key: "media_type", label: "类型" }, { key: "local_available", label: "本地", format: "boolean" }, { key: "r2_state", label: "副本", format: "status" }, { key: "referenced", label: "已引用", format: "boolean" }, { key: "created_at", label: "创建时间", format: "date" }] },
    "/health": { title: "运行状态", hint: "查看服务依赖和当前延迟", path: "/admin/api/health", columns: [{ key: "db", label: "数据库", format: "status" }, { key: "latency", label: "延迟" }, { key: "version", label: "版本" }] },
    "/audit": { title: "审计日志", hint: "按动作、操作者和结果查询", path: "/admin/api/audit", searchPlaceholder: "动作、操作者或目标", detailPath: (row) => row.id ? `/admin/api/audit/${encodeURIComponent(String(row.id))}` : "", columns: [{ key: "action", label: "动作" }, { key: "username", label: "操作者" }, { key: "result", label: "结果", format: "status" }, { key: "target", label: "目标" }, { key: "ip", label: "IP" }, { key: "created_at", label: "时间", format: "date" }] },
  }
  const config = map[path] || map["/audit"]
  const { items, total, payload, q, setQ, page, setPage, error, loading, load } = useRows(config.path)
  const [cleanup, setCleanup] = useState<Row | null>(null)
  const [open, setOpen] = useState<Row | null>(null)
  const [detail, setDetail] = useState<Row | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)
  const [detailError, setDetailError] = useState("")
  const requeue = path === "/storage/blobs"
  const detailPath = open && config.detailPath ? config.detailPath(open) : ""

  useEffect(() => {
    setOpen(null)
    setDetail(null)
    setDetailError("")
  }, [path])

  useEffect(() => {
    if (!open) {
      setDetail(null)
      setDetailError("")
      setDetailLoading(false)
      return
    }
    if (!detailPath) {
      setDetail(open)
      setDetailError("")
      setDetailLoading(false)
      return
    }
    let active = true
    setDetail(open)
    setDetailError("")
    setDetailLoading(true)
    api
      .get<Row>(detailPath)
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
  }, [open, detailPath])
  return (
    <>
      <PageHeader title={config.title} hint={config.hint}>
        {config.searchPlaceholder ? <SearchForm value={q} onChange={setQ} onSubmit={() => { setPage(1); load(q, 1) }} placeholder={config.searchPlaceholder} /> : null}
        {path === "/health" && (
          <button
            className="btn"
            type="button"
            onClick={() => api.post("/admin/cleanup/preview").then((data) => setCleanup(data as Row)).catch((err: Error) => toast(err.message, "err"))}
          >
            预览清理
          </button>
        )}
      </PageHeader>
      <div className="table-wrap system-table-wrap">
        <TableState loading={loading} error={error} onRetry={() => void load()} isEmpty={!items.length} empty={q ? "没有匹配的数据" : "没有数据"}>
          <table>
            <thead>
              <tr>
                {config.columns.map((column) => (
                  <th key={column.key}>{column.label}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {items.map((row, index) => (
                <tr key={String(row.id || row.sha256 || index)} className={config.detailPath ? "clickable" : undefined} onClick={config.detailPath ? () => setOpen(row) : undefined}>
                  {config.columns.map((column) => (
                    <td key={column.key}>{systemCell(row, column)}</td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </TableState>
      </div>
      {path === "/health" && payload.diagnostics ? <HealthDiagnostics value={payload.diagnostics} /> : null}
      <Pagination page={page} total={total} perPage={25} onChange={setPage} />
      <Dialog
        open={!!cleanup}
        title="清理过期数据"
        hint={cleanup?.confirmation}
        onClose={() => setCleanup(null)}
        footer={
          <button
            className="btn btn-danger"
            onClick={() =>
              api
                .post("/admin/api/cleanup", { token: cleanup?.token, confirmation: cleanup?.confirmation })
                .then(() => {
                  toast("清理完成")
                  setCleanup(null)
                })
                .catch((err: Error) => toast(err.message, "err"))
            }
          >
            确认清理
          </button>
        }
      >
        <pre className="summary">{JSON.stringify(cleanup?.preview, null, 2)}</pre>
      </Dialog>
      <Dialog
        open={Boolean(config.detailPath && open)}
        title={open?.sha256 ? "Blob 详情" : `${config.title}详情`}
        wide
        onClose={() => setOpen(null)}
        footer={
          <>
            {requeue && open?.sha256 && (
              <button
                className="btn"
                type="button"
                onClick={() =>
                  api
                    .post(`/admin/api/blobs/${open.sha256}/requeue`)
                    .then(() => {
                      toast("已重新入队")
                      setOpen(null)
                      load()
                    })
                    .catch((err: Error) => toast(err.message, "err"))
                }
              >
                重试副本
              </button>
            )}
            <button className="btn" type="button" onClick={() => setOpen(null)}>
              关闭
            </button>
          </>
        }
      >
        {detailLoading ? <p className="hint">加载详情…</p> : null}
        {detailError ? <div className="error">{detailError}</div> : null}
        <FieldList row={detailRecord(detail)} prefer={config.columns.map((column) => column.key)} />
        <details>
          <summary>原始数据</summary>
          <pre className="summary">{JSON.stringify(detail || open, null, 2)}</pre>
        </details>
      </Dialog>
    </>
  )
}

export function SettingsPage() {
  const [config, setConfig] = useState<Row | null>(null)
  const [attributes, setAttributes] = useState<Row[]>([])
  const [prompt, setPrompt] = useState("")
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState("")
  const [attrOpen, setAttrOpen] = useState(false)
  const [editingAttr, setEditingAttr] = useState<Row | null>(null)
  const [attrForm, setAttrForm] = useState({ id: "", name_zh: "", name_en: "", coefficient: 1, enabled: true })
  const [promptTest, setPromptTest] = useState("")
  const [promptVerdict, setPromptVerdict] = useState<Row | null>(null)
  const [busy, setBusy] = useState(false)
  const [ranking, setRanking] = useState({ coin_extra_weight: 0.35, download_weight: 0.15, freshness_amplitude: 3, freshness_decay_days: 7, featured_boost: 1.5, jitter_base: 0.5 })
  const rankingFields: { key: keyof typeof ranking; label: string; hint: string }[] = [
    { key: "coin_extra_weight", label: "硬币加成系数", hint: "投票余额超过投票人数部分的加权" },
    { key: "download_weight", label: "下载量系数", hint: "下载量的 ln 加成" },
    { key: "freshness_amplitude", label: "新鲜度幅度", hint: "新资源的峰值加成" },
    { key: "freshness_decay_days", label: "新鲜度衰减天数", hint: "加成按 e^(-age/days) 衰减" },
    { key: "featured_boost", label: "精选加成", hint: "精选资源的倍数" },
    { key: "jitter_base", label: "随机系数", hint: "确定性洗牌偏移，实际抖动范围为该值到 +1" },
  ]

  const load = async () => {
    setLoading(true)
    setError("")
    try {
      const data = await api.get<Row>("/admin/api/settings")
      setConfig((data.items || [])[0] || null)
      setAttributes(data.attributes || [])
      setPrompt(data.moderation_prompt || "")
      if (data.ranking) {
        setRanking((current) => ({ ...current, ...data.ranking }))
      }
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setLoading(false)
    }
  }
  useEffect(() => {
    void load()
  }, [])

  const openAttrCreate = () => {
    setEditingAttr(null)
    setAttrForm({ id: "", name_zh: "", name_en: "", coefficient: 1, enabled: true })
    setAttrOpen(true)
  }
  const openAttrEdit = (attr: Row) => {
    setEditingAttr(attr)
    setAttrForm({ id: attr.id || "", name_zh: attr.name_zh || "", name_en: attr.name_en || "", coefficient: Number(attr.coefficient) || 1, enabled: attr.enabled !== false })
    setAttrOpen(true)
  }
  const saveAttribute = () => {
    setBusy(true)
    api
      .post("/admin/api/attributes", attrForm)
      .then(() => {
        toast("已保存")
        setAttrOpen(false)
        setEditingAttr(null)
        load()
      })
      .catch((err: Error) => toast(err.message, "err"))
      .finally(() => setBusy(false))
  }
  const disableAttribute = (attr: Row) => {
    setBusy(true)
    api
      .post(`/admin/api/attributes/${encodeURIComponent(String(attr.id))}/delete`)
      .then(() => {
        toast("已停用")
        load()
      })
      .catch((err: Error) => toast(err.message, "err"))
      .finally(() => setBusy(false))
  }
  const savePrompt = (test = false) => {
    setBusy(true)
    api
      .post("/admin/api/moderation/prompt", { prompt, text: promptTest, test })
      .then((data) => {
        if (test) {
          setPromptVerdict(data.verdict as Row)
        } else {
          toast("已保存审核提示词")
          load()
        }
      })
      .catch((err: Error) => toast(err.message, "err"))
      .finally(() => setBusy(false))
  }

  const saveRanking = () => {
    setBusy(true)
    api
      .post("/admin/api/settings/ranking", ranking)
      .then((data) => {
        toast("排序权重已生效")
        if (data.ranking) setRanking((current) => ({ ...current, ...data.ranking }))
      })
      .catch((err: Error) => toast(err.message, "err"))
      .finally(() => setBusy(false))
  }

  return (
    <>
      <PageHeader title="设置" hint="服务端配置、资源属性与评论审核提示词。">
        <button className="btn" type="button" onClick={() => void load()} disabled={loading}>刷新</button>
      </PageHeader>
      <div className="page-body stack">
        {error ? <div className="table-state error page-error"><span>{error}</span><button className="btn small-btn" type="button" onClick={() => void load()}>重试</button></div> : null}
        {loading ? <Empty>加载中</Empty> : null}
        {config ? (
          <div className="panel">
            <div className="section-head"><div><h3>服务配置</h3><p className="hint">密钥只显示状态，不显示原文</p></div></div>
            <div className="compact-kv kv">
              <div><dt>BandBBS 客户端</dt><dd className="mono">{config.bandbbs_client_id || "未配置"}</dd></div>
              <div><dt>GitHub 客户端</dt><dd className="mono">{config.github_client_id || "未配置"}</dd></div>
              <div><dt>公共地址</dt><dd className="mono">{config.public_url || "—"}</dd></div>
            </div>
          </div>
        ) : null}

        <div className="panel">
          <div className="section-head">
            <div><h3>资源属性</h3><p className="hint">属性用于资源分类与系数加成，停用后不可再选择</p></div>
            <button className="btn" type="button" disabled={busy} onClick={openAttrCreate}>新建属性</button>
          </div>
          <TableState loading={loading} error="" isEmpty={!attributes.length} empty="还没有属性">
            <table>
              <thead>
                <tr><th>ID</th><th>中文名</th><th>英文名</th><th>系数</th><th>使用数</th><th>状态</th><th></th></tr>
              </thead>
              <tbody>
                {attributes.map((attr) => (
                  <tr key={attr.id}>
                    <td className="mono">{attr.id}</td>
                    <td>{attr.name_zh}</td>
                    <td>{attr.name_en || "—"}</td>
                    <td>{attr.coefficient}</td>
                    <td>{attr.usage_count ?? "—"}</td>
                    <td><Status value={attr.enabled ? "enabled" : "disabled"} /></td>
                    <td>
                      <div className="row-actions">
                        <button className="btn" type="button" disabled={busy} onClick={() => openAttrEdit(attr)}>编辑</button>
                        {attr.enabled ? <button className="btn btn-danger" type="button" disabled={busy} onClick={() => disableAttribute(attr)}>停用</button> : null}
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </TableState>
        </div>

        <div className="panel">
          <div className="section-head"><div><h3>推荐排序权重</h3><p className="hint">即时生效；推荐分 = 参与度 × 新鲜度 × 精选 × 属性系数 × 随机系数</p></div></div>
          <div className="stack">
            <div className="form-grid">
              {rankingFields.map((field) => (
                <Field key={field.key} label={field.label}>
                  <input
                    type="number"
                    step="0.05"
                    min="0.01"
                    value={ranking[field.key]}
                    onChange={(event) => setRanking({ ...ranking, [field.key]: Number(event.target.value) })}
                  />
                  <small className="hint">{field.hint}</small>
                </Field>
              ))}
            </div>
            <div className="actions">
              <button className="btn btn-primary" type="button" disabled={busy} onClick={saveRanking}>保存排序权重</button>
            </div>
          </div>
        </div>

        <div className="panel">
          <div className="section-head"><div><h3>评论审核提示词</h3><p className="hint">同步审核所有新评论；模型未启用时忽略</p></div></div>
          <div className="stack">
            <Field label="提示词">
              <textarea value={prompt} onChange={(event) => setPrompt(event.target.value)} rows={8} />
            </Field>
            <div className="actions">
              <button className="btn btn-primary" type="button" disabled={busy} onClick={() => savePrompt(false)}>保存</button>
            </div>
            <Field label="测试文本">
              <textarea value={promptTest} onChange={(event) => setPromptTest(event.target.value)} placeholder="输入一段评论，预览审核结论" />
            </Field>
            <div className="actions">
              <button className="btn" type="button" disabled={busy || !promptTest.trim()} onClick={() => savePrompt(true)}>测试审核</button>
            </div>
            {promptVerdict ? (
              <div className="kv compact-kv">
                <div><dt>结论</dt><dd><Status value={promptVerdict.action} /></dd></div>
                <div><dt>分类</dt><dd>{promptVerdict.categories?.length ? promptVerdict.categories.join("、") : "—"}</dd></div>
                <div><dt>理由</dt><dd>{promptVerdict.reason || "—"}</dd></div>
              </div>
            ) : null}
          </div>
        </div>
      </div>
      <Dialog
        open={attrOpen}
        title={editingAttr ? `编辑属性 ${editingAttr.id}` : "新建属性"}
        onClose={() => setAttrOpen(false)}
        footer={
          <button className="btn btn-primary" type="button" disabled={busy || !attrForm.id.trim()} onClick={saveAttribute}>
            保存
          </button>
        }
      >
        <div className="form-grid">
          <Field label="ID（小写字母数字）">
            <input value={attrForm.id} disabled={!!editingAttr} onChange={(event) => setAttrForm({ ...attrForm, id: event.target.value })} placeholder="例如 community" />
          </Field>
          <Field label="系数">
            <input type="number" step="0.01" min="0.01" max="10" value={attrForm.coefficient} onChange={(event) => setAttrForm({ ...attrForm, coefficient: Number(event.target.value) })} />
          </Field>
        </div>
        <Field label="中文名">
          <input value={attrForm.name_zh} onChange={(event) => setAttrForm({ ...attrForm, name_zh: event.target.value })} />
        </Field>
        <Field label="英文名">
          <input value={attrForm.name_en} onChange={(event) => setAttrForm({ ...attrForm, name_en: event.target.value })} />
        </Field>
        <label className="check-field">
          <input type="checkbox" checked={attrForm.enabled} onChange={(event) => setAttrForm({ ...attrForm, enabled: event.target.checked })} />
          启用
        </label>
      </Dialog>
    </>
  )
}