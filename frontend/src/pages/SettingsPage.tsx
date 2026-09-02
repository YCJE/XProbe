import { useEffect, useState } from "react";
import { api, type CodesResp, type SessionsResp, type TagsResp } from "../lib/api";
import { Button, GlassCard, Input } from "../components/ui";
import { formatBytes } from "../lib/format";

type Tab = "agents" | "codes" | "tags" | "sessions";

/** 设置页(设计文档 6.6): Agent/注册码/标签/会话管理。 */
export function SettingsPage() {
  const [tab, setTab] = useState<Tab>("agents");
  const tabs: { key: Tab; label: string }[] = [
    { key: "agents", label: "Agent 与 Token" },
    { key: "codes", label: "注册码" },
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
      {tab === "agents" && <AgentsPanel />}
      {tab === "codes" && <CodesPanel />}
      {tab === "tags" && <TagsPanel />}
      {tab === "sessions" && <SessionsPanel />}
    </div>
  );
}

interface AgentRow {
  id: number; hostname: string; online: boolean; agent_version: string; token_mask: string;
}

function AgentsPanel() {
  const [rows, setRows] = useState<AgentRow[]>([]);
  const [newToken, setNewToken] = useState<{ id: number; token: string } | null>(null);

  const load = () => api.get<{ agents: AgentRow[] }>("/api/v1/agents/tokens").then((r) => setRows(r.agents));
  useEffect(() => { load(); }, []);

  const resetToken = async (id: number) => {
    if (!confirm("重置后旧 Token 立即失效, 需更新 Agent 配置。继续?")) return;
    const r = await api.post<{ token: string }>(`/api/v1/agents/${id}/reset-token`);
    setNewToken({ id, token: r.token });
    load();
  };

  return (
    <GlassCard>
      <table className="w-full text-sm">
        <thead><tr className="border-b border-card-border text-left text-xs text-muted">
          <th className="py-2">ID</th><th>主机名</th><th>在线</th><th>版本</th><th>Token</th><th />
        </tr></thead>
        <tbody>
          {rows.map((a) => (
            <tr key={a.id} className="border-b border-card-border/50 last:border-0">
              <td className="py-2 tnum">{a.id}</td>
              <td>{a.hostname}</td>
              <td className="tnum">{a.online ? "●" : "○"}</td>
              <td className="mono text-xs">{a.agent_version}</td>
              <td className="mono text-xs">{a.token_mask}</td>
              <td className="py-1 text-right">
                <Button variant="ghost" onClick={() => resetToken(a.id)}>重置 Token</Button>
              </td>
            </tr>
          ))}
          {rows.length === 0 && <tr><td colSpan={6} className="py-6 text-center text-xs text-muted">暂无 Agent</td></tr>}
        </tbody>
      </table>
      {newToken && (
        <p className="mt-3 rounded-lg p-3 text-xs" style={{ background: "var(--card-border)" }}>
          Agent #{newToken.id} 新 Token(仅显示一次, 立即保存):{" "}
          <code className="mono break-all">{newToken.token}</code>
        </p>
      )}
    </GlassCard>
  );
}

function CodesPanel() {
  const [codes, setCodes] = useState<CodesResp["codes"]>([]);
  const [created, setCreated] = useState("");
  const base = `${location.protocol}//${location.host}`;

  const load = () => api.get<CodesResp>("/api/v1/agents/register-codes").then((r) => setCodes(r.codes));
  useEffect(() => { load(); }, []);

  const create = async () => {
    const r = await api.post<{ code: string }>("/api/v1/agents/register-codes");
    setCreated(r.code);
    load();
  };

  const installCmd = (code: string) =>
    `curl -fsSL https://raw.githubusercontent.com/YCJE/XProbe/main/scripts/install-agent.sh | bash -s -- --server ${base} --code ${code}`;

  return (
    <GlassCard>
      <div className="mb-3 flex items-center justify-between">
        <p className="text-xs text-muted">注册码一次性使用, 15 分钟有效, 最多 5 个未使用</p>
        <Button onClick={create}>生成注册码</Button>
      </div>
      {created && (
        <div className="mb-3 rounded-lg p-3" style={{ background: "var(--card-border)" }}>
          <p className="text-xs text-muted">一键安装命令(仅显示一次):</p>
          <code className="mono break-all text-xs">{installCmd(created)}</code>
        </div>
      )}
      <table className="w-full text-sm">
        <thead><tr className="border-b border-card-border text-left text-xs text-muted">
          <th className="py-2">哈希</th><th>状态</th><th>过期时间</th><th />
        </tr></thead>
        <tbody>
          {codes.map((c) => (
            <tr key={c.hash} className="border-b border-card-border/50 last:border-0">
              <td className="mono py-2 text-xs">{c.hash.slice(0, 12)}…</td>
              <td className="text-xs">{c.used ? "已使用" : new Date(c.expires_at * 1000) > new Date() ? "有效" : "已过期"}</td>
              <td className="text-xs tnum">{new Date(c.expires_at * 1000).toLocaleString()}</td>
              <td className="text-right">
                {!c.used && (
                  <Button variant="ghost" onClick={async () => { await api.del(`/api/v1/agents/register-codes/${c.hash}`); load(); }}>
                    删除
                  </Button>
                )}
              </td>
            </tr>
          ))}
          {codes.length === 0 && <tr><td colSpan={4} className="py-6 text-center text-xs text-muted">暂无注册码</td></tr>}
        </tbody>
      </table>
    </GlassCard>
  );
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
