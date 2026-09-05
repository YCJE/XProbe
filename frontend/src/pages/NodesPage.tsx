import { useEffect, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useNavigate } from "react-router-dom";
import { api } from "../lib/api";
import type { ServerInfo, Tag } from "../lib/types";
import { Button, Empty, GlassCard, Input, StatusDot } from "../components/ui";

/** 服务器页(Komari 模式): 预创建节点 → 拿安装命令 → Agent 注册自动绑定。 */
export function NodesPage() {
  const nav = useNavigate();
  const [servers, setServers] = useState<ServerInfo[]>([]);
  const [tags, setTags] = useState<Tag[]>([]);
  const [addOpen, setAddOpen] = useState(false);
  const [cmdFor, setCmdFor] = useState<{ id: number; code: string; command: string } | null>(null);

  const load = () => {
    api.get<{ servers: ServerInfo[] }>("/api/v1/servers").then((r) => setServers(r.servers ?? []));
    api.get<{ tags: Tag[] }>("/api/v1/tags").then((r) => setTags(r.tags ?? [])).catch(() => undefined);
  };
  useEffect(() => { load(); }, []);

  const base = `${location.protocol}//${location.host}`;

  return (
    <div className="mx-auto max-w-6xl px-4 py-6">
      <div className="mb-4 flex items-center justify-between">
        <h1 className="text-lg font-semibold">服务器</h1>
        <Button onClick={() => setAddOpen(true)}>+ 添加节点</Button>
      </div>

      <GlassCard className="overflow-x-auto">
        <table className="w-full min-w-[860px] text-sm">
          <thead>
            <tr className="border-b border-card-border text-left text-xs text-muted">
              <th className="px-4 py-3">名称</th>
              <th className="px-2 py-3">IP 地址</th>
              <th className="px-2 py-3">客户端版本</th>
              <th className="px-2 py-3">备注</th>
              <th className="px-4 py-3 text-right">操作</th>
            </tr>
          </thead>
          <tbody>
            {servers.map((s) => (
              <tr key={s.id} className="border-b border-card-border/50 last:border-0 hover:bg-card">
                <td className="px-4 py-3">
                  <span className="flex items-center gap-2">
                    <StatusDot online={s.online} />
                    <button className="font-medium text-link" onClick={() => nav(`/server/${s.id}`)}>
                      {s.display_name || s.hostname}
                    </button>
                  </span>
                </td>
                <td className="px-2 py-3 mono text-xs">{s.ipv4 || s.ipv6 || <span className="text-muted">待安装</span>}</td>
                <td className="px-2 py-3 mono text-xs">{s.agent_version || "-"}</td>
                <td className="max-w-[240px] truncate px-2 py-3 text-xs text-muted">{s.notes || "-"}</td>
                <td className="px-4 py-3 text-right">
                  <NodeActions s={s} base={base} onChanged={load} />
                </td>
              </tr>
            ))}
            {servers.length === 0 && (
              <tr><td colSpan={5}><Empty title="还没有节点" hint="点击「添加节点」创建, 拿到安装命令后在被控服务器执行" /></td></tr>
            )}
          </tbody>
        </table>
      </GlassCard>

      {addOpen && (
        <AddNodeDialog
          onClose={() => setAddOpen(false)}
          onCreated={(id, code) => {
            setAddOpen(false);
            setCmdFor({
              id, code,
              command: `curl -fsSL https://raw.githubusercontent.com/YCJE/XProbe/main/scripts/install-agent.sh | bash -s -- --server ${base} --code ${code}`,
            });
            load();
          }}
        />
      )}
      {cmdFor && (
        <CommandDialog cmd={cmdFor.command} onClose={() => { setCmdFor(null); load(); }} />
      )}
    </div>
  );
}

