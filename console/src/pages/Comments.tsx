import { useEffect, useState } from "react";
import { decideComment, listComments, type CommentItem } from "../api";

export function CommentsPage() {
  const [items, setItems] = useState<CommentItem[]>([]);
  const [error, setError] = useState("");
  const [note, setNote] = useState("");

  const load = () =>
    listComments("review")
      .then((data) => setItems(data.items || []))
      .catch((err: Error) => setError(err.message));

  useEffect(() => {
    load();
  }, []);

  const act = async (id: string, action: string) => {
    setError("");
    try {
      await decideComment(id, action, note);
      setNote("");
      await load();
    } catch (err) {
      setError((err as Error).message);
    }
  };

  return (
    <>
      <header className="page-head">
        <h1>评论</h1>
        <p>待人工处理</p>
      </header>
      <div className="table-wrap">
        {error && <div className="error">{error}</div>}
        <input className="search" value={note} onChange={(event) => setNote(event.target.value)} placeholder="隐藏时填写理由" style={{ margin: "0 12px 12px" }} />
        {items.length === 0 && <div className="empty">没有待处理的评论</div>}
        {items.length > 0 && (
          <table>
            <thead>
              <tr>
                <th>用户</th>
                <th>内容</th>
                <th>状态</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {items.map((item) => (
                <tr key={item.id}>
                  <td>{item.username}</td>
                  <td>{item.body}</td>
                  <td>
                    <span className="badge">{item.state}</span>
                  </td>
                  <td>
                    <button className="btn btn-primary" type="button" onClick={() => act(item.id, "approve")}>
                      通过
                    </button>{" "}
                    <button className="btn btn-danger" type="button" onClick={() => act(item.id, "hide")}>
                      隐藏
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
