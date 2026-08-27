import { useEffect, useState } from "react";
import { loadSession } from "../api";

type Member = { id: string; name: string; slug: string; owner: string; representative?: boolean };
type Pending = {
  id: string;
  slug: string;
  pending_revision?: { id: string; name: string; summary: string };
  members?: Member[];
  representative_name?: string;
};

export function CollectionReviewPage() {
  const [items, setItems] = useState<Pending[]>([]);
  const [selected, setSelected] = useState("");
  const [error, setError] = useState("");
  const [note, setNote] = useState("");
  const [csrf, setCsrf] = useState("");
  const [rejecting, setRejecting] = useState(false);

  const load = () =>
    fetch("/admin/api/collections/review", { credentials: "same-origin" })
      .then(async (response) => {
        const data = await response.json();
        if (response.status === 401) {
          window.location.href = "/admin/login";
          return;
        }
        if (!response.ok) throw new Error(data.message || "加载失败");
        const next: Pending[] = data.collections || data.items || [];
        setItems(next);
        setSelected((current) => {
          if (current && next.some((item) => item.id === current)) return current;
          return next[0]?.id || "";
        });
      })
      .catch((err: Error) => setError(err.message));

  useEffect(() => {
    loadSession()
      .then((session) => {
        setCsrf(session.csrf_token);
        return load();
      })
      .catch((err: Error) => setError(err.message));
  }, []);

  const current = items.find((item) => item.id === selected);

  const decide = async (revisionId: string, approve: boolean) => {
    if (!approve && !note.trim()) {
      setError("退回必须填写理由");
      return;
    }
    setError("");
    try {
      const response = await fetch(`/admin/api/collections/review/${revisionId}`, {
        method: "POST",
        credentials: "same-origin",
        headers: { "Content-Type": "application/json", "X-CSRF-Token": csrf },
        body: JSON.stringify({ approve, note }),
      });
      const data = await response.json().catch(() => ({}));
      if (!response.ok) throw new Error(data.message || "失败");
      setNote("");
      setRejecting(false);
      await load();
    } catch (err) {
      setError((err as Error).message);
    }
  };

  return (
    <>
      <header className="page-head">
        <h1>合集审核</h1>
      </header>
      <div className="split">
        <div className="queue">
          {items.length === 0 && <div className="empty">没有待审合集</div>}
          {items.map((item) => (
            <button
              key={item.id}
              type="button"
              className={`queue-item ${item.id === selected ? "selected" : ""}`}
              onClick={() => setSelected(item.id)}
            >
              <div className="title">{item.pending_revision?.name || item.slug}</div>
              <div className="meta">{item.members?.length ?? 0} 个成员</div>
            </button>
          ))}
        </div>
        <div className="detail">
          {!current && <div className="detail-empty">从左边选一条</div>}
          {current && (
            <>
              <h2 className="detail-title">{current.pending_revision?.name || current.slug}</h2>
              {current.pending_revision?.summary && <p className="summary">{current.pending_revision.summary}</p>}
              {current.representative_name && <p className="detail-meta">代表作：{current.representative_name}</p>}
              <div className="files">
                {(current.members || []).map((member) => (
                  <div className="file" key={member.id}>
                    <div>
                      <div>
                        {member.name}
                        {member.representative ? " · 代表作" : ""}
                      </div>
                      <small>
                        {member.owner} · {member.slug}
                      </small>
                    </div>
                  </div>
                ))}
                {(!current.members || current.members.length === 0) && <div className="empty">没有成员</div>}
              </div>
              {error && <div className="error">{error}</div>}
              <div className="actions">
                {current.pending_revision?.id && (
                  <>
                    <button className="btn btn-primary" type="button" onClick={() => decide(current.pending_revision!.id, true)}>
                      通过
                    </button>
                    <button className="btn btn-danger" type="button" onClick={() => setRejecting(true)}>
                      退回
                    </button>
                  </>
                )}
              </div>
            </>
          )}
        </div>
      </div>
      {rejecting && current?.pending_revision?.id && (
        <div className="modal-back" onClick={() => setRejecting(false)}>
          <div className="modal" onClick={(event) => event.stopPropagation()}>
            <h2>退回这一版</h2>
            <p className="detail-meta">作者会看到这段话</p>
            <textarea value={note} onChange={(event) => setNote(event.target.value)} placeholder="需要改什么" />
            <div className="actions">
              <button className="btn" onClick={() => setRejecting(false)}>
                取消
              </button>
              <button className="btn btn-danger" disabled={!note.trim()} onClick={() => decide(current.pending_revision!.id, false)}>
                退回
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
