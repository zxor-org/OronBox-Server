import { useEffect, useState } from "react";
import { useNavigate } from "react-router";
import { listResources, type ResourceItem } from "../api";
import { PageHeader, Pagination, SearchForm, Status, TableState, TargetChips, formatRelative, kindLabel, stateLabel } from "../ui";

export function ResourcesPage() {
  const navigate = useNavigate();
  const [items, setItems] = useState<ResourceItem[]>([]);
  const [total, setTotal] = useState(0);
  const [q, setQ] = useState("");
  const [kind, setKind] = useState("");
  const [state, setState] = useState("");
  const [target, setTarget] = useState("");
  const [page, setPage] = useState(1);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);

  const load = (search = q, nextPage = page) => {
    setLoading(true);
    setError("");
    listResources(search, kind, state, nextPage, target)
      .then((data) => {
        setItems(data.items || []);
        setTotal(data.total || 0);
      })
      .catch((err: Error) => setError(err.message))
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    load(q, page);
  }, [kind, state, target, page]);

  return (
    <>
      <PageHeader title="全部资源" hint="点行进入工作区，发布目标来自当前版本的发布计划">
        <SearchForm
          value={q}
          onChange={setQ}
          onSubmit={() => {
            setPage(1);
            load(q, 1);
          }}
          placeholder="搜索名称或作者"
        />
      </PageHeader>
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
        <select value={target} onChange={(event) => { setTarget(event.target.value); setPage(1); }}>
          <option value="">全部目标</option>
          <option value="oronbox">OronBox</option>
          <option value="bandbbs">米坛</option>
          <option value="astrobox">AstroBox</option>
        </select>
      </div>
      <div className="table-wrap">
        <TableState loading={loading} error={error} onRetry={() => void load()} isEmpty={!items.length} empty={q ? "没有匹配的资源" : "没有资源"}>
          <table>
            <thead>
              <tr>
                <th>名称</th>
                <th>作者</th>
                <th>类型</th>
                <th>发布目标</th>
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
                  <td><TargetChips targets={(item.targets as string[]) || []} /></td>
                  <td>{item.revision_name || item.revision_number}</td>
                  <td>
                    <Status value={item.moderation || item.review_state || ""} label={stateLabel[item.moderation || ""] || stateLabel[item.review_state || ""] || item.moderation || item.review_state} />
                  </td>
                  <td>{formatRelative(item.updated_at)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </TableState>
      </div>
      <Pagination page={page} total={total} perPage={25} onChange={setPage} />
    </>
  );
}
