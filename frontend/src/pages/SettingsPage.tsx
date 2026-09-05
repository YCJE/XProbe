import { useEffect, useState } from "react";
import { api, type CodesResp, type SessionsResp, type TagsResp } from "../lib/api";
import { Button, GlassCard, Input } from "../components/ui";
import { formatBytes } from "../lib/format";

type Tab = "tags" | "sessions";

/** 设置页(设计文档 6.6): Agent/注册码/标签/会话管理。 */
export function SettingsPage() {
  const [tab, setTab] = useState<Tab>("tags");
  const tabs: { key: Tab; label: string }[] = [
    { key: "tags", label: "标签" },
    { key: "sessions", label: "登录会话" },
  ];
  return (
    <div className="mx-auto max-w-5xl px-4 py-6">
      <div className="mb-4 flex gap-1">
        {tabs.map((t) => (
          <Button key={t.key} variant={tab === t.key ? "primary" : "ghost"} onClick={() => setTab(t.key)}>
            {t.label}
          </Button>
        ))}
      </div>
      {tab === "tags" && <TagsPanel />}
      {tab === "sessions" && <SessionsPanel />}
    </div>
  );
}

interface AgentRow {
  id: number; hostname: string; online: boolean; agent_version: string; token_mask: string;
}



function TagsPanel() {
  const [tags, setTags] = useState<TagsResp["tags"]>([]);
  const [name, setName] = useState("");
  const load = () => api.get<TagsResp>("/api/v1/tags").then((r) => setTags(r.tags));
  useEffect(() => { load(); }, []);

  return (
    <GlassCard>
      <div className="mb-3 flex gap-2">
        <Input placeholder="标签名" value={name} onChange={(e) => setName(e.target.value)} className="w-48" />
        <Button onClick={async () => { if (name) { await api.post("/api/v1/tags", { name }); setName(""); load(); } }}>
          添加
        </Button>
      </div>
      <div className="flex flex-wrap gap-2">
        {tags.map((t) => (
          <span key={t.id} className="flex items-center gap-2 rounded-lg px-3 py-1.5 text-sm" style={{ background: "var(--card-border)" }}>
            {t.name}
            <button
              className="text-xs text-muted"
              onClick={async () => { if (confirm(`删除标签「${t.name}」?`)) { await api.del(`/api/v1/tags/${t.id}`); load(); } }}
            >
              ✕
            </button>
          </span>
        ))}
        {tags.length === 0 && <p className="py-4 text-xs text-muted">暂无标签</p>}
      </div>
    </GlassCard>
  );
}

function SessionsPanel() {
  const [sessions, setSessions] = useState<SessionsResp["sessions"]>([]);
  const load = () => api.get<SessionsResp>("/api/v1/auth/sessions").then((r) => setSessions(r.sessions));
  useEffect(() => { load(); }, []);

  return (
    <GlassCard>
      <div className="mb-3 flex justify-between">
        <p className="text-xs text-muted">当前登录的全部设备会话</p>
        <Button variant="danger" onClick={async () => {
          if (confirm("吊销全部会话并登出所有设备?")) { await api.del("/api/v1/auth/sessions"); location.reload(); }
        }}>登出所有设备</Button>
      </div>
      <table className="w-full text-sm">
        <thead><tr className="border-b border-card-border text-left text-xs text-muted">
          <th className="py-2">登录时间</th><th>过期</th><th>IP</th><th>设备</th><th />
        </tr></thead>
        <tbody>
          {sessions.map((s) => (
            <tr key={s.id} className="border-b border-card-border/50 last:border-0">
              <td className="py-2 text-xs tnum">{new Date(s.created_at).toLocaleString()}</td>
              <td className="text-xs tnum">{new Date(s.expires_at).toLocaleString()}</td>
              <td className="mono text-xs">{s.ip || "--"}</td>
              <td className="max-w-[240px] truncate text-xs text-muted">{s.user_agent || "--"}</td>
              <td className="text-right">
                {s.current ? (
                  <span className="text-xs" style={{ color: "var(--success)" }}>当前</span>
                ) : (
                  <Button variant="ghost" onClick={async () => { await api.del(`/api/v1/auth/sessions/${s.id}`); load(); }}>吊销</Button>
                )}
              </td>
            </tr>
          ))}
          {sessions.length === 0 && <tr><td colSpan={5} className="py-6 text-center text-xs text-muted">无活跃会话</td></tr>}
        </tbody>
      </table>
      <p className="mt-3 text-xs text-muted">忘记密码: 服务器执行 <code className="mono">xprobe-server reset-password --username admin</code>(需 tput 交互输入)</p>
      <p className="mt-1 text-xs text-muted">占用存储说明: 会话仅存哈希(S9)· 数据库大小 {formatBytes(0)} 由系统状态页展示</p>
    </GlassCard>
  );
}
