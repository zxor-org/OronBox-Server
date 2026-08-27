import { FormEvent, useEffect, useRef, useState } from "react";
import { useNavigate, useParams } from "react-router";
import { getResource, saveResourceDraft, submitResourceDraft } from "../api";

export function ResourceEditPage() {
  const { id } = useParams();
  const navigate = useNavigate();
  const [name, setName] = useState("");
  const [summary, setSummary] = useState("");
  const [paidType, setPaidType] = useState("free");
  const [revisionId, setRevisionId] = useState("");
  const [attributes, setAttributes] = useState<string[]>([]);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [canSubmit, setCanSubmit] = useState(false);
  const [pending, setPending] = useState(false);
  const [loadedId, setLoadedId] = useState("");
  const loadGen = useRef(0);

  useEffect(() => {
    if (!id) return;
    loadGen.current += 1;
    const gen = loadGen.current;
    let cancelled = false;
    setLoadedId("");
    setName("");
    setSummary("");
    setPaidType("free");
    setRevisionId("");
    setAttributes([]);
    setCanSubmit(false);
    setPending(false);
    setError("");
    getResource(id)
      .then((data) => {
        if (cancelled || loadGen.current !== gen) return;
        setName(data.name || "");
        setSummary(data.summary || "");
        setPaidType(data.paid_type || "free");
        setRevisionId(data.revision_id || "");
        setAttributes(data.attributes || []);
        setCanSubmit(!!data.can_submit);
        setPending(!!data.pending);
        setLoadedId(id);
      })
      .catch((err: Error) => {
        if (!cancelled) setError(err.message);
      });
    return () => {
      cancelled = true;
    };
  }, [id]);

  const save = async (submit: boolean) => {
    if (!id || loadedId !== id) return;
    const gen = loadGen.current;
    setBusy(true);
    setError("");
    try {
      const saved = await saveResourceDraft(id, {
        name,
        summary,
        paid_type: paidType,
        revision_id: revisionId,
        attributes,
      });
      if (loadGen.current !== gen) return;
      if (saved.revision_id) setRevisionId(saved.revision_id);
      if (!pending) setCanSubmit(true);
      if (submit) {
        await submitResourceDraft(id, saved.revision_id || revisionId);
        if (loadGen.current !== gen) return;
        navigate("/review");
        return;
      }
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <header className="page-head">
        <h1>编辑资源</h1>
      </header>
      <form
        className="home"
        onSubmit={(event: FormEvent) => {
          event.preventDefault();
          save(false);
        }}
      >
        <label>
          名称
          <input className="search" value={name} onChange={(event) => setName(event.target.value)} style={{ display: "block", marginTop: 6, width: "100%", maxWidth: 480 }} />
        </label>
        <label style={{ display: "block", marginTop: 16 }}>
          简介
          <textarea className="search" value={summary} onChange={(event) => setSummary(event.target.value)} style={{ display: "block", marginTop: 6, width: "100%", maxWidth: 640, minHeight: 120 }} />
        </label>
        <label style={{ display: "block", marginTop: 16 }}>
          付费
          <select className="search" value={paidType} onChange={(event) => setPaidType(event.target.value)} style={{ display: "block", marginTop: 6 }}>
            <option value="free">免费</option>
            <option value="paid">付费</option>
            <option value="force_paid">强制付费</option>
          </select>
        </label>
        {pending && <p className="detail-meta">这一版正在审核，保存会改待审内容，不能再提交。</p>}
        {error && <div className="error">{error}</div>}
        <div className="actions" style={{ marginTop: 20 }}>
          <button className="btn" disabled={busy || loadedId !== id} type="submit">
            保存草稿
          </button>
          {canSubmit && (
            <button className="btn btn-primary" disabled={busy || loadedId !== id} type="button" onClick={() => save(true)}>
              提交审核
            </button>
          )}
        </div>
      </form>
    </>
  );
}
