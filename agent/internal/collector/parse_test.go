package collector

import (
	"strings"
	"testing"
)

const statFixture = `cpu  74608 2520 24433 1117073 6176 4054 0 0 0 0
cpu0 74608 2520 24433 1117073 6176 4054 0 0 0 0
cpu1 37020 1260 12216 558536 3088 2027 0 0 0 0
intr 12345
`

func TestParseStat(t *testing.T) {
	busy, total, cores, err := ParseStat([]byte(statFixture))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// busy = 74608+2520+24433+4054 = 105615; total = busy + idle 1117073 + iowait 6176
	if busy != 105615 || total != 1228864 {
		t.Fatalf("busy/total = %v/%v, want 105615/1228864", busy, total)
	}
	if cores != 2 {
		t.Fatalf("cores = %d, want 2", cores)
	}
}

func TestParseStatIgnoresGuestTime(t *testing.T) {
	// user=10 idle=100 guest=5 guest_nice=5; guest time is already part of user.
	busy, total, cores, err := ParseStat([]byte("cpu 10 0 0 100 0 0 0 0 5 5\ncpu0 10 0 0 100 0 0 0 0 5 5\n"))
	if err != nil {
		t.Fatal(err)
	}
	if busy != 10 || total != 110 || cores != 1 {
		t.Fatalf("busy/total/cores = %v/%v/%d, want 10/110/1", busy, total, cores)
	}
}

func TestParseStatBadInput(t *testing.T) {
	if _, _, _, err := ParseStat([]byte("garbage\n")); err == nil {
		t.Fatal("expected error for garbage input")
	}
}

const cpuinfoFixture = `processor	: 0
model name	: Intel(R) Xeon(R) E5-2680 v4 @ 2.40GHz
processor	: 1
model name	: Intel(R) Xeon(R) E5-2680 v4 @ 2.40GHz
`

func TestParseCPUInfo(t *testing.T) {
	model, cores, err := ParseCPUInfo([]byte(cpuinfoFixture))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cores != 2 {
		t.Fatalf("cores = %d, want 2", cores)
	}
	if model != "Intel(R) Xeon(R) E5-2680 v4 @ 2.40GHz" {
		t.Fatalf("model = %q", model)
	}
}

func TestParseCPUInfoARMFallback(t *testing.T) {
	fixture := "Processor	: ARMv7 Processor rev 4 (v7l)\nProcessor	: ARMv7 Processor rev 4 (v7l)\n"
	model, cores, err := ParseCPUInfo([]byte(fixture))
	if err != nil || cores != 2 {
		t.Fatalf("cores=%d err=%v", cores, err)
	}
	if !strings.Contains(model, "ARMv7") {
		t.Fatalf("model = %q", model)
	}
}

