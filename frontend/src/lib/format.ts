// 数字与状态格式化(设计系统 §3: 全部数值 tabular-nums, 单位自适应)。

export function formatBytes(n: number | null | undefined): string {
  if (n === null || n === undefined) return "--";
  if (n < 1024) return `${n} B`;
  const units = ["KB", "MB", "GB", "TB", "PB"];
  let v = n / 1024;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v >= 100 ? v.toFixed(0) : v.toFixed(1)} ${units[i]}`;
}

export function formatSpeed(n: number | null | undefined): string {
  if (n === null || n === undefined) return "--";
  return `${formatBytes(n)}/s`;
}

export function formatUptime(seconds: number | null | undefined): string {
  if (seconds === null || seconds === undefined) return "--";
  const d = Math.floor(seconds / 86400);
  const h = Math.floor((seconds % 86400) / 3600);
  if (d > 0) return `${d}天${h}时`;
  const m = Math.floor((seconds % 3600) / 60);
  if (h > 0) return `${h}时${m}分`;
  return `${m}分`;
}

/** 延迟色阶(设计系统 §2.3): 返回 CSS 变量名。超时/离线为 lat6。 */
export function latencyVar(ms: number | null | undefined): string {
  if (ms === null || ms === undefined) return "var(--lat-6)";
  if (ms < 50) return "var(--lat-1)";
  if (ms < 100) return "var(--lat-2)";
  if (ms < 200) return "var(--lat-3)";
  if (ms < 400) return "var(--lat-4)";
  if (ms < 60000) return "var(--lat-5)";
  return "var(--lat-6)";
}

export function usageColor(pct: number): string {
  if (pct > 85) return "var(--danger)";
  if (pct > 60) return "var(--warning)";
  return "var(--success)";
}

export function expiresInDays(expiresAt: number, now = Date.now()): number {
  if (!expiresAt) return Infinity;
  return Math.ceil((expiresAt * 1000 - now) / 86400000);
}

export function formatDate(ts: number): string {
  if (!ts) return "--";
  const d = new Date(ts * 1000);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}

export const currencySymbol: Record<string, string> = {
  CNY: "¥", USD: "$", EUR: "€", JPY: "¥",
};

/** 取 CSS 变量的实际色值(echarts canvas 不解析 var())。 */
export function cssVar(name: string): string {
  if (typeof document === "undefined") return "#888";
  const v = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
  return v || "#888";
}
