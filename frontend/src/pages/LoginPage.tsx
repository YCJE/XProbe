import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { api, ApiError } from "../lib/api";
import { Button, GlassCard, Input } from "../components/ui";

/** 登录页(含首次管理员初始化, 设计文档 7.3: 无默认密码)。 */
export function LoginPage() {
  const nav = useNavigate();
  const [mode, setMode] = useState<"check" | "setup" | "login">("check");
  const [username, setUsername] = useState("admin");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  if (mode === "check") {
    api.get<{ setup_done: boolean }>("/api/v1/auth/setup-state")
      .then((r) => setMode(r.setup_done ? "login" : "setup"))
      .catch(() => setMode("login"));
    return <div className="flex h-screen items-center justify-center text-sm text-muted">加载中…</div>;
  }

  const submit = async () => {
    setBusy(true);
    setError("");
    try {
      if (mode === "setup") {
        await api.post("/api/v1/auth/setup", { username, password });
      }
      await api.post("/api/v1/auth/login", { username, password });
      nav("/dashboard");
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "网络错误");
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="flex h-screen items-center justify-center px-4">
      <GlassCard className="w-full max-w-sm">
        <h1 className="mb-1 text-lg font-semibold">XProbe 控制台</h1>
        <p className="mb-5 text-xs text-muted">
          {mode === "setup" ? "首次使用: 创建管理员账号(无默认密码)" : "安全优先的纯只读服务器探针"}
        </p>
        <form
          className="flex flex-col gap-3"
          onSubmit={(e) => {
            e.preventDefault();
            submit();
          }}
        >
          <Input
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            placeholder="用户名"
            autoComplete="username"
          />
          <Input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            placeholder={mode === "setup" ? "密码(≥12 位, 含大小写与数字)" : "密码"}
            autoComplete={mode === "setup" ? "new-password" : "current-password"}
          />
          {error && <p className="text-xs" style={{ color: "var(--danger)" }}>{error}</p>}
          <Button type="submit" disabled={busy || !username || !password}>
            {busy ? "请稍候…" : mode === "setup" ? "创建管理员并登录" : "登录"}
          </Button>
        </form>
      </GlassCard>
    </div>
  );
}
