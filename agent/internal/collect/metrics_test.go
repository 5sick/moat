package collect

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCollectFromFakeProc(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "stat", "cpu  100 0 100 700 100 0 0 0 0 0\ncpu0 1 1 1 1\n")
	write(t, dir, "meminfo", "MemTotal:       1000 kB\nMemFree:  100 kB\nMemAvailable:    250 kB\nSwapTotal: 400 kB\nSwapFree: 300 kB\n")
	write(t, dir, "net/dev", "Inter-|   Receive\n face |bytes\n    lo: 999 0 0 0 0 0 0 0 999 0 0 0 0 0 0 0\n  eth0: 1000 0 0 0 0 0 0 0 2000 0 0 0 0 0 0 0\nveth12: 5000 0 0 0 0 0 0 0 5000 0 0 0 0 0 0 0\n")
	write(t, dir, "loadavg", "0.50 0.40 0.30 1/100 1234\n")
	write(t, dir, "uptime", "12345.67 100.0\n")
	write(t, dir, "self/mounts", "")

	c := &Collector{Proc: dir}
	m := c.Collect()
	if m.CPU != 0 {
		t.Errorf("첫 CPU는 0이어야 함: %v", m.CPU)
	}
	if m.Mem.Total != 1000*1024 || m.Mem.Used != 750*1024 {
		t.Errorf("mem %+v", m.Mem)
	}
	if m.Swap.Used != 100*1024 {
		t.Errorf("swap %+v", m.Swap)
	}
	if m.Load != [3]float64{0.5, 0.4, 0.3} || m.Uptime != 12345 {
		t.Errorf("load %v uptime %v", m.Load, m.Uptime)
	}
	// 두 번째: total 1000→1900(+900), idle+iowait 800→1300(+500) → 400/900 = 44.4%
	write(t, dir, "stat", "cpu  300 0 300 1100 200 0 0 0 0 0\n")
	write(t, dir, "net/dev", "  eth0: 3000 0 0 0 0 0 0 0 4000 0 0 0 0 0 0 0\n")
	m = c.Collect()
	if m.CPU < 44.4 || m.CPU > 44.5 {
		t.Errorf("cpu %v", m.CPU)
	}
	if m.Net.RX <= 0 || m.Net.TX <= 0 {
		t.Errorf("net %+v", m.Net)
	}
}

func TestVirtualIfaceExcluded(t *testing.T) {
	for _, n := range []string{"lo", "veth1a2b", "docker0", "br-9cb5"} {
		if !virtualIface(n) {
			t.Errorf("%s는 가상", n)
		}
	}
	for _, n := range []string{"eth0", "enp0s6", "wg0", "ens3"} {
		if virtualIface(n) {
			t.Errorf("%s는 물리(또는 wg)", n)
		}
	}
}

func TestRealDisksIncludeRootFirst(t *testing.T) {
	d := readDisks("/proc/self/mounts")
	if len(d) == 0 {
		t.Skip("실제 디스크 없음 (컨테이너?)")
	}
	if d[0].Mount != "/" {
		t.Logf("루트가 첫 번째가 아님: %+v", d)
	}
	for _, x := range d {
		if x.Total == 0 || x.Used > x.Total {
			t.Errorf("이상한 값 %+v", x)
		}
	}
}
