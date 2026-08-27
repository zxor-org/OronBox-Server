import { useEffect, useState } from "react";
import { Pagination, SearchForm, Status, formatRelative } from "../ui";

type Column = { key: string; label: string };

function cell(row: Record<string, unknown>, key: string) {
  const value = row[key];
  if (value == null || value === "") return "—";
  if (key.endsWith("_at") && typeof value === "string") return formatRelative(value);
  if (key === "state" || key === "status" || key === "result") return <Status value={String(value)} />;
  if (typeof value === "string" || typeof value === "number" || typeof value === "boolean") return String(value);
  if (Array.isArray(value)) return value.join("、");
  return JSON.stringify(value);
}

export function ListPage({ title, hint, path, columns }: { title: string; hint?: string; path: string; columns: Column[] }) {
  const [items, setItems] = useState<Record<string, unknown>[]>([]);
  const [total, setTotal] = useState(0);
  const [q, setQ] = useState("");
  const [page, setPage] = useState(1);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);

  const load = (search = q, nextPage = page) => {
    const params = new URLSearchParams({ page: String(nextPage), per_page: "25" });
    if (search) params.set("q", search);
    const url = `${path}${path.includes("?") ? "&" : "?"}${params}`;
    setLoading(true);
    fetch(url, { credentials: "same-origin" })
      .then(async (response) => {
        const data = await response.json().catch(() => ({}));
        if (response.status === 401) {
          window.location.href = "/admin/login";
          return;
        }
        if (!response.ok) throw new Error(data.message || "加载失败");
        setItems(data.items || []);
        setTotal(data.total || 0);
      })
      .catch((err: Error) => setError(err.message))
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    load(q, page);
  }, [path, page]);

  return (
    <>
      <header className="page-head">
        <div>
          <h1>{title}</h1>
          {hint ? <p>{hint}</p> : null}
        </div>
        <SearchForm
          value={q}
          onChange={setQ}
          onSubmit={() => {
            setPage(1);
            load(q, 1);
          }}
          placeholder="搜索"
        />
      </header>
      <div className="table-wrap">
        {error && <div className="error">{error}</div>}
        {loading && <div className="empty">加载中…</div>}
        {!loading && items.length === 0 && !error && <div className="empty">没有数据</div>}
        {!loading && items.length > 0 && (
          <table>
            <thead>
              <tr>
                {columns.map((column) => (
                  <th key={column.key}>{column.label}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {items.map((row, index) => (
                <tr key={String(row.ID || row.id || index)}>
                  {columns.map((column) => (
                    <td key={column.key}>{cell(row, column.key)}</td>
                  ))}
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
