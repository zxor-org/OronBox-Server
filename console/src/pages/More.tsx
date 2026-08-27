import { Link } from "react-router";

export function MorePage({ admin }: { admin: boolean }) {
  return (
    <>
      <header className="page-head">
        <h1>更多</h1>
      </header>
      <div className="home">
        <div className="cards">
          <Link className="card" to="/tickets">
            <div className="label">工单</div>
          </Link>
          {admin && (
            <Link className="card" to="/coins">
              <div className="label">硬币</div>
            </Link>
          )}
          <Link className="card" to="/devices">
            <div className="label">设备</div>
          </Link>
          <Link className="card" to="/plugins">
            <div className="label">插件</div>
          </Link>
          {admin && (
            <Link className="card" to="/announcements">
              <div className="label">公告</div>
            </Link>
          )}
          <Link className="card" to="/health">
            <div className="label">状态</div>
          </Link>
          <Link className="card" to="/audit">
            <div className="label">审计</div>
          </Link>
          <Link className="card" to="/messages">
            <div className="label">消息</div>
          </Link>
          <Link className="card" to="/releases">
            <div className="label">版本</div>
          </Link>
          {admin && (
            <Link className="card" to="/blog">
              <div className="label">Blog</div>
            </Link>
          )}
          {admin && (
            <Link className="card" to="/settings">
              <div className="label">设置</div>
            </Link>
          )}
        </div>
      </div>
    </>
  );
}
