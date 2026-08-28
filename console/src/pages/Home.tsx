import { useEffect, useState } from "react"
import { Link } from "react-router"
import { ChatCircleIcon, ClipboardTextIcon, CloudArrowUpIcon, FlagIcon, PackageIcon, PuzzlePieceIcon } from "@phosphor-icons/react"
import { api, type Session } from "../api"
import { PageHeader } from "../ui"

type Overview = {
  pending_reviews: number
  overdue_reviews: number
  pending_comments: number
  open_reports: number
  failed_publications: number
  pending_plugins: number
}

type Point = { date: string; count: number }
type Analytics = {
  range: string
  user_growth: Point[]
  downloads: Point[]
  totals: {
    total_users: number
    total_downloads: number
    downloads_7d: number
    downloads_30d: number
    new_users_7d: number
    new_users_30d: number
    resources: number
    published_resources: number
  }
}

const ranges: { id: string; label: string }[] = [
  { id: "30d", label: "近 30 天" },
  { id: "90d", label: "近 90 天" },
  { id: "12m", label: "近 12 个月" },
]

function shortDate(date: string, monthly: boolean) {
  if (monthly) {
    const [, month] = date.split("-")
    return `${month}月`
  }
  const [, month, day] = date.split("-")
  return `${month}-${day}`
}

function TrendChart({ points, monthly, accent }: { points: Point[]; monthly: boolean; accent: string }) {
  const width = 640
  const height = 200
  const padding = { top: 14, right: 8, bottom: 26, left: 40 }
  const innerWidth = width - padding.left - padding.right
  const innerHeight = height - padding.top - padding.bottom
  const max = Math.max(1, ...points.map((point) => point.count))
  const labels = new Set<number>()
  const step = Math.max(1, Math.ceil(points.length / 6))
  for (let index = 0; index < points.length; index += step) labels.add(index)

  return (
    <svg className="trend-chart" viewBox={`0 0 ${width} ${height}`} role="img" aria-label="趋势图">
      {[0, 0.5, 1].map((fraction) => {
        const y = padding.top + innerHeight * (1 - fraction)
        return (
          <g key={fraction}>
            <line x1={padding.left} x2={width - padding.right} y1={y} y2={y} className="chart-grid" />
            <text x={padding.left - 6} y={y + 3} textAnchor="end" className="chart-axis">
              {Math.round(max * fraction)}
            </text>
          </g>
        )
      })}
      {points.map((point, index) => {
        const barWidth = innerWidth / points.length
        const x = padding.left + index * barWidth
        const barHeight = point.count > 0 ? Math.max(2, (point.count / max) * innerHeight) : 0
        const y = padding.top + innerHeight - barHeight
        return (
          <g key={point.date}>
            <rect x={x + 1} y={y} width={Math.max(1, barWidth - 2)} height={barHeight} rx={2} className="chart-bar" style={accent ? { fill: accent } : undefined}>
              <title>{`${point.date}：${point.count}`}</title>
            </rect>
            {labels.has(index) ? (
              <text x={x + barWidth / 2} y={height - 8} textAnchor="middle" className="chart-axis">
                {shortDate(point.date, monthly)}
              </text>
            ) : null}
          </g>
        )
      })}
    </svg>
  )
}

function TrendPanel({ title, hint, points, monthly, accent }: { title: string; hint: string; points: Point[]; monthly: boolean; accent: string }) {
  const total = points.reduce((sum, point) => sum + point.count, 0)
  return (
    <section className="trend-panel">
      <div className="trend-head">
        <div>
          <h3>{title}</h3>
          <p className="hint">{hint}</p>
        </div>
        <strong className="trend-total">{total.toLocaleString("zh-CN")}</strong>
      </div>
      <TrendChart points={points} monthly={monthly} accent={accent} />
    </section>
  )
}

