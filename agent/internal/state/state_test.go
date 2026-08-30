package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/YCJE/XProbe/internal/model"
)

func fixedNow(y int, m time.Month, d int) func() time.Time {
	return func() time.Time { return time.Date(y, m, d, 12, 0, 0, 0, time.UTC) }
}

func TestTracker_FreshInstallBaseline(t *testing.T) {
	// 首次安装: 当前读数为基线, 不把安装前流量计入
	tr, _ := Load("", fixedNow(2026, 8, 1))
	m := tr.Update(5000, 3000)
	if m.RxBytes != 0 || m.TxBytes != 0 {
		t.Fatalf("fresh baseline should be 0, got %+v", m)
	}
	m = tr.Update(6000, 3500)
	if m.RxBytes != 1000 || m.TxBytes != 500 {
		t.Fatalf("increment = %+v, want rx=1000 tx=500", m)
	}
}

func TestTracker_UTCYearMonthBoundary(t *testing.T) {
	tr, _ := Load("", fixedNow(2026, 8, 31))
	tr.Update(1000, 500)
	tr.Update(2000, 800) // 8 月累计 rx=1000 tx=300

	// 跨到 9 月: 归零重新累计(设计文档 4.4 月界 UTC)
	tr.now = fixedNow(2026, 9, 1)
	m := tr.Update(3000, 1200)
	if m.Month != "2026-09" {
		t.Fatalf("month = %s, want 2026-09", m.Month)
	}
	if m.RxBytes != 1000 || m.TxBytes != 400 {
		t.Fatalf("after rollover = %+v, want rx=1000 tx=400 (delta only)", m)
	}
}

func TestTracker_CounterResetAfterReboot(t *testing.T) {
	tr, _ := Load("", fixedNow(2026, 8, 10))
	tr.Update(100<<30, 50<<30) // 长期运行, 累计很大
	tr.Update((100<<30)+1000, (50<<30)+500)

	// 重启: /proc/net/dev 计数器从 0 重新增长, cur < last → 增量取 cur 全量
	m := tr.Update(2<<30, 1<<30)
	if m.RxBytes != 1000+2<<30 {
		t.Fatalf("rx = %d, want %d", m.RxBytes, 1000+2<<30)
	}
	if m.TxBytes != 500+1<<30 {
		t.Fatalf("tx = %d, want %d", m.TxBytes, 500+1<<30)
	}
}

func TestTracker_PersistAcrossReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	tr1, err := Load(path, fixedNow(2026, 8, 15))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	tr1.Update(1000, 500)
	tr1.Update(3000, 1500)

	tr2, err := Load(path, fixedNow(2026, 8, 15))
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	m := tr2.Update(4000, 2000)
	if m.RxBytes != 3000 || m.TxBytes != 1500 {
		t.Fatalf("after reload = %+v, want rx=3000 tx=1500", m)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("state file missing: %v", err)
	}
}

func TestTracker_CorruptStateStartsOver(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	os.WriteFile(path, []byte("{corrupt"), 0o600)
	tr, err := Load(path, fixedNow(2026, 8, 15))
	if err != nil {
		t.Fatalf("Load corrupt: %v", err)
	}
	m := tr.Update(1000, 500)
	if m.Month != "2026-08" || m.RxBytes != 0 {
		t.Fatalf("corrupt state should restart cleanly, got %+v", m)
	}
}

func TestMonthOf_UTC(t *testing.T) {
	// 2026-08-31 23:30 UTC+8 = 2026-08-31 15:30 UTC → 2026-08
	ts := time.Date(2026, 8, 31, 23, 30, 0, 0, time.FixedZone("CST", 8*3600))
	if got := model.MonthOf(ts); got != "2026-08" {
		t.Fatalf("month = %s, want 2026-08", got)
	}
}
