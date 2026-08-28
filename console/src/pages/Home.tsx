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

  useEffect(() => {
    api.get<Overview>("/admin/api/overview").then(setOverview).catch(() => setOverview({
      pending_reviews: session.pending_reviews,
      overdue_reviews: 0,
      pending_comments: 0,
      open_reports: 0,
      failed_publications: 0,
      pending_plugins: 0,
    }))
  }, [session.pending_reviews])

  const cards = [
    { to: "/review", num: overview?.pending_reviews ?? session.pending_reviews, label: "待审核", hint: overview?.overdue_reviews ? `${overview.overdue_reviews} 条已超时` : "", icon: ClipboardTextIcon },
    { to: "/comments", num: overview?.pending_comments ?? "…", label: "待处理评论", icon: ChatCircleIcon },
    { to: "/reports", num: overview?.open_reports ?? "…", label: "未关闭举报", icon: FlagIcon },
    { to: "/publications", num: overview?.failed_publications ?? "…", label: "失败的发布", icon: CloudArrowUpIcon },
    { to: "/plugins", num: overview?.pending_plugins ?? "…", label: "待审插件", icon: PuzzlePieceIcon },
    { to: "/resources", num: "→", label: "进入全部资源", icon: PackageIcon },
  ]

  return (
    <>
      <PageHeader hint={`你好，${session.user}。先处理超时审核和失败发布。`} />
      <div className="page-body">
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
