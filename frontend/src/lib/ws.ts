import { useEffect, useRef } from "react";
import { useDashboard } from "../store/dashboard";
import type { ServerInfo } from "./types";

/** 仪表盘实时推送(设计文档 6.8): 首帧即渲染; 断线自动重连; 页面可见即重拉全量。 */
export function useDashboardWS() {
  const applyPush = useDashboard((s) => s.applyPush);
  const setConnected = useDashboard((s) => s.setConnected);
  const load = useDashboard((s) => s.load);
  const retry = useRef(0);
  const timer = useRef<ReturnType<typeof setTimeout>>();

  useEffect(() => {
    let closed = false;
    let ws: WebSocket | null = null;

    const connect = () => {
      if (closed) return;
      const proto = location.protocol === "https:" ? "wss" : "ws";
      ws = new WebSocket(`${proto}://${location.host}/ws/dashboard`);
      ws.onopen = () => {
        retry.current = 0;
        setConnected(true);
      };
      ws.onmessage = (ev) => {
        try {
          const msg = JSON.parse(ev.data) as { type: string; servers?: ServerInfo[] };
          if (msg.type === "dashboard_update" && msg.servers) applyPush(msg.servers);
        } catch { /* 忽略坏帧 */ }
      };
      ws.onclose = () => {
        setConnected(false);
        if (closed) return;
        const delay = Math.min(1000 * 2 ** retry.current++, 15000);
        timer.current = setTimeout(connect, delay);
      };
      ws.onerror = () => ws?.close();
    };
    connect();

    // 页面切回/网络恢复: 主动重拉全量(NodeGet 主题验证过的体验优化)
    const refresh = () => {
      if (document.visibilityState === "visible") load();
    };
    document.addEventListener("visibilitychange", refresh);
    window.addEventListener("online", refresh);

    return () => {
      closed = true;
      if (timer.current) clearTimeout(timer.current);
      document.removeEventListener("visibilitychange", refresh);
      window.removeEventListener("online", refresh);
      ws?.close();
    };
  }, [applyPush, setConnected, load]);
}
