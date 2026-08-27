import { FormEvent, useEffect, useState } from "react";
import { replyTicket } from "../api";

type Ticket = {
  id: string;
  kind: string;
  subject: string;
  status: string;
  username?: string;
  message?: string;
};

export function TicketsPage() {
  const [items, setItems] = useState<Ticket[]>([]);
  const [q, setQ] = useState("");
  const [reply, setReply] = useState("");
  const [error, setError] = useState("");

  const load = (search = q) => {
    const url = search ? `/admin/api/tickets?q=${encodeURIComponent(search)}` : "/admin/api/tickets";
    fetch(url, { credentials: "same-origin" })
      .then(async (response) => {
        const data = await response.json().catch(() => ({}));
        if (response.status === 401) {
          window.location.href = "/admin/login";
          return;
        }
        if (!response.ok) throw new Error(data.message || "加载失败");
        setItems(data.items || []);
      })
      .catch((err: Error) => setError(err.message));
  };

  useEffect(() => {
    load("");
  }, []);

  const act = async (id: string, status: string) => {
    if (status === "replied" && !reply.trim()) {
      setError("回复不能为空");
      return;
    }
    setError("");
    try {
      await replyTicket(id, status, reply);
      setReply("");
      await load();
    } catch (err) {
      setError((err as Error).message);
    }
  };

  return (
    <>
      <header className="page-head">
        <h1>工单</h1>
        <form
          onSubmit={(event: FormEvent) => {
            event.preventDefault();
            load(q);
          }}
        >
          <input className="search" value={q} onChange={(event) => setQ(event.target.value)} placeholder="搜索工单" />
        </form>
      </header>
      <div className="table-wrap">
        {error && <div className="error">{error}</div>}
        <textarea className="search" value={reply} onChange={(event) => setReply(event.target.value)} placeholder="回复内容" style={{ margin: "0 12px 12px", minHeight: 72, width: "calc(100% - 24px)", maxWidth: 640 }} />
        {items.length === 0 && !error && <div className="empty">没有工单</div>}
        {items.length > 0 && (
          <table>
            <thead>
              <tr>
                <th>用户</th>
                <th>标题</th>
                <th>状态</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {items.map((item) => (
                <tr key={item.id}>
                  <td>{item.username || "—"}</td>
                  <td>
                    <div>{item.subject}</div>
                    {item.message && <small>{item.message}</small>}
                  </td>
                  <td>
                    <span className="badge">{item.status}</span>
                  </td>
                  <td>
                    <button className="btn btn-primary" type="button" onClick={() => act(item.id, "replied")}>
                      回复
                    </button>{" "}
                    <button className="btn" type="button" onClick={() => act(item.id, "resolved")}>
                      关闭
                    </button>
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
