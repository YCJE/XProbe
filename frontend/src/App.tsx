import { useEffect, useState } from "react";
import { Navigate, Outlet, Route, Routes, useNavigate } from "react-router-dom";
import { api, ApiError, type ServersResp } from "./lib/api";
import { Button } from "./components/ui";
import { LoginPage } from "./pages/LoginPage";
import { DashboardPage } from "./pages/DashboardPage";
import { DetailPage } from "./pages/DetailPage";
import { SettingsPage } from "./pages/SettingsPage";
import { AlertsPage } from "./pages/AlertsPage";
import { NotifyPage } from "./pages/NotifyPage";
import { ServicesPage } from "./pages/ServicesPage";
import { ReportsPage } from "./pages/ReportsPage";
import { MapView } from "./components/MapView";
import { ShareConfigPage } from "./pages/SharePage";
import { SharePage } from "./pages/SharePage";

function TopNav() {
  const nav = useNavigate();
  const [dark, setDark] = useState(document.documentElement.classList.contains("dark"));

  const toggleTheme = () => {
    const next = !dark;
    setDark(next);
    document.documentElement.classList.toggle("dark", next);
    localStorage.setItem("xprobe-theme", next ? "dark" : "light");
  };

  const logout = async () => {
    await api.post("/api/v1/auth/logout").catch(() => undefined);
    nav("/login");
  };

  const item = "rounded-lg px-3 py-1.5 text-sm hover:bg-card";
  return (
    <header className="mx-auto flex max-w-7xl items-center gap-2 px-4 py-4">
      <span className="mr-2 text-base font-bold tracking-tight">
        <span style={{ color: "var(--primary)" }}>X</span>Probe
      </span>
      <nav className="flex gap-1">
        <a className={item} href="#/dashboard">仪表盘</a>
        <a className={item} href="#/services">服务</a>
        <a className={item} href="#/reports">报表</a>
        <a className={item} href="#/alerts">告警</a>
        <a className={item} href="#/notify">通知</a>
        <a className={item} href="#/settings">设置</a>
        <a className={item} href="#/share-config">公开页</a>
      </nav>
      <span className="ml-auto flex items-center gap-1">
        <Button variant="ghost" onClick={toggleTheme} aria-label="切换主题">
          {dark ? "☀" : "☾"}
        </Button>
        <Button variant="ghost" onClick={logout}>登出</Button>
      </span>
    </header>
  );
}

export function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route path="/s/:shareId/*" element={<SharePage />} />
      <Route path="/" element={<RequireAuth />}>
        <Route index element={<Navigate to="/dashboard" replace />} />
        <Route path="dashboard" element={<><TopNav /><DashboardPage /></>} />
        <Route path="dashboard/map" element={<><TopNav /><DashboardPage view="map" /></>} />
        <Route path="server/:id" element={<><TopNav /><DetailPage /></>} />
        <Route path="services" element={<><TopNav /><ServicesPage /></>} />
        <Route path="reports" element={<><TopNav /><ReportsPage /></>} />
        <Route path="alerts" element={<><TopNav /><AlertsPage /></>} />
        <Route path="notify" element={<><TopNav /><NotifyPage /></>} />
        <Route path="settings" element={<><TopNav /><SettingsPage /></>} />
        <Route path="share-config" element={<><TopNav /><ShareConfigPage /></>} />
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
        else setState("ok"); // 网络错误仍允许进入(WS 会提示重连)
      });
  }, []);
  if (state === "check") {
    return <div className="flex h-screen items-center justify-center text-sm text-muted">加载中…</div>;
  }
  return <Outlet />;
}

export default App;