function AddNodeDialog({ onClose, onCreated }: { onClose: () => void; onCreated: (id: number, code: string) => void }) {
  const [name, setName] = useState("");
  const [notes, setNotes] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");

  const create = async () => {
    setBusy(true); setErr("");
    try {
      const r = await api.post<{ id: number; code: string }>("/api/v1/servers", { name, notes });
      onCreated(r.id, r.code);
    } catch (e) {
      setErr(String(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog title="添加节点" onClose={onClose}>
      <Input placeholder="节点名称(必填)" value={name} onChange={(e) => setName(e.target.value)} />
      <Input placeholder="备注(可选)" value={notes} onChange={(e) => setNotes(e.target.value)} />
      {err && <p className="text-xs" style={{ color: "var(--danger)" }}>{err}</p>}
      <p className="text-xs text-muted">创建后会生成该节点专用的一次性安装命令; Agent 首次连接即绑定到此节点。</p>
      <Button onClick={create} disabled={busy || !name}>添加节点</Button>
    </Dialog>
  );
}

function NodeActions({ s, base, onChanged }: { s: ServerInfo; base: string; onChanged: () => void }) {
  const [showCmd, setShowCmd] = useState(false);
  const [editOpen, setEditOpen] = useState(false);

  return (
    <span className="inline-flex gap-1">
      <Button variant="ghost" onClick={() => setShowCmd(!showCmd)} title="一键安装命令">安装命令</Button>
      <Button variant="ghost" onClick={() => setEditOpen(true)} title="编辑节点信息">编辑</Button>
      <Button variant="danger" onClick={async () => {
        if (confirm(`删除节点「${s.display_name || s.hostname}」? 其监控数据将一并删除。`)) {
          await api.del(`/api/v1/servers/${s.id}`); onChanged();
        }
      }}>删除</Button>
      {showCmd && createPortal(<InstallCmdPanel s={s} base={base} onClose={() => setShowCmd(false)} />, document.body)}
      {editOpen && createPortal(<EditNodeDialog s={s} onClose={() => { setEditOpen(false); onChanged(); }} />, document.body)}
    </span>
  );
}

function InstallCmdPanel({ s, base, onClose }: { s: ServerInfo; base: string; onClose: () => void }) {
  const [code, setCode] = useState("");
  const [gen, setGen] = useState(false);
  const genCode = async () => {
    const r = await api.post<{ code: string }>(`/api/v1/servers/${s.id}/install-code`);
    setCode(r.code); setGen(true);
  };
  const cmd = (c: string) =>
    `curl -fsSL https://raw.githubusercontent.com/YCJE/XProbe/main/scripts/install-agent.sh | bash -s -- --server ${base} --code ${c}`;

  return (
    <Dialog title={`安装命令 · ${s.display_name || s.hostname}`} onClose={onClose}>
      {gen ? (
        <>
          <p className="text-xs text-muted">在目标服务器执行(注册码 15 分钟有效, 仅本节点):</p>
          <code className="mono block break-all rounded-lg p-3 text-xs" style={{ background: "var(--card-border)" }}>
            {cmd(code)}
          </code>
          <Button variant="ghost" onClick={() => navigator.clipboard?.writeText(cmd(code))}>复制命令</Button>
        </>
      ) : (
        <>
          <p className="text-xs text-muted">为本节点生成新的专用注册码(一次性, 15 分钟有效):</p>
          <Button onClick={genCode}>生成安装命令</Button>
        </>
      )}
    </Dialog>
  );
}

function EditNodeDialog({ s, onClose }: { s: ServerInfo; onClose: () => void }) {
  const [name, setName] = useState(s.display_name || s.hostname);
  const [notes, setNotes] = useState(s.notes ?? "");
  const save = async () => {
    await api.put(`/api/v1/servers/${s.id}/meta`, {
      display_name: name, notes, region: s.region, country_code: s.country_code,
      isp: s.isp, tag_ids: s.tags, expires_at: s.expires_at, price_amount: s.price_amount,
      price_currency: s.price_currency, price_cycle: s.price_cycle, traffic_quota_bytes: s.traffic_quota_bytes,
    });
    onClose();
  };
  return (
    <Dialog title={`编辑节点 · ${s.hostname}`} onClose={onClose}>
      <Input placeholder="名称" value={name} onChange={(e) => setName(e.target.value)} />
      <Input placeholder="备注" value={notes} onChange={(e) => setNotes(e.target.value)} />
      <Button onClick={save} disabled={!name}>保存</Button>
    </Dialog>
  );
}

function Dialog({ title, onClose, children }: { title: string; onClose: () => void; children: ReactNode }) {
  return (
    createPortal(
      <div className="fixed inset-0 z-[60] flex items-center justify-center bg-black/50 p-4" onClick={onClose}>
        <div className="glass w-full max-w-lg p-5" onClick={(e) => e.stopPropagation()}>
          <h3 className="mb-3 text-sm font-semibold">{title}</h3>
          <div className="flex flex-col gap-3">{children}</div>
        </div>
      </div>,
      document.body,
    )
  );
}

function CommandDialog({ cmd, onClose }: { cmd: string; onClose: () => void }) {
  return (
    createPortal(
      <div className="fixed inset-0 z-[60] flex items-center justify-center bg-black/50 p-4" onClick={onClose}>
        <div className="glass w-full max-w-2xl p-5" onClick={(e) => e.stopPropagation()}>
          <h3 className="mb-3 text-sm font-semibold">节点安装命令</h3>
          <p className="mb-2 text-xs text-muted">在目标服务器以 root 执行(注册码 15 分钟有效, 首次连接即绑定本节点):</p>
          <code className="mono block break-all rounded-lg p-3 text-xs" style={{ background: "var(--card-border)" }}>{cmd}</code>
          <div className="mt-3 flex gap-2">
            <Button variant="ghost" onClick={() => navigator.clipboard?.writeText(cmd)}>复制命令</Button>
            <Button onClick={onClose}>关闭</Button>
          </div>
        </div>
      </div>,
      document.body,
    )
  );
}
