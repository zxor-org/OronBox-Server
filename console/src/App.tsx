import { NavLink, Outlet, Route, Routes, useLocation } from "react-router"
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
import { applyTheme, readTheme, type Theme } from "./theme"

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

const titles: Record<string, string> = {
  "": "概览",
  review: "待审核",
  resources: "全部资源",
  comments: "评论审核",
  collections: "合集",
  "collections/review": "合集审核",
  publications: "发布任务",
  users: "用户",
  reports: "举报与反馈",
  devices: "设备目录",
  plugins: "插件管理",
  coins: "硬币管理",
  messages: "系统消息",
  home: "首页编排",
  blog: "Blog 管理",
  announcements: "公告",
  releases: "客户端版本",
  "oauth/events": "OAuth 事件",
  "oauth/states": "OAuth States",
  "oauth/tickets": "登录 Tickets",
  clients: "客户端统计",
  "storage/blobs": "Blob 与副本",
  health: "运行状态",
  audit: "审计日志",
  settings: "设置",
}

function Shell({ session }: { session: Session }) {
  const admin = session.role === "admin"
  const [theme, setTheme] = useState<Theme>(() => readTheme())
  const location = useLocation()
  const crumbs = location.pathname.replace(/^\//, "").split("/").filter(Boolean)
  const keys = crumbs.length ? crumbs.map((_, index) => crumbs.slice(0, index + 1).join("/")) : [""]
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
            onClick={() => {
              const next = theme === "dark" ? "light" : "dark"
              applyTheme(next)
              setTheme(next)
            }}
          >
            {theme === "dark" ? "日间" : "夜间"}
          </button>
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
        <header className="topbar">
          {keys.map((key, index) => {
            const last = index === keys.length - 1
            const label = titles[key] || crumbs[index] || "概览"
            return (
              <span key={key || "home"} className="crumb">
                {index > 0 ? <span className="crumb-sep">/</span> : null}
                {last ? <span className="current">{label}</span> : <NavLink to={key ? `/${key}` : "/"}>{label}</NavLink>}
              </span>
            )
          })}
        </header>
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
