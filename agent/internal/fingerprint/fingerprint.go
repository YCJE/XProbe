// Package fingerprint 实现主机指纹绑定(设计文档 7.5, 防 Token 跨机盗用 T6)。
package fingerprint

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
)

// Compute = SHA256(install_salt|CPU型号|主网卡MAC|系统类型)。
// 不依赖 hostname(易变, 改名会误拒连); install_salt 由安装脚本生成并持久化于配置。
func Compute(salt, cpuModel, primaryMAC, goos string) string {
	raw := salt + "|" + cpuModel + "|" + primaryMAC + "|" + goos
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}

// PickPrimaryMAC 从网卡列表选取主网卡 MAC: 首个 Up 且非回环且 MAC 非空的接口。
func PickPrimaryMAC(ifaces []net.Interface) (string, error) {
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagLoopback != 0 || ifc.Flags&net.FlagUp == 0 {
			continue
		}
		if ifc.HardwareAddr == nil || ifc.HardwareAddr.String() == "" {
			continue
		}
		return ifc.HardwareAddr.String(), nil
	}
	return "", errors.New("fingerprint: no suitable network interface with MAC")
}
