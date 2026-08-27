import { useEffect, useState } from "react";
import { useNavigate } from "react-router";
import { listResources, type ResourceItem } from "../api";
import { Pagination, SearchForm, Status, formatRelative } from "../ui";

const kindLabel: Record<string, string> = { watchface: "表盘", app: "应用", quickapp: "快应用" };
const stateLabel: Record<string, string> = {
  visible: "展示中",
  hidden: "已隐藏",
  frozen: "已冻结",
  pending: "待审核",
  approved: "已通过",
  rejected: "已退回",
};

export function ResourcesPage() {
  const navigate = useNavigate();
  const [items, setItems] = useState<ResourceItem[]>([]);
  const [total, setTotal] = useState(0);
  const [q, setQ] = useState("");
  const [kind, setKind] = useState("");
  const [state, setState] = useState("");
  const [page, setPage] = useState(1);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);

  const load = (search = q, nextPage = page) => {
    setLoading(true);
    listResources(search, kind, state, nextPage)
      .then((data) => {
        setItems(data.items || []);
        setTotal(data.total || 0);
      })
      .catch((err: Error) => setError(err.message))
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    load(q, page);
  }, [kind, state, page]);

  return (
    <>
      <header className="page-head">
        <h1>资源</h1>
        <SearchForm
          value={q}
          onChange={setQ}
          onSubmit={() => {
            setPage(1);
            load(q, 1);
          }}
          placeholder="搜索名称或作者"
        />
      </header>
      <div className="toolbar">
        <select value={kind} onChange={(event) => { setKind(event.target.value); setPage(1); }}>
          <option value="">全部类型</option>
          <option value="watchface">表盘</option>
          <option value="app">应用</option>
          <option value="quickapp">快应用</option>
        </select>
        <select value={state} onChange={(event) => { setState(event.target.value); setPage(1); }}>
          <option value="">全部状态</option>
          <option value="visible">展示中</option>
          <option value="hidden">已隐藏</option>
          <option value="frozen">已冻结</option>
          <option value="pending">待审核</option>
        </select>
      </div>
      <div className="table-wrap">
        {error && <div className="error">{error}</div>}
        {loading && <div className="empty">加载中…</div>}
        {!loading && items.length === 0 && <div className="empty">没有匹配的资源</div>}
        {!loading && items.length > 0 && (
          <table>
            <thead>
              <tr>
                <th>名称</th>
                <th>作者</th>
                <th>类型</th>
                <th>当前版本</th>
                <th>状态</th>
                <th>更新</th>
              </tr>
            </thead>
            <tbody>
              {items.map((item) => (
                <tr key={item.id} className="clickable" onClick={() => navigate(`/resources/${item.id}`)}>
                  <td>{item.name || item.revision_name || item.slug}</td>
                  <td>{item.owner}</td>
                  <td>{kindLabel[item.kind || ""] || item.kind}</td>
                  <td>{item.revision_name || item.revision_number}</td>
                  <td>
                    <Status value={item.moderation || item.review_state || ""} label={stateLabel[item.moderation || ""] || stateLabel[item.review_state || ""] || item.moderation || item.review_state} />
                  </td>
                  <td>{formatRelative(item.updated_at)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
      <Pagination page={page} total={total} perPage={25} onChange={setPage} />
    </>
  );
}
