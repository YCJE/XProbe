import { useEffect, useState } from "react";
import { api, ApiError } from "../lib/api";
import type { NotifyChannel } from "../lib/types";
import { Button, Empty, GlassCard, Input, Select } from "../components/ui";

/** 通知渠道管理(设计文档 6.6); 后端在 M5 上线。 */
export function NotifyPage() {
  const [channels, setChannels] = useState<NotifyChannel[]>([]);
  const [err, setErr] = useState("");
  const [type, setType] = useState<NotifyChannel["type"]>("webhook");
  const [name, setName] = useState("");
  const [endpoint, setEndpoint] = useState("");

  const load = async () => {
    try {
      const r = await api.get<{ channels: NotifyChannel[] }>("/api/v1/notify/channels");
      setChannels(r.channels ?? []);
      setErr("");
    } catch (e) {
      setErr(e instanceof ApiError && e.status === 404 ? "" : String(e));
    }
  };
  useEffect(() => { load(); }, []);

  const create = async () => {
    const config =
      type === "webhook" ? { url: endpoint } :
      type === "telegram" ? { bot_token: endpoint, chat_id: "" } :
      { host: endpoint, port: 465 };
    await api.post("/api/v1/notify/channels", { name, type, config });
    setName(""); setEndpoint("");
    load();
  };

  return (
    <div className="mx-auto max-w-3xl px-4 py-6">
      <GlassCard>
        <h2 className="mb-3 text-sm font-semibold">通知渠道</h2>
        <div className="mb-4 flex flex-wrap items-center gap-2">
          <Input placeholder="名称" value={name} onChange={(e) => setName(e.target.value)} className="w-36" />
          <Select value={type} onChange={(e) => setType(e.target.value as NotifyChannel["type"])}>
            <option value="webhook">Webhook</option>
            <option value="telegram">Telegram</option>
            <option value="smtp">邮件 SMTP</option>
          </Select>
          <Input
            className="w-64"
            placeholder={type === "webhook" ? "https://hooks.example.com/…" : type === "telegram" ? "Bot Token" : "smtp.example.com"}
            value={endpoint}
            onChange={(e) => setEndpoint(e.target.value)}
          />
          <Button onClick={create} disabled={!name || !endpoint}>添加</Button>
        </div>
        {channels.length === 0 ? (
          <Empty title="暂无通知渠道" hint="Webhook / Telegram / SMTP 均经 SSRF 防护" />
        ) : (
          <ul className="text-sm">
            {channels.map((c) => (
              <li key={c.id} className="flex items-center justify-between border-b border-card-border/50 py-2 last:border-0">
                <span>{c.name} <span className="ml-2 text-xs text-muted mono">{c.type}</span></span>
                <Button variant="ghost" onClick={async () => { await api.del(`/api/v1/notify/channels/${c.id}`); load(); }}>删除</Button>
              </li>
            ))}
          </ul>
        )}
        {err && <p className="mt-2 text-xs text-muted">通知接口尚未就绪({err})</p>}
      </GlassCard>
    </div>
  );
}

/** 公开分享页配置占位(M5 提供后端)。 */
export function ShareConfigPage() {
  return (
    <div className="mx-auto max-w-3xl px-4 py-6">
      <GlassCard>
        <h2 className="mb-3 text-sm font-semibold">公开分享页</h2>
        <p className="text-xs text-muted">
          状态页 /s/:share_id 免登录展示勾选的服务器(卡片/表格双视图, 无 IP/Token 等敏感字段)。
          后端在 M5 上线后可在此配置标题/Logo/页脚与包含的服务器。
        </p>
      </GlassCard>
    </div>
  );
}
