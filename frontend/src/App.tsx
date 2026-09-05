import { useEffect, useState, type ReactNode } from "react";
import { Navigate, Outlet, Route, Routes, useLocation, useNavigate } from "react-router-dom";
import { api, ApiError, type ServersResp } from "./lib/api";
import { Button, cn } from "./components/ui";
import { LoginPage } from "./pages/LoginPage";
import { DashboardPage } from "./pages/DashboardPage";
import { DetailPage } from "./pages/DetailPage";
import { NodesPage } from "./pages/NodesPage";
import { ServicesPage } from "./pages/ServicesPage";
import { ReportsPage } from "./pages/ReportsPage";
import { SettingsPage } from "./pages/SettingsPage";
import { AlertsPage } from "./pages/AlertsPage";
import { NotifyPage } from "./pages/NotifyPage";
import { ShareConfigPage, SharePage } from "./pages/SharePage";

const MENU = [
  { to: "/dashboard", label: "仪表盘", icon: "M3 3h8v8H3zM13 3h8v5h-8zM13 10h8v11h-8zM3 13h8v8H3z" },
  { to: "/servers", label: "服务器", icon: "M4 4h16v6H4zM4 14h16v6H4zM7 7h.01M7 17h.01" },
  { to: "/services", label: "服务监控", icon: "M22 12h-4l-3 9L9 3l-3 9H2" },
  { to: "/reports", label: "报表", icon: "M4 20V10M10 20V4M16 20v-6M22 20H2" },
  { to: "/alerts", label: "告警", icon: "M12 2L2 7l10 5 10-5-10-5zM2 17l10 5 10-5M2 12l10 5 10-5" },
  { to: "/notify", label: "通知", icon: "M18 8a6 6 0 0 0-12 0c0 7-3 9-3 9h18s-3-2-3-9M13.7 21a2 2 0 0 1-3.4 0" },
  { to: "/settings", label: "设置", icon: "M12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6zM19.4 15a7.9 7.9 0 0 0 .1-2 7.9 7.9 0 0 0-.1-2l2.1-1.6-2-3.4-2.5 1a8 8 0 0 0-1.7-1L15 2h-6l-.3 2.6a8 8 0 0 0-1.7 1l-2.5-1-2 3.4L4.6 11a7.9 7.9 0 0 0 0 4l-2.1 1.6 2 3.4 2.5-1a8 8 0 0 0 1.7 1L9 22h6l.3-2.6a8 8 0 0 0 1.7-1l2.5 1 2-3.4-2.1-1.6z" },
  { to: "/share-config", label: "公开页", icon: "M18 8a3 3 0 1 0-2.8-4M6 15a3 3 0 1 0 0 6 3 3 0 0 0 0-6zM18 19a3 3 0 1 0 0-6M8.6 13.5l6.8 3.7M8.6 10.5l6.8-3.7" },
];

