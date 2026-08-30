package collector

import "testing"

const mountsSample = `/dev/sda1 / ext4 rw,relatime 0 0
proc /proc proc rw,nosuid,nodev,noexec,relatime 0 0
tmpfs /run tmpfs rw,nosuid,nodev 0 0
/dev/sdb1 /data xfs rw,relatime,attr2 0 0
/dev/sdb1 /data xfs rw,relatime,attr2 0 0
/dev/sdc1 /mnt/my\040disk ext4 rw 0 0
overlay / overlay overlay rw 0 0
`

func TestParseMounts_FiltersAndDedups(t *testing.T) {
	mounts := parseMounts([]byte(mountsSample))
	if len(mounts) != 3 {
		t.Fatalf("mounts = %+v, want 3 (/,/data,/mnt/my disk)", mounts)
	}
	if mounts[0].Mountpoint != "/" || mounts[0].FSType != "ext4" {
		t.Fatalf("mount0 = %+v", mounts[0])
	}
	if mounts[2].Mountpoint != "/mnt/my disk" {
		t.Fatalf("octal escape not decoded: %q", mounts[2].Mountpoint)
	}
}

func TestDiskCollect_StatfsValues(t *testing.T) {
	src := newFakeSources(map[string][]byte{"/proc/mounts": []byte(mountsSample)}, nil)
	src.Statfs = func(path string) (FsStat, error) {
		return FsStat{Blocks: 1000, Bfree: 300, Bavail: 250, Bsize: 4096}, nil
	}
	d := NewDisk(src)
	usages, err := d.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(usages) != 3 {
		t.Fatalf("usages = %d, want 3", len(usages))
	}
	u := usages[0]
	if u.Device != "/" {
		t.Fatalf("device = %q, want /", u.Device)
	}
	if u.Total != 1000*4096 {
		t.Fatalf("total = %d", u.Total)
	}
	// used = (Blocks - Bavail) * Bsize, 与 df 可用口径一致(排除 root 保留块)
	if u.Used != (1000-250)*4096 {
		t.Fatalf("used = %d", u.Used)
	}
}

func TestDiskCollect_AllStatfsFail(t *testing.T) {
	src := newFakeSources(map[string][]byte{"/proc/mounts": []byte(mountsSample)}, nil)
	src.Statfs = func(string) (FsStat, error) { return FsStat{}, ErrUnsupported }
	if _, err := NewDisk(src).Collect(); err == nil {
		t.Fatal("all statfs failing should error")
	}
}
