import { useEffect, useState } from "react";
import { api } from "../lib/api";
import { formatUptime, latencyVar } from "../lib/format";
import type { ServerInfo } from "../lib/types";
import { Badge, Button, Empty, GlassCard, Input, StatusDot } from "../components/ui";

interface PublicServer {
  display_name: string;
  hostname: string;
  online: boolean;
  country_code: string;
  region: string;
  isp: string;
  cpu: number | null;
  mem_used: number;
  mem_total: number;
  disk: { device: string; total: number; used: number }[];
  uptime: number;
  ping: Record<string, number>;
  ping_loss: Record<string, number>;
}

interface PublicPayload {
  share_id: string;
  title: string;
  logo_url: string;
  footer_text: string;
  servers: PublicServer[];
}

const flagEmoji = (cc: string) =>
  cc && cc.length === 2
    ? String.fromCodePoint(...[...cc.toUpperCase()].map((c) => 127397 + c.charCodeAt(0)))
    : "🌐";

/** 公开分享页 /s/:shareId(设计文档 6.6: 白名单字段, 无 IP/Token/配置)。 */
export function SharePage() {
  const { shareId } = useParamsShare();
  const [data, setData] = useState<PublicPayload | null>(null);
  const [notFound, setNotFound] = useState(false);

  useEffect(() => {
    if (!shareId) return;
    api.get<PublicPayload>(`/api/v1/public/${shareId}`)
      .then(setData)
      .catch(() => setNotFound(true));
  }, [shareId]);

  useEffect(() => {
    const t = setInterval(() => {
      if (shareId) api.get<PublicPayload>(`/api/v1/public/${shareId}`).then(setData).catch(() => undefined);
    }, 10_000);
    return () => clearInterval(t);
  }, [shareId]);

  if (notFound) {
    return (
      <div className="mx-auto max-w-3xl px-4 py-16">
        <GlassCard><Empty title="分享链接不存在或已失效" /></GlassCard>
      </div>
    );
  }
  if (!data) {
    return <div className="flex h-screen items-center justify-center text-sm text-muted">加载中…</div>;
  }

  return (
    <div className="mx-auto max-w-7xl px-4 py-8">
      <header className="mb-6 text-center">
        {data.logo_url && (
          <img src={data.logo_url} alt="logo" className="mx-auto mb-2 h-12" referrerPolicy="no-referrer" />
        )}
        <h1 className="text-xl font-bold">{data.title || "服务状态"}</h1>
      </header>
      {data.servers.length === 0 ? (
        <GlassCard><Empty title="暂无公开的服务器" /></GlassCard>
      ) : (
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
          {data.servers.map((s) => {
            const memPct = s.mem_total ? (s.mem_used / s.mem_total) * 100 : 0;
            const diskPct = s.disk?.length
              ? Math.max(...s.disk.map((d) => (d.total ? (d.used / d.total) * 100 : 0)))
              : 0;
            return (
              <GlassCard key={s.hostname} className="flex flex-col gap-3">
                <div className="flex items-center gap-2">
                  <span aria-hidden>{flagEmoji(s.country_code)}</span>
                  <span className="truncate text-sm font-semibold">{s.display_name || s.hostname}</span>
                  <span className="ml-auto flex items-center gap-1.5 text-xs text-muted">
                    <StatusDot online={s.online} />
                    {s.online ? `在线 · ${formatUptime(s.uptime)}` : "离线"}
                  </span>
                </div>
                <div className="flex justify-around">
                  <Ring text={s.cpu === null || s.cpu === undefined ? "--" : `${Math.round(s.cpu)}`} label="CPU" />
                  <Ring text={s.mem_total ? `${Math.round(memPct)}` : "--"} label="内存" />
                  <Ring text={s.disk?.length ? `${Math.round(diskPct)}` : "--"} label="磁盘" />
                </div>
                {Object.keys(s.ping ?? {}).length > 0 && (
                  <div className="flex flex-col gap-1 border-t border-card-border pt-2 text-xs tnum">
                    {Object.entries(s.ping).map(([name, ms]) => (
                      <div key={name} className="flex justify-between">
                        <span className="text-muted">{name}</span>
                        <span style={{ color: latencyVar(ms) }}>
                          {ms.toFixed(1)}ms
                          {(s.ping_loss?.[name] ?? 0) > 0 && ` 丢${s.ping_loss[name].toFixed(0)}%`}
                        </span>
                      </div>
                    ))}
                  </div>
                )}
              </GlassCard>
            );
          })}
        </div>
      )}
      {data.footer_text && (
        <footer className="mt-8 text-center text-xs text-muted">{data.footer_text}</footer>
      )}
    </div>
  );
}