export function HomePage({ session }: { session: Session }) {
  const [overview, setOverview] = useState<Overview | null>(null)
  const [analytics, setAnalytics] = useState<Analytics | null>(null)
  const [range, setRange] = useState("30d")
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState("")

  const load = async () => {
    setLoading(true)
    setError("")
    try {
      const [overviewData, analyticsData] = await Promise.all([
        api.get<Overview>("/admin/api/overview"),
        api.get<Analytics>(`/admin/api/analytics?range=${range}`),
      ])
      setOverview(overviewData)
      setAnalytics(analyticsData)
    } catch (err) {
      setOverview(null)
      setAnalytics(null)
      setError((err as Error).message)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void load()
  }, [session.pending_reviews])

  useEffect(() => {
    if (overview) void load()
  }, [range])

  const monthly = range === "12m"
  const number = (value: number, fallback = "—") => overview ? value : loading ? "…" : fallback
  const cards = [
    { to: "/review", num: overview?.pending_reviews ?? (loading ? session.pending_reviews : "—"), label: "待审核", hint: overview?.overdue_reviews ? `${overview.overdue_reviews} 条已超时` : "", icon: ClipboardTextIcon },
    { to: "/comments", num: number(overview?.pending_comments || 0), label: "待处理评论", icon: ChatCircleIcon },
    { to: "/reports", num: number(overview?.open_reports || 0), label: "未关闭举报", icon: FlagIcon },
    { to: "/publications", num: number(overview?.failed_publications || 0), label: "失败的发布", icon: CloudArrowUpIcon },
    { to: "/plugins", num: number(overview?.pending_plugins || 0), label: "待审插件", icon: PuzzlePieceIcon },
    { to: "/resources", num: "→", label: "进入全部资源", icon: PackageIcon },
  ]
  const totals = analytics?.totals

  return (
    <>
      <PageHeader title="概览" hint={`你好，${session.user}。先处理超时审核和失败发布，下面的图展示用户增长与下载量。`}>
        <div className="actions">
          <select value={range} onChange={(event) => setRange(event.target.value)} aria-label="统计区间">
            {ranges.map((item) => (
              <option key={item.id} value={item.id}>{item.label}</option>
            ))}
          </select>
          <button className="btn" type="button" onClick={() => void load()} disabled={loading}>刷新</button>
        </div>
      </PageHeader>
      <div className="page-body">
        {error ? (
          <div className="table-state error page-error">
            <span>概览加载失败：{error}</span>
            <button className="btn small-btn" type="button" onClick={() => void load()}>重试</button>
          </div>
        ) : null}
        <div className="stats">
          {cards.map((card) => {
            const Icon = card.icon
            return (
              <Link className="stat" to={card.to} key={card.to}>
                <div className="stat-head">
                  <Icon size={20} />
                  <div className="label">{card.label}</div>
                </div>
                <div className="num">{card.num}</div>
                {card.hint ? <div className="hint">{card.hint}</div> : null}
              </Link>
            )
          })}
        </div>

        {analytics ? (
          <>
            <div className="stats analytics-totals">
              <div className="stat"><div className="stat-head"><div className="label">总用户</div></div><div className="num">{(totals?.total_users ?? 0).toLocaleString("zh-CN")}</div></div>
              <div className="stat"><div className="stat-head"><div className="label">累计下载</div></div><div className="num">{(totals?.total_downloads ?? 0).toLocaleString("zh-CN")}</div></div>
              <div className="stat"><div className="stat-head"><div className="label">近 7 天下载</div></div><div className="num">{(totals?.downloads_7d ?? 0).toLocaleString("zh-CN")}</div></div>
              <div className="stat"><div className="stat-head"><div className="label">近 30 天下载</div></div><div className="num">{(totals?.downloads_30d ?? 0).toLocaleString("zh-CN")}</div></div>
              <div className="stat"><div className="stat-head"><div className="label">新增用户近 7 天</div></div><div className="num">{(totals?.new_users_7d ?? 0).toLocaleString("zh-CN")}</div></div>
              <div className="stat"><div className="stat-head"><div className="label">新增用户近 30 天</div></div><div className="num">{(totals?.new_users_30d ?? 0).toLocaleString("zh-CN")}</div></div>
              <div className="stat"><div className="stat-head"><div className="label">资源</div></div><div className="num">{totals?.resources?.toLocaleString("zh-CN") ?? "—"}</div></div>
            </div>
            <div className="trend-grid">
              <TrendPanel title="用户增长" hint="按注册时间统计的新用户" points={analytics.user_growth || []} monthly={monthly} accent="var(--accent)" />
              <TrendPanel title="下载量" hint="下载事件按天/月聚合" points={analytics.downloads || []} monthly={monthly} accent="var(--ok)" />
            </div>
          </>
        ) : loading ? (
          <EmptyText />
        ) : null}
      </div>
    </>
  )
}

function EmptyText() {
  return <div className="empty">加载中…</div>
}