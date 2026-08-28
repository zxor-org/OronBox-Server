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

export function HomePage({ session }: { session: Session }) {
  const [overview, setOverview] = useState<Overview | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState("")

  const load = async () => {
    setLoading(true)
    setError("")
    try {
      setOverview(await api.get<Overview>("/admin/api/overview"))
    } catch (err) {
      setOverview(null)
      setError((err as Error).message)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void load()
  }, [session.pending_reviews])

  const number = (value: number, fallback = "—") => overview ? value : loading ? "…" : fallback
  const cards = [
    { to: "/review", num: overview?.pending_reviews ?? (loading ? session.pending_reviews : "—"), label: "待审核", hint: overview?.overdue_reviews ? `${overview.overdue_reviews} 条已超时` : "", icon: ClipboardTextIcon },
    { to: "/comments", num: number(overview?.pending_comments || 0), label: "待处理评论", icon: ChatCircleIcon },
    { to: "/reports", num: number(overview?.open_reports || 0), label: "未关闭举报", icon: FlagIcon },
    { to: "/publications", num: number(overview?.failed_publications || 0), label: "失败的发布", icon: CloudArrowUpIcon },
    { to: "/plugins", num: number(overview?.pending_plugins || 0), label: "待审插件", icon: PuzzlePieceIcon },
    { to: "/resources", num: "→", label: "进入全部资源", icon: PackageIcon },
  ]

  return (
    <>
      <PageHeader title="概览" hint={`你好，${session.user}。先处理超时审核和失败发布`}>
        <button className="btn" type="button" onClick={() => void load()} disabled={loading}>刷新</button>
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
      </div>
    </>
  )
}