function Ring({ text, label }: { text: string; label: string }) {
  return (
    <div className="flex flex-col items-center">
      <div
        className="flex h-16 w-16 items-center justify-center rounded-full text-lg font-semibold tnum"
        style={{ border: "4px solid var(--card-border)" }}
      >
        {text}
      </div>
      <span className="mt-1 text-xs text-muted">{label}</span>
    </div>
  );
}

function useParamsShare(): { shareId?: string } {
  // 由 App.tsx 的 /s/:shareId 路由传入
  const params = new URLSearchParams(location.hash.split("?")[1] ?? "");
  const match = location.hash.match(/#\/s\/([^/?#]+)/);
  return { shareId: match?.[1] ?? params.get("shareId") ?? undefined };
}

/** 管理端: 公开页配置(M5 后端)。 */
export function ShareConfigPage() {
  const [cfg, setCfg] = useState({ title: "", logo_url: "", footer_text: "", agent_ids: "" });
  const [shareID, setShareID] = useState("");
  const [msg, setMsg] = useState("");
  const [current, setCurrent] = useState<ServerInfo[]>([]);

  useEffect(() => {
    api.get<{ share: { share_id: string; title: string; logo_url: string; footer_text: string; agent_ids: number[] } | null }>("/api/v1/config/share")
      .then((r) => {
        if (r.share) {
          setShareID(r.share.share_id);
          setCfg({
            title: r.share.title ?? "",
            logo_url: r.share.logo_url ?? "",
            footer_text: r.share.footer_text ?? "",
            agent_ids: (r.share.agent_ids ?? []).join(","),
          });
        }
      })
      .catch(() => undefined);
    api.get<{ servers: ServerInfo[] }>("/api/v1/servers").then((r) => setCurrent(r.servers));
  }, []);

  const save = async () => {
    const ids = cfg.agent_ids
      .split(",")
      .map((x) => Number(x.trim()))
      .filter((x) => Number.isFinite(x) && x > 0);
    const r = await api.put<{ share_id: string }>("/api/v1/config/share", {
      title: cfg.title, logo_url: cfg.logo_url, footer_text: cfg.footer_text, agent_ids: ids,
    });
    setShareID(r.share_id);
    setMsg(`已保存, 分享链接: ${location.origin}/#/s/${r.share_id}`);
  };

  return (
    <div className="mx-auto max-w-3xl px-4 py-6">
      <GlassCard>
        <h2 className="mb-3 text-sm font-semibold">公开分享页配置</h2>
        <div className="flex flex-col gap-3 text-sm">
          <Input placeholder="页面标题" value={cfg.title} onChange={(e) => setCfg({ ...cfg, title: e.target.value })} />
          <Input
            placeholder="Logo URL(仅 https)"
            value={cfg.logo_url}
            onChange={(e) => setCfg({ ...cfg, logo_url: e.target.value })}
          />
          <Input placeholder="页脚文字" value={cfg.footer_text} onChange={(e) => setCfg({ ...cfg, footer_text: e.target.value })} />
          <div>
            <p className="mb-1 text-xs text-muted">公开的服务器 ID(逗号分隔; 状态页不展示 IP/Token 等敏感字段):</p>
            <div className="mb-2 flex flex-wrap gap-1">
              {current.map((s) => (
                <Badge key={s.id}>
                  #{s.id} {s.display_name || s.hostname}
                </Badge>
              ))}
            </div>
            <Input placeholder="如 1,3,5" value={cfg.agent_ids} onChange={(e) => setCfg({ ...cfg, agent_ids: e.target.value })} />
          </div>
          <div className="flex items-center gap-3">
            <Button onClick={save}>保存</Button>
            {shareID && (
              <a className="text-xs text-link" href={`#/s/${shareID}`} target="_blank" rel="noreferrer">
                预览分享页
              </a>
            )}
          </div>
          {msg && <p className="text-xs" style={{ color: "var(--success)" }}>{msg}</p>}
        </div>
      </GlassCard>
    </div>
  );
}
