import { describe, expect, it } from "vitest";
import { filterSortServers } from "../store/dashboard";
import { formatBytes, latencyVar, expiresInDays } from "../lib/format";
import type { ServerInfo } from "../lib/types";

function server(over: Partial<ServerInfo>): ServerInfo {
  return {
    id: 1, hostname: "h", display_name: "", online: true, os: "", arch: "", agent_version: "",
    ipv4: "1.2.3.4", ipv6: "", region: "", country_code: "US", isp: "", notes: "", tags: [],
    cpu: 10, cores: 2, mem_total: 100, mem_used: 50, swap_total: 0, swap_used: 0, disk: [],
    rx_speed: 0, tx_speed: 0, tcp_connections: 0, udp_connections: 0,
    traffic_monthly: { month: "2026-08", rx_bytes: 0, tx_bytes: 0 },
    uptime: 0, process_count: 0, ping: {}, ping_loss: {},
    expires_at: 0, price_amount: 0, price_currency: "", price_cycle: "", traffic_quota_bytes: 0,
    last_seen: 0, ...over,
  };
}

describe("filterSortServers", () => {
  const servers = [
    server({ id: 1, display_name: "US-LAX-01", tags: [1], country_code: "US", cpu: 90 }),
    server({ id: 2, display_name: "HK-01", tags: [], country_code: "HK", ipv4: "", ipv6: "::1", cpu: 10 }),
  ];
  it("搜索匹配", () => {
    expect(filterSortServers(servers, { search: "lax", tagFilter: null, regionFilter: "", ipFilter: "all", sortBy: "name" }).map((s) => s.id)).toEqual([1]);
  });
  it("标签筛选", () => {
    expect(filterSortServers(servers, { search: "", tagFilter: 1, regionFilter: "", ipFilter: "all", sortBy: "name" }).map((s) => s.id)).toEqual([1]);
  });
  it("IP 栈筛选(v6)", () => {
    expect(filterSortServers(servers, { search: "", tagFilter: null, regionFilter: "", ipFilter: "v6", sortBy: "name" }).map((s) => s.id)).toEqual([2]);
  });
  it("CPU 排序", () => {
    expect(filterSortServers(servers, { search: "", tagFilter: null, regionFilter: "", ipFilter: "all", sortBy: "cpu" })[0].id).toBe(1);
  });
});

describe("format", () => {
  it("formatBytes 单位自适应", () => {
    expect(formatBytes(0)).toBe("0 B");
    expect(formatBytes(1024)).toBe("1.0 KB");
    expect(formatBytes(1024 ** 3)).toBe("1.0 GB");
    expect(formatBytes(null)).toBe("--");
  });
  it("latencyVar 色阶阈值(设计文档 6.4.4)", () => {
    expect(latencyVar(10)).toBe("var(--lat-1)");
    expect(latencyVar(80)).toBe("var(--lat-2)");
    expect(latencyVar(150)).toBe("var(--lat-3)");
    expect(latencyVar(300)).toBe("var(--lat-4)");
    expect(latencyVar(500)).toBe("var(--lat-5)");
    expect(latencyVar(null)).toBe("var(--lat-6)");
  });
  it("expiresInDays 到期天数", () => {
    const now = Date.now();
    expect(expiresInDays(Math.floor((now + 5 * 86400000) / 1000), now)).toBe(5);
    expect(expiresInDays(0, now)).toBe(Infinity);
  });
});
