import { create } from "zustand";
import type { ServerInfo, Tag } from "../lib/types";
import { api, type ServersResp, type TagsResp } from "../lib/api";

export type ViewMode = "card" | "table";

interface DashboardState {
  servers: ServerInfo[];
  tags: Tag[];
  view: ViewMode;
  search: string;
  tagFilter: number | null;
  regionFilter: string;
  ipFilter: "all" | "v4" | "v6";
  sortBy: "name" | "cpu" | "mem" | "traffic" | "latency" | "expires";
  connected: boolean;
  load: () => Promise<void>;
  applyPush: (servers: ServerInfo[]) => void;
  setConnected: (v: boolean) => void;
  set: <K extends keyof DashboardState>(k: K, v: DashboardState[K]) => void;
}

/** 筛选 + 排序(纯函数, 便于单测)。 */
export function filterSortServers(
  servers: ServerInfo[],
  f: { search: string; tagFilter: number | null; regionFilter: string; ipFilter: string; sortBy: string },
): ServerInfo[] {
  let out = servers.filter((s) => {
    const name = `${s.display_name || s.hostname} ${s.region} ${s.isp}`.toLowerCase();
    if (f.search && !name.includes(f.search.toLowerCase())) return false;
    if (f.tagFilter !== null && !s.tags.includes(f.tagFilter)) return false;
    if (f.regionFilter && s.country_code !== f.regionFilter) return false;
    if (f.ipFilter === "v4" && !s.ipv4) return false;
    if (f.ipFilter === "v6" && !s.ipv6) return false;
    return true;
  });
  const by: Record<string, (a: ServerInfo, b: ServerInfo) => number> = {
    name: (a, b) => (a.display_name || a.hostname).localeCompare(b.display_name || b.hostname),
    cpu: (a, b) => (b.cpu ?? -1) - (a.cpu ?? -1),
    mem: (a, b) => (b.mem_total ? b.mem_used / b.mem_total : -1) - (a.mem_total ? a.mem_used / a.mem_total : -1),
    traffic: (a, b) => b.traffic_monthly.rx_bytes + b.traffic_monthly.tx_bytes - (a.traffic_monthly.rx_bytes + a.traffic_monthly.tx_bytes),
    latency: (a, b) => minPing(a) - minPing(b),
    expires: (a, b) => a.expires_at - b.expires_at,
  };
  out = [...out].sort(by[f.sortBy] ?? by.name);
  return out;
}

function minPing(s: ServerInfo): number {
  const vals = Object.values(s.ping ?? {});
  return vals.length ? Math.min(...vals) : Number.MAX_SAFE_INTEGER;
}

export const useDashboard = create<DashboardState>((set) => ({
  servers: [],
  tags: [],
  view: "card",
  search: "",
  tagFilter: null,
  regionFilter: "",
  ipFilter: "all",
  sortBy: "name",
  connected: false,
  load: async () => {
    try {
      const [s, t] = await Promise.all([
        api.get<ServersResp>("/api/v1/servers"),
        api.get<TagsResp>("/api/v1/tags").catch(() => ({ tags: [] }) as TagsResp),
      ]);
      set({ servers: s.servers, tags: t.tags });
    } catch {
      /* 登录跳转由 api 层处理 */
    }
  },
  applyPush: (servers) => set({ servers }),
  setConnected: (v) => set({ connected: v }),
  set: (k, v) => set({ [k]: v } as Partial<DashboardState>),
}));
