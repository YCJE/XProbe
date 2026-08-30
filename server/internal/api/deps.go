package api

import (
	"time"

	"github.com/YCJE/XProbe/server/internal/pkg"
	"github.com/YCJE/XProbe/server/internal/repository"
	"github.com/YCJE/XProbe/server/internal/service"
)

// Deps 汇集 API 层依赖(全部路由共用)。
type Deps struct {
	// Agent 通道
	Registry        *service.Registry
	Hub             *service.Hub
	Agents          *repository.AgentRepo
	PingTargets     *repository.PingTargetRepo
	Records         *repository.RecordRepo
	RegisterLimiter *pkg.Limiter // 5 次/分钟/IP
	// 管理面
	Auth            *service.Auth
	JWT             *pkg.JWTManager
	Sessions        *repository.SessionRepo
	Tags            *repository.TagRepo
	LoginLimiter    *pkg.Limiter // 5 次/分钟/IP
	GlobalLimiter   *pkg.Limiter // 120 次/分钟/IP
	CertFingerprint string
	WSCompression   bool
	Now             func() time.Time
}
