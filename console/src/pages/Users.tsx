import { FormEvent, useEffect, useState } from "react";
import { setUserState } from "../api";

type User = {
  id: string;
  username: string;
  role: string;
  resource_count: number;
  banned?: boolean;
  frozen?: boolean;
  ban_reason?: string;
};

export function UsersPage() {
  const [items, setItems] = useState<User[]>([]);
  const [q, setQ] = useState("");
  const [reason, setReason] = useState("");
  const [error, setError] = useState("");

  const load = (search = q) => {
    const url = search ? `/admin/api/users?q=${encodeURIComponent(search)}` : "/admin/api/users";
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

  const act = async (id: string, action: string, role = "") => {
    setError("");
    try {
      await setUserState(id, action, reason, role);
      setReason("");
      await load();
    } catch (err) {
      setError((err as Error).message);
    }
  };

  return (
    <>
      <header className="page-head">
        <h1>用户</h1>
        <form
          onSubmit={(event: FormEvent) => {
            event.preventDefault();
            load(q);
          }}
        >
          <input className="search" value={q} onChange={(event) => setQ(event.target.value)} placeholder="搜索用户" />
        </form>
      </header>
      <div className="table-wrap">
        {error && <div className="error">{error}</div>}
        <input className="search" value={reason} onChange={(event) => setReason(event.target.value)} placeholder="封禁/冻结理由" style={{ margin: "0 12px 12px" }} />
        {items.length === 0 && !error && <div className="empty">没有用户</div>}
        {items.length > 0 && (
          <table>
            <thead>
              <tr>
                <th>用户</th>
                <th>角色</th>
                <th>资源</th>
                <th>状态</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {items.map((item) => (
                <tr key={item.id}>
                  <td>{item.username}</td>
                  <td>{item.role}</td>
                  <td>{item.resource_count}</td>
                  <td>
                    {item.banned ? "已封禁" : item.frozen ? "创作者已冻结" : "正常"}
                  </td>
                  <td>
                    {item.banned ? (
                      <button className="btn" type="button" onClick={() => act(item.id, "unban")}>
                        解封
                      </button>
                    ) : (
                      <button className="btn btn-danger" type="button" onClick={() => act(item.id, "ban")}>
                        封禁
                      </button>
                    )}{" "}
                    {item.frozen ? (
                      <button className="btn" type="button" onClick={() => act(item.id, "unfreeze_creator")}>
                        解冻
                      </button>
                    ) : (
                      <button className="btn" type="button" onClick={() => act(item.id, "freeze_creator")}>
                        冻结创作
                      </button>
                    )}{" "}
                    {item.role !== "reviewer" && (
                      <button className="btn" type="button" onClick={() => act(item.id, "set_role", "reviewer")}>
                        设为审核员
                      </button>
                    )}
                    {item.role === "reviewer" && (
                      <button className="btn" type="button" onClick={() => act(item.id, "set_role", "user")}>
                        取消审核员
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
