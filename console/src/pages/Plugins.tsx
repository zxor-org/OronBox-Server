import { FormEvent, useEffect, useState } from "react";
import { reviewPlugin } from "../api";

type Plugin = {
  id: string;
  name: string;
  state: string;
  version?: string;
  author?: string;
  pending_version_id?: string;
  description?: string;
};

export function PluginsPage() {
  const [items, setItems] = useState<Plugin[]>([]);
  const [q, setQ] = useState("");
  const [note, setNote] = useState("");
  const [error, setError] = useState("");

  const load = (search = q) => {
    const params = new URLSearchParams();
    if (search) params.set("q", search);
    const query = params.toString();
    fetch(`/admin/api/plugins${query ? `?${query}` : ""}`, { credentials: "same-origin" })
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

  const act = async (id: string, decision: "approve" | "reject") => {
    if (decision === "reject" && !note.trim()) {
      setError("退回必须填写理由");
      return;
    }
    setError("");
    try {
      await reviewPlugin(id, decision, note);
      setNote("");
      await load();
    } catch (err) {
      setError((err as Error).message);
    }
  };

  return (
    <>
      <header className="page-head">
        <h1>插件</h1>
        <form
          onSubmit={(event: FormEvent) => {
            event.preventDefault();
            load(q);
          }}
        >
          <input className="search" value={q} onChange={(event) => setQ(event.target.value)} placeholder="搜索插件" />
        </form>
      </header>
      <div className="table-wrap">
        {error && <div className="error">{error}</div>}
        <input className="search" value={note} onChange={(event) => setNote(event.target.value)} placeholder="退回理由" style={{ margin: "0 12px 12px" }} />
        {items.length === 0 && !error && <div className="empty">没有插件</div>}
        {items.length > 0 && (
          <table>
            <thead>
              <tr>
                <th>名称</th>
                <th>作者</th>
                <th>状态</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {items.map((item) => (
                <tr key={item.id}>
                  <td>
                    <div>{item.name}</div>
                    {item.description && <small>{item.description}</small>}
                  </td>
                  <td>{item.author || "—"}</td>
                  <td>
                    <span className="badge">{item.state}</span>
                  </td>
                  <td>
                    {item.pending_version_id && (
                      <>
                        <button className="btn btn-primary" type="button" onClick={() => act(item.id, "approve")}>
                          通过
                        </button>{" "}
                        <button className="btn btn-danger" type="button" onClick={() => act(item.id, "reject")}>
                          退回
                        </button>
                      </>
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