func TestParseLoadavg(t *testing.T) {
	l1, l5, l15, err := ParseLoadavg([]byte("0.52 0.48 0.50 2/156 4321\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if l1 != 0.52 || l5 != 0.48 || l15 != 0.50 {
		t.Fatalf("loads = %v %v %v", l1, l5, l15)
	}
}

func TestParseMemInfo(t *testing.T) {
	fixture := `MemTotal:        8000000 kB
MemFree:         3000000 kB
MemAvailable:    5000000 kB
SwapTotal:       2000000 kB
SwapFree:        1500000 kB
HugePages_Total:       0
`
	total, avail, swapTotal, swapFree, err := ParseMemInfo([]byte(fixture))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if total != 8000000*1024 || avail != 5000000*1024 ||
		swapTotal != 2000000*1024 || swapFree != 1500000*1024 {
		t.Fatalf("mem values wrong: %d %d %d %d", total, avail, swapTotal, swapFree)
	}
}

func TestParseMemInfoClampsImpossibleAvailability(t *testing.T) {
	total, avail, swapTotal, swapFree, err := ParseMemInfo([]byte(
		"MemTotal: 100 kB\nMemAvailable: 200 kB\nSwapTotal: 10 kB\nSwapFree: 20 kB\n"))
	if err != nil {
		t.Fatal(err)
	}
	if avail != total || swapFree != swapTotal {
		t.Fatalf("values not clamped: total=%d avail=%d swap=%d/%d", total, avail, swapTotal, swapFree)
	}
}

func TestParseMemInfoFallbackToFree(t *testing.T) {
	fixture := "MemTotal:  8000000 kB\nMemFree:  3000000 kB\n"
	_, avail, _, _, err := ParseMemInfo([]byte(fixture))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if avail != 3000000*1024 {
		t.Fatalf("expected MemFree fallback, got %d", avail)
	}
}

const netdevFixture = `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo: 1000 10 0 0 0 0 0 0 1000 10 0 0 0 0 0 0
  eth0: 2000 20 0 0 0 0 0 0 4000 40 0 0 0 0 0 0
`

func TestParseNetDev(t *testing.T) {
	devs, err := ParseNetDev([]byte(netdevFixture))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(devs) != 2 {
		t.Fatalf("expected 2 interfaces, got %d", len(devs))
	}
	if devs["eth0"].RxBytes != 2000 || devs["eth0"].TxBytes != 4000 {
		t.Fatalf("eth0 counters wrong: %+v", devs["eth0"])
	}
	if devs["lo"].RxBytes != 1000 {
		t.Fatalf("lo counters wrong: %+v", devs["lo"])
	}
}

func TestParseTCPEstablished(t *testing.T) {
	fixture := `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100007F:0277 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 1
   1: 0F02000A:0016 0102010A:9C40 01 00000000:00000000 00:00000000 00000000     0        0 2
   2: 0F02000A:9C40 0102010A:0016 01 00000000:00000000 00:00000000 00000000     0        0 3
`
	if got := ParseTCPEstablished([]byte(fixture)); got != 2 {
		t.Fatalf("established = %d, want 2", got)
	}
}

func TestParseUDPCount(t *testing.T) {
	fixture := `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode ref pointer drops
   0: 00000000:0277 00000000:0000 07 00000000:00000000 00:00000000 00000000     0        0 4
   1: 00000000:007B 00000000:0000 07 00000000:00000000 00:00000000 00000000     0        0 5
`
	if got := ParseUDPCount([]byte(fixture)); got != 2 {
		t.Fatalf("udp count = %d, want 2", got)
	}
}

func TestParseMounts(t *testing.T) {
	fixture := `/dev/vda1 / ext4 rw,relatime 0 0
proc /proc proc rw,nosuid 0 0
tmpfs /run tmpfs rw 0 0
/dev/vda15 /boot/efi vfat rw 0 0
overlay / overlay overlay rw 0 0
squashfs /snap/core squashfs ro 0 0
`
	mounts := ParseMounts([]byte(fixture))
	if len(mounts) != 2 {
		t.Fatalf("expected 2 real mounts, got %+v", mounts)
	}
	if mounts[0].MountPoint != "/" || mounts[0].Device != "/dev/vda1" {
		t.Fatalf("root mount wrong: %+v", mounts[0])
	}
	if mounts[1].MountPoint != "/boot/efi" {
		t.Fatalf("efi mount wrong: %+v", mounts[1])
	}
}

func TestParseMountsEscapedSpace(t *testing.T) {
	fixture := "/dev/vdb1 /mnt/my\\040data xfs rw 0 0\n"
	mounts := ParseMounts([]byte(fixture))
	if len(mounts) != 1 || mounts[0].MountPoint != "/mnt/my data" {
		t.Fatalf("escaped space not decoded: %+v", mounts)
	}
}

func TestParseUptime(t *testing.T) {
	u, err := ParseUptime([]byte("123456.78 460840.31\n"))
	if err != nil || u != 123456.78 {
		t.Fatalf("uptime = %v err = %v", u, err)
	}
}
