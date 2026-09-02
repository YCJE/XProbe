import { useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import * as echarts from "echarts/core";
import { ScatterChart, EffectScatterChart } from "echarts/charts";
import { GeoComponent, TooltipComponent } from "echarts/components";
import { CanvasRenderer } from "echarts/renderers";
import type { ServerInfo } from "../lib/types";
import { resolveCoord } from "../lib/geo";
import { Empty, GlassCard } from "./ui";

echarts.use([ScatterChart, EffectScatterChart, GeoComponent, TooltipComponent, CanvasRenderer]);

let worldRegistered = false;

/** 地图视图(NodeGet 三视图之三): 国家质心/手动坐标聚合光点, 点击弹列表进详情。 */
export function MapView({ servers }: { servers: ServerInfo[] }) {
  const nav = useNavigate();
  const ref = useRef<HTMLDivElement>(null);
  const [mapError, setMapError] = useState(false);

  useEffect(() => {
    if (worldRegistered) return;
    fetch("/world.json")
      .then((r) => (r.ok ? r.json() : Promise.reject(new Error("world.json missing"))))
      .then((json) => {
        echarts.registerMap("world", json as never);
        worldRegistered = true;
        setMapError(false);
        render();
      })
      .catch(() => setMapError(true));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    if (worldRegistered) render();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [servers]);

  function render() {
    if (!ref.current) return;
    const dark = document.documentElement.classList.contains("dark");
    // 按坐标聚合
    const groups = new Map<string, { coord: [number, number]; items: ServerInfo[] }>();
    for (const s of servers) {
      const coord = resolveCoord(s);
      if (!coord) continue;
      const key = coord.map((v) => v.toFixed(1)).join(",");
      const g = groups.get(key) ?? { coord, items: [] };
      g.items.push(s);
      groups.set(key, g);
    }
    const points = [...groups.entries()].map(([key, g]) => ({
      name: key,
      value: [g.coord[1], g.coord[0], g.items.length],
      items: g.items,
    }));

    const chart = echarts.init(ref.current!);
    chart.setOption({
      animation: false,
      backgroundColor: "transparent",
      tooltip: {
        trigger: "item",
        formatter: (p: { data?: { items?: ServerInfo[] } }) => {
          const items = p.data?.items ?? [];
          return items.map((s) => `${s.display_name || s.hostname} · ${s.online ? "在线" : "离线"}`).join("<br/>");
        },
      },
      geo: {
        map: "world",
        roam: true,
        silent: true,
        itemStyle: {
          areaColor: dark ? "#16223a" : "#e3e9f4",
          borderColor: dark ? "#2c3d5e" : "#c3cfe3",
        },
        emphasis: { disabled: true },
      },
      series: [
        {
          type: "effectScatter",
          coordinateSystem: "geo",
          data: points.filter((p) => p.items.some((s) => s.online)),
          symbolSize: (v: number[]) => 8 + Math.min(v[2], 10) * 2,
          rippleEffect: { scale: 2.2 },
          itemStyle: { color: "var(--success)" },
        },
        {
          type: "scatter",
          coordinateSystem: "geo",
          data: points.filter((p) => !p.items.some((s) => s.online)),
          symbolSize: 10,
          itemStyle: { color: "var(--lat-6)", opacity: 0.8 },
        },
      ],
    });
    const onClick = (params: unknown) => {
      const items = (params as { data?: { items?: ServerInfo[] } }).data?.items;
      if (items && items.length === 1) nav(`/server/${items[0].id}`);
    };
    chart.on("click", onClick);
    const onResize = () => chart.resize();
    window.addEventListener("resize", onResize);
    return () => {
      window.removeEventListener("resize", onResize);
      chart.dispose();
    };
  }

  if (mapError) {
    return (
      <GlassCard>
        <Empty title="世界地图数据不可用" hint="缺少 public/world.json(部署时由构建提供)" />
      </GlassCard>
    );
  }

  const noCoord = servers.filter((s) => !resolveCoord(s)).length;
  return (
    <div>
      <GlassCard className="p-2">
        <div ref={ref} className="h-[520px] w-full" />
      </GlassCard>
      {noCoord > 0 && (
        <p className="mt-2 text-xs text-muted">
          {noCoord} 台服务器未设置国家/坐标(元数据中填写 country_code 或经纬度)
        </p>
      )}
    </div>
  );
}
