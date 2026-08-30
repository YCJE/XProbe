import { useParams } from "react-router-dom";
import { GlassCard, Empty } from "../components/ui";

/** 公开分享页 /s/:shareId(M5 后端上线后接管, 设计文档 6.6)。 */
export function SharePage() {
  const { shareId } = useParams();
  return (
    <div className="mx-auto max-w-7xl px-4 py-8">
      <header className="mb-6 text-center">
        <h1 className="text-xl font-bold">服务状态</h1>
        <p className="text-xs text-muted">Share · {shareId}</p>
      </header>
      <GlassCard>
        <Empty title="状态页数据准备中" hint="M5 上线后此处展示白名单服务器的卡片/表格视图" />
      </GlassCard>
    </div>
  );
}
