import { useEffect, useState } from "react";
import { useOutletContext } from "react-router";
import { retryPublication, type Session } from "../api";

type Item = { id: string; name: string; target: string; state: string; error: string };

export function PublicationsPage() {
  const session = useOutletContext<Session>();
  const [items, setItems] = useState<Item[]>([]);
  const [error, setError] = useState("");

  const load = () =>
    fetch("/admin/api/publications", { credentials: "same-origin" })
      .then(async (response) => {
        const data = await response.json();
        if (response.status === 401) {
          window.location.href = "/admin/login";
          return;
        }
        if (!response.ok) throw new Error(data.message || "加载失败");
        setItems(data.items || []);
      })
      .catch((err: Error) => setError(err.message));

  useEffect(() => {
    load();
  }, []);

  return (
    <>
      <header className="page-head">
        <h1>发布</h1>
      </header>
      <div className="table-wrap">
        {error && <div className="error">{error}</div>}
        {items.length === 0 && <div className="empty">没有发布任务</div>}
        {items.length > 0 && (
          <table>
            <thead>
              <tr>
                <th>资源</th>
                <th>目标</th>
                <th>状态</th>
                <th>错误</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {items.map((item) => (
                <tr key={item.id}>
                  <td>{item.name}</td>
                  <td>{item.target}</td>
                  <td>{item.state}</td>
                  <td>{item.error || "—"}</td>
                  <td>
                    {item.state === "failed" && session.role === "admin" && (
                      <button
                        className="btn"
                        type="button"
                        onClick={() => retryPublication(item.id).then(load).catch((err: Error) => setError(err.message))}
                      >
                        重试
                      </button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </>
  );
}
