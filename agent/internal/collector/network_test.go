package collector

import (
	"strings"
	"testing"
	"time"
)

const netdevSample = `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo: 1000000    5000    0    0    0     0          0         0  1000000    5000    0    0    0     0       0          0
  eth0: 5000000000    8000    0    0    0     0          0         0 2000000000    7000    0    0    0     0       0          0
  eth1: 1000    10    0    0    0     0          0         0 2000    20    0    0    0     0       0          0
`

const procNetTCPSample = `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100007F:0035 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 12345 1 0000000000000000 100 0 0 10 0
   1: 0100007F:1F90 0100007F:C350 01 00000000:00000000 02:000B2B0F 00000000  1000        0 12346 1 0000000000000000 20 4 30 10 -1
`

const procNetUDPSample = `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode ref pointer drops
   0: 00000000:007B 00000000:0000 07 00000000:00000000 00:00000000 00000000     0        0 23456 2 0000000000000000 0
`

func TestParseNetDev_SumsNonLoopback(t *testing.T) {
	m, err := parseNetDev([]byte(netdevSample))
	if err != nil {
		t.Fatalf("parseNetDev: %v", err)
	}
	if len(m) != 2 {
		t.Fatalf("ifaces = %v, want eth0+eth1", keysOf(m))
	}
	sum := sumNetDev(m)
	if sum.rxBytes != 5000000000+1000 || sum.txBytes != 2000000000+2000 {
		t.Fatalf("sum = %+v", sum)
	}
}

func keysOf[M ~map[string]V, V any](m M) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestNetworkCollect_SpeedsAndConnections(t *testing.T) {
	base := time.Unix(1718900000, 0)
	calls := 0
	clock := func() time.Time {
		calls++
		if calls <= 1 {
			return base
		}
		return base.Add(2 * time.Second)
	}
	files := map[string][]byte{
		"/proc/net/dev":  []byte(netdevSample),
		"/proc/net/tcp":  []byte(procNetTCPSample),
		"/proc/net/udp":  []byte(procNetUDPSample),
		"/proc/net/tcp6": []byte("  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n   0: 00000000000000000000000000000001:0035 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 1 1 0 100 0 0 10 0\n"),
	}
	n := NewNetwork(newFakeSources(files, nil))
	n.now = clock

	info1, err := n.Collect()
	if err != nil {
		t.Fatalf("first Collect: %v", err)
	}
	if info1.RxSpeed != 0 || info1.TxSpeed != 0 {
		t.Fatalf("first sample speeds must be 0, got %v/%v", info1.RxSpeed, info1.TxSpeed)
	}
	if info1.TCPConnections != 3 { // tcp 2 行 + tcp6 1 行
		t.Fatalf("tcp conns = %d, want 3", info1.TCPConnections)
	}
	if info1.UDPConnections != 1 {
		t.Fatalf("udp conns = %d, want 1", info1.UDPConnections)
	}

	// 第二次: rx 累计 +400 字节 / 2s = 200 B/s
	files["/proc/net/dev"] = []byte(strings.Replace(netdevSample, "5000000000", "5000000400", 1))
	n.src = newFakeSources(files, nil)
	n.now = clock
	info2, err := n.Collect()
	if err != nil {
		t.Fatalf("second Collect: %v", err)
	}
	if info2.RxSpeed != 200 {
		t.Fatalf("rx speed = %d, want 200", info2.RxSpeed)
	}
	if info2.TxSpeed != 0 {
		t.Fatalf("tx speed = %d, want 0", info2.TxSpeed)
	}
}

func TestRate_CounterWrapDropsSample(t *testing.T) {
	if got := rate(100, 50, time.Second); got != 0 {
		t.Fatalf("wrap rate = %d, want 0", got)
	}
	if got := rate(0, 300, 3*time.Second); got != 100 {
		t.Fatalf("rate = %d, want 100", got)
	}
}

func TestCountProcNetLines(t *testing.T) {
	if got := countProcNetLines([]byte(procNetTCPSample)); got != 2 {
		t.Fatalf("lines = %d, want 2", got)
	}
	if got := countProcNetLines([]byte("  sl  local_address rem_address st\n")); got != 0 {
		t.Fatalf("header-only = %d, want 0", got)
	}
}