function Sidebar({ collapsed, onToggle }: { collapsed: boolean; onToggle: () => void }) {
  const nav = useNavigate();
  const loc = useLocation();
  const dark = document.documentElement.classList.contains("dark");
  const [open, setOpen] = useState(false);

  const item = (m: (typeof MENU)[number]) => {
    const active = loc.hash.startsWith(`#${m.to}`);
    return (
      <button
        key={m.to}
        onClick={() => { nav(m.to); setOpen(false); }}
        className={cn(
          "flex w-full items-center gap-3 rounded-lg px-3 py-2 text-sm transition-colors",
          active ? "bg-primary text-primary-fg" : "text-muted hover:bg-card hover:text-foreground",
        )}
        title={collapsed ? m.label : undefined}
      >
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor"
          strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className="shrink-0" aria-hidden>
          <path d={m.icon} />
        </svg>
        {!collapsed && <span className="truncate">{m.label}</span>}
      </button>
    );
  };

  return (
    <>
      {open && <div className="fixed inset-0 z-40 bg-black/50 md:hidden" onClick={() => setOpen(false)} />}
      <aside
        className={cn(
          "fixed inset-y-0 left-0 z-50 flex flex-col border-r border-card-border bg-card backdrop-blur transition-all duration-200",
          collapsed ? "w-16" : "w-56",
          open ? "translate-x-0" : "-translate-x-full md:translate-x-0",
        )}
      >
        <div className={cn("flex items-center px-4 py-5", collapsed && "px-3")}>
          <span className="text-lg font-bold tracking-tight">
            <span style={{ color: "var(--primary)" }}>X</span>
            {!collapsed && <span>Probe</span>}
          </span>
        </div>
        <nav className="flex flex-1 flex-col gap-1 px-2">{MENU.map(item)}</nav>
        <div className={cn("flex flex-col gap-1 border-t border-card-border p-2", collapsed && "items-center")}>
          <Button variant="ghost" onClick={() => {
            const next = !dark;
            document.documentElement.classList.toggle("dark", next);
            localStorage.setItem("xprobe-theme", next ? "dark" : "light");
          }} title="切换主题" className={cn("w-full justify-start", collapsed && "justify-center")}>
            {dark ? "☀" : "☾"}{!collapsed && " 主题"}
          </Button>
          <Button variant="ghost" onClick={async () => {
            await api.post("/api/v1/auth/logout").catch(() => undefined);
            location.hash = "#/login";
          }} className={cn("w-full justify-start", collapsed && "justify-center")}>
            ⎋{!collapsed && " 登出"}
          </Button>
          <Button variant="ghost" onClick={onToggle} className={cn("w-full justify-start", collapsed && "justify-center")}
            title={collapsed ? "展开侧边栏" : "收起侧边栏"}>
            {collapsed ? "»" : "«"}{!collapsed && " 收起"}
          </Button>
        </div>
      </aside>
      <div className="fixed inset-x-0 top-0 z-30 flex items-center gap-2 border-b border-card-border bg-card p-3 md:hidden">
        <Button variant="ghost" onClick={() => setOpen(true)}>☰</Button>
        <span className="font-bold"><span style={{ color: "var(--primary)" }}>X</span>Probe</span>
      </div>
    </>
  );
}

function Layout() {
  const [collapsed, setCollapsed] = useState(() => localStorage.getItem("xprobe-sidebar") === "1");
  const toggle = () => {
    setCollapsed((v) => {
      localStorage.setItem("xprobe-sidebar", v ? "0" : "1");
      return !v;
    });
  };
  return (
    <div className="min-h-screen">
      <Sidebar collapsed={collapsed} onToggle={toggle} />
      <main className={cn("p-4 pt-16 transition-all duration-200 md:pt-6", collapsed ? "md:ml-16" : "md:ml-56")}>
        <Outlet />
      </main>
    </div>
  );
}

export function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route path="/s/:shareId/*" element={<SharePage />} />
      <Route element={<RequireAuth />}>
        <Route element={<Layout />}>
          <Route index element={<Navigate to="/dashboard" replace />} />
          <Route path="dashboard" element={<DashboardPage />} />
          <Route path="dashboard/map" element={<DashboardPage view="map" />} />
          <Route path="server/:id" element={<DetailPage />} />
          <Route path="servers" element={<NodesPage />} />
          <Route path="services" element={<ServicesPage />} />
          <Route path="reports" element={<ReportsPage />} />
          <Route path="alerts" element={<AlertsPage />} />
          <Route path="notify" element={<NotifyPage />} />
          <Route path="settings" element={<SettingsPage />} />
          <Route path="share-config" element={<ShareConfigPage />} />
        </Route>
      </Route>
      <Route path="*" element={<Navigate to="/dashboard" replace />} />
    </Routes>
  );
}

/** 登录守卫: 访问受保护页前探活一次; 子路由经 <Outlet/> 渲染(v6 语义)。 */
function RequireAuth() {
  const [state, setState] = useState<"check" | "ok">("check");
  useEffect(() => {
    api.get<ServersResp>("/api/v1/servers")
      .then(() => setState("ok"))
      .catch((e) => {
        if (e instanceof ApiError && e.status === 401) location.hash = "#/login";
        else setState("ok");
      });
  }, []);
  if (state === "check") {
    return <div className="flex h-screen items-center justify-center text-sm text-muted">加载中…</div>;
  }
  return <Outlet />;
}

export default App;
