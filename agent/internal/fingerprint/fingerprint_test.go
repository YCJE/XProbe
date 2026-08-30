package fingerprint

import (
	"net"
	"testing"
)

func TestCompute_DeterministicAndSaltSensitive(t *testing.T) {
	a := Compute("salt1", "Intel Xeon", "aa:bb:cc:dd:ee:ff", "linux")
	b := Compute("salt1", "Intel Xeon", "aa:bb:cc:dd:ee:ff", "linux")
	if a != b || len(a) != 64 {
		t.Fatalf("not deterministic: %s vs %s", a, b)
	}
	if Compute("salt2", "Intel Xeon", "aa:bb:cc:dd:ee:ff", "linux") == a {
		t.Fatal("different salt must produce different fingerprint")
	}
	if Compute("salt1", "AMD EPYC", "aa:bb:cc:dd:ee:ff", "linux") == a {
		t.Fatal("different cpu must produce different fingerprint")
	}
	if Compute("salt1", "Intel Xeon", "11:22:33:44:55:66", "linux") == a {
		t.Fatal("different mac must produce different fingerprint")
	}
}

func macIf(name string, flags net.Flags, hw string) net.Interface {
	var mac net.HardwareAddr
	if hw != "" {
		mac, _ = net.ParseMAC(hw)
	}
	return net.Interface{Name: name, Flags: flags, HardwareAddr: mac}
}

func TestPickPrimaryMAC(t *testing.T) {
	up := net.FlagUp
	ifaces := []net.Interface{
		macIf("lo", up|net.FlagLoopback, "aa:bb:cc:dd:ee:00"), // 回环跳过
		macIf("eth0", up, "aa:bb:cc:dd:ee:01"),                // 命中
		macIf("eth1", up, "aa:bb:cc:dd:ee:02"),                // 后备
	}
	got, err := PickPrimaryMAC(ifaces)
	if err != nil || got != "aa:bb:cc:dd:ee:01" {
		t.Fatalf("got %s, %v", got, err)
	}

	// 只有回环与无 MAC 接口 → 错误
	bad := []net.Interface{
		macIf("lo", up|net.FlagLoopback, "aa:bb:cc:dd:ee:00"),
		macIf("dummy", up, ""),
		macIf("down", 0, "aa:bb:cc:dd:ee:03"),
	}
	if _, err := PickPrimaryMAC(bad); err == nil {
		t.Fatal("no suitable iface should error")
	}
}
