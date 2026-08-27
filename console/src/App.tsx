import { NavLink, Outlet, Route, Routes } from "react-router"
import { useEffect, useState } from "react"
import { loadSession, logout, type Session } from "./api"
import { ReviewPage } from "./pages/Review"
import { ResourcesPage } from "./pages/Resources"
import { ResourcePage } from "./pages/Resource"
import { CommentsPage } from "./pages/Comments"
import { HomePage } from "./pages/Home"
import { PublicationsPage } from "./pages/Publications"
import { CollectionReviewPage } from "./pages/CollectionReview"
import { UsersPage } from "./pages/Users"
import { PluginsPage } from "./pages/Plugins"
import {
  AnnouncementsPage,
  BlogPage,
  CoinsPage,
  CollectionsPage,
  DevicesPage,
  HomeComposerPage,
  MessagesPage,
  ReleasesPage,
  ReportsPage,
  SystemPage,
} from "./pages/System"
import { ToastHost } from "./ui"

type NavItem = { to: string; label: string; admin?: boolean; end?: boolean; badge?: boolean }

const groups: { label: string; items: NavItem[] }[] = [
  {
    label: "审核工作台",
    items: [
      { to: "/review", label: "待审核", badge: true },
      { to: "/collections/review", label: "合集审核" },
      { to: "/comments", label: "评论审核" },
      { to: "/reports", label: "举报与反馈" },
    ],
  },
  {
    label: "内容与发布",
    items: [
      { to: "/", label: "概览", admin: true, end: true },
      { to: "/resources", label: "全部资源" },
      { to: "/publications", label: "发布任务" },
      { to: "/devices", label: "设备目录" },
      { to: "/plugins", label: "插件管理" },
    ],
  },
  {
    label: "社区与用户",
    items: [
      { to: "/users", label: "用户", admin: true },
      { to: "/coins", label: "硬币管理", admin: true },
      { to: "/messages", label: "系统消息" },
    ],
  },
  {
    label: "内容运营",
    items: [
      { to: "/home", label: "首页编排" },
      { to: "/blog", label: "Blog 管理", admin: true },
      { to: "/announcements", label: "公告", admin: true },
      { to: "/releases", label: "客户端版本" },
    ],
  },
  {
    label: "系统与诊断",
    items: [
      { to: "/oauth/events", label: "OAuth 事件" },
      { to: "/oauth/states", label: "OAuth States" },
      { to: "/oauth/tickets", label: "登录 Tickets" },
      { to: "/clients", label: "客户端统计" },
      { to: "/storage/blobs", label: "Blob 与副本" },
      { to: "/health", label: "运行状态" },
      { to: "/audit", label: "审计日志" },
      { to: "/settings", label: "设置", admin: true },
    ],
  },
]

function Shell({ session }: { session: Session }) {
  const admin = session.role === "admin"
  return (
    <div className="shell">
      <aside className="sidebar">
        <div className="brand">
          OronBox<span>管理</span>
        </div>
        <nav className="nav">
          {groups.map((group) => {
            const items = group.items.filter((item) => !item.admin || admin)
            if (!items.length) return null
            return (
              <div className="nav-group" key={group.label}>
                <h2>{group.label}</h2>
                {items.map((item) => (
                  <NavLink key={item.to} to={item.to} end={item.end}>
                    {item.label}
                    {item.badge && session.pending_reviews > 0 ? <span className="count">{session.pending_reviews}</span> : null}
                  </NavLink>
                ))}
              </div>
            )
          })}
        </nav>
        <div className="sidebar-foot">
          <span>{session.user}</span>
          <button
            type="button"
            onClick={async () => {
              await logout()
              window.location.href = "/admin/login"
            }}
          >
            退出
          </button>
        </div>
      </aside>
      <div className="main">
        <Outlet context={session} />
      </div>
      <ToastHost />
    </div>
  )
}

export function App() {
  const [session, setSession] = useState<Session | null>(null)
  const [error, setError] = useState("")

  useEffect(() => {
    loadSession()
      .then(setSession)
      .catch((err: Error) => {
        if (err.message !== "unauthenticated") setError(err.message)
      })
  }, [])

  if (error) return <div className="empty">{error}</div>
  if (!session) return <div className="empty">加载中…</div>

  return (
    <Routes>
      <Route element={<Shell session={session} />}>
        <Route index element={session.role === "admin" ? <HomePage session={session} /> : <ReviewPage />} />
        <Route path="review" element={<ReviewPage />} />
        <Route path="review/:id" element={<ReviewPage />} />
        <Route path="resources" element={<ResourcesPage />} />
        <Route path="resources/:id" element={<ResourcePage />} />
        <Route path="comments" element={<CommentsPage />} />
        <Route path="collections" element={<CollectionsPage />} />
        <Route path="collections/review" element={<CollectionReviewPage />} />
        <Route path="collections/:id" element={<CollectionsPage />} />
        <Route path="publications" element={<PublicationsPage />} />
        <Route path="users" element={session.role === "admin" ? <UsersPage /> : <ReviewPage />} />
        <Route path="users/:id" element={session.role === "admin" ? <UsersPage /> : <ReviewPage />} />
        <Route path="reports" element={<ReportsPage />} />
        <Route path="feedback" element={<ReportsPage />} />
        <Route path="devices" element={<DevicesPage />} />
        <Route path="plugins" element={<PluginsPage />} />
        <Route path="plugins/:id" element={<PluginsPage />} />
        <Route path="coins" element={<CoinsPage />} />
        <Route path="messages" element={<MessagesPage />} />
        <Route path="home" element={<HomeComposerPage />} />
        <Route path="blog" element={<BlogPage />} />
        <Route path="blog/:slug" element={<BlogPage />} />
        <Route path="announcements" element={<AnnouncementsPage />} />
        <Route path="releases" element={<ReleasesPage />} />
        <Route path="oauth/:kind" element={<SystemPage />} />
        <Route path="clients" element={<SystemPage />} />
        <Route path="storage/blobs" element={<SystemPage />} />
        <Route path="health" element={<SystemPage />} />
        <Route path="audit" element={<SystemPage />} />
        <Route path="settings" element={<SystemPage />} />
      </Route>
    </Routes>
  )
}
