package service

import (
	"github.com/YCJE/XProbe/internal/model"
	"github.com/YCJE/XProbe/server/internal/repository"
)

// BuildDashboardServers 合并 Agent 元数据与 Hub 实时数据(设计文档 5.3/6.8)。
func BuildDashboardServers(agents []model.Agent, hub *Hub) []model.DashboardServer {
	out := make([]model.DashboardServer, 0, len(agents))
	for i := range agents {
		a := &agents[i]
		ds := model.DashboardServer{
			ID:                a.ID,
			Hostname:          a.Hostname,
			DisplayName:       a.DisplayName,
			Online:            hub.IsOnline(a.ID),
			OS:                a.OS,
			Arch:              a.Arch,
			AgentVersion:      a.AgentVersion,
			IPv4:              a.IPv4,
			IPv6:              a.IPv6,
			Region:            a.Region,
			CountryCode:       a.CountryCode,
			ISP:               a.ISP,
			Tags:              repository.ParseTagIDs(a.TagIDs),
			ExpiresAt:         a.ExpiresAt,
			PriceAmount:       a.PriceAmount,
			PriceCurrency:     a.PriceCurrency,
			PriceCycle:        a.PriceCycle,
			TrafficQuotaBytes: a.TrafficQuotaBytes,
			GeoLat:            a.GeoLat,
			GeoLon:            a.GeoLon,
			LastSeen:          a.LastSeen,
		}
		if r, ok := hub.LatestReport(a.ID); ok {
			ds.CPU = r.CPU.Usage
			ds.Cores = r.CPU.Cores
			ds.MemTotal, ds.MemUsed = r.Memory.Total, r.Memory.Used
			ds.SwapTotal, ds.SwapUsed = r.Memory.SwapTotal, r.Memory.SwapUsed
			ds.Disk = r.Disk
			ds.RxSpeed, ds.TxSpeed = r.Network.RxSpeed, r.Network.TxSpeed
			ds.TCPConnections = r.Network.TCPConnections
			ds.UDPConnections = r.Network.UDPConnections
			ds.TrafficMonthly = r.TrafficMonthly
			ds.Uptime = r.Uptime
			ds.ProcessCount = r.ProcessCount
		}
		if p, ok := hub.LatestPing(a.ID); ok {
			ds.Ping = map[string]float64{}
			ds.PingLoss = map[string]float64{}
			for _, t := range p {
				name := t.Name
				if name == "" {
					name = t.Target
				}
				ds.Ping[name] = t.AvgLatency
				ds.PingLoss[name] = t.Loss
			}
		}
		out = append(out, ds)
	}
	return out
}
