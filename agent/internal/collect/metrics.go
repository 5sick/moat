// Package collect는 /proc·statfs·systemd·Docker에서 노드 상태를 읽는다.
// 외부 라이브러리 없이 /proc를 직접 파싱한다 (1GB 노드에서도 가볍게).
package collect

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Metrics는 Hub에 10초마다 보내는 값이다 (JSON 형식은 Hub의 parseSample과 맞춘다).
type Metrics struct {
	Type   string     `json:"type"`
	CPU    float64    `json:"cpu"`
	Mem    UsedTotal  `json:"mem"`
	Swap   UsedTotal  `json:"swap"`
	Disks  []Disk     `json:"disks"`
	Net    Net        `json:"net"`
	Load   [3]float64 `json:"load"`
	Uptime int64      `json:"uptime"`
}

type UsedTotal struct {
	Used  uint64 `json:"used"`
	Total uint64 `json:"total"`
}

type Disk struct {
	Mount string `json:"mount"`
	Used  uint64 `json:"used"`
	Total uint64 `json:"total"`
}

// Net은 초당 바이트 (물리 인터페이스 합계).
type Net struct {
	RX float64 `json:"rx"`
	TX float64 `json:"tx"`
}

// Collector는 CPU·네트워크처럼 이전 값과의 차이가 필요한 항목의 상태를 들고 있다.
type Collector struct {
	Proc string // 기본 /proc (테스트에서 바꿈)

	prevCPU  cpuTimes
	prevNet  [2]uint64
	prevTime time.Time
}

func NewCollector() *Collector { return &Collector{Proc: "/proc"} }

// Collect는 지금 값을 읽는다. 첫 호출의 CPU·네트워크는 0이다.
func (c *Collector) Collect() Metrics {
	m := Metrics{Type: "metrics", Disks: []Disk{}}
	now := time.Now()

	if cur, err := readCPU(filepath.Join(c.Proc, "stat")); err == nil {
		if c.prevCPU.total > 0 {
			m.CPU = cur.usagePercent(c.prevCPU)
		}
		c.prevCPU = cur
	}
	if mem, err := readMeminfo(filepath.Join(c.Proc, "meminfo")); err == nil {
		m.Mem = UsedTotal{Used: mem["MemTotal"] - mem["MemAvailable"], Total: mem["MemTotal"]}
		m.Swap = UsedTotal{Used: mem["SwapTotal"] - mem["SwapFree"], Total: mem["SwapTotal"]}
	}
	m.Disks = readDisks(filepath.Join(c.Proc, "self/mounts"))
	if rx, tx, err := readNetDev(filepath.Join(c.Proc, "net/dev")); err == nil {
		if !c.prevTime.IsZero() {
			dt := now.Sub(c.prevTime).Seconds()
			if dt > 0 && rx >= c.prevNet[0] && tx >= c.prevNet[1] {
				m.Net = Net{RX: float64(rx-c.prevNet[0]) / dt, TX: float64(tx-c.prevNet[1]) / dt}
			}
		}
		c.prevNet = [2]uint64{rx, tx}
	}
	c.prevTime = now
	m.Load = readLoad(filepath.Join(c.Proc, "loadavg"))
	m.Uptime = readUptime(filepath.Join(c.Proc, "uptime"))
	return m
}

type cpuTimes struct{ idle, total uint64 }

func (cur cpuTimes) usagePercent(prev cpuTimes) float64 {
	dt := float64(cur.total - prev.total)
	if cur.total <= prev.total || dt <= 0 {
		return 0
	}
	di := float64(cur.idle - prev.idle)
	return 100 * (dt - di) / dt
}

func readCPU(path string) (cpuTimes, error) {
	f, err := os.Open(path)
	if err != nil {
		return cpuTimes{}, err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		fields := strings.Fields(s.Text())
		if len(fields) < 5 || fields[0] != "cpu" {
			continue
		}
		var t cpuTimes
		for i, v := range fields[1:] {
			n, _ := strconv.ParseUint(v, 10, 64)
			if i >= 8 { // guest, guest_nice는 user에 이미 포함
				break
			}
			t.total += n
			if i == 3 || i == 4 { // idle, iowait
				t.idle += n
			}
		}
		return t, nil
	}
	return cpuTimes{}, os.ErrNotExist
}

// readMeminfo는 값을 바이트로 돌려준다.
func readMeminfo(path string) (map[string]uint64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string]uint64{}
	s := bufio.NewScanner(f)
	for s.Scan() {
		k, rest, ok := strings.Cut(s.Text(), ":")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		n, _ := strconv.ParseUint(fields[0], 10, 64)
		if len(fields) > 1 && fields[1] == "kB" {
			n *= 1024
		}
		out[k] = n
	}
	return out, nil
}

// 실제 저장장치 파일시스템만 (tmpfs·overlay·squashfs 등 제외)
var realFS = map[string]bool{"ext4": true, "ext3": true, "xfs": true, "btrfs": true, "zfs": true, "f2fs": true, "vfat": false}

func readDisks(mountsPath string) []Disk {
	disks := []Disk{}
	f, err := os.Open(mountsPath)
	if err != nil {
		return disks
	}
	defer f.Close()
	seen := map[string]bool{}
	s := bufio.NewScanner(f)
	for s.Scan() {
		fields := strings.Fields(s.Text())
		if len(fields) < 3 || !realFS[fields[2]] {
			continue
		}
		dev, mount := fields[0], unescapeMount(fields[1])
		if seen[dev] { // 같은 장치를 여러 곳에 마운트(bind)한 경우 한 번만
			continue
		}
		var st syscall.Statfs_t
		if err := syscall.Statfs(mount, &st); err != nil || st.Blocks == 0 {
			continue
		}
		seen[dev] = true
		bs := uint64(st.Bsize)
		total := st.Blocks * bs
		used := total - st.Bfree*bs
		d := Disk{Mount: mount, Used: used, Total: total}
		if mount == "/" {
			disks = append([]Disk{d}, disks...)
		} else {
			disks = append(disks, d)
		}
		if len(disks) >= 16 {
			break
		}
	}
	return disks
}

func unescapeMount(s string) string {
	return strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`).Replace(s)
}

// 가상 인터페이스는 집계에서 제외 (Docker 트래픽 이중 집계 방지)
func virtualIface(name string) bool {
	for _, p := range []string{"lo", "veth", "docker", "br-", "virbr", "cni", "flannel", "tun", "tap"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

func readNetDev(path string) (rx, tx uint64, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		name, rest, ok := strings.Cut(s.Text(), ":")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		if virtualIface(name) {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) < 9 {
			continue
		}
		r, _ := strconv.ParseUint(fields[0], 10, 64)
		t, _ := strconv.ParseUint(fields[8], 10, 64)
		rx += r
		tx += t
	}
	return rx, tx, nil
}

func readLoad(path string) [3]float64 {
	var l [3]float64
	b, err := os.ReadFile(path)
	if err != nil {
		return l
	}
	fields := strings.Fields(string(b))
	for i := 0; i < 3 && i < len(fields); i++ {
		l[i], _ = strconv.ParseFloat(fields[i], 64)
	}
	return l
}

func readUptime(path string) int64 {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(b))
	if len(fields) == 0 {
		return 0
	}
	v, _ := strconv.ParseFloat(fields[0], 64)
	return int64(v)
}
