// Package portmap은 집 공유기에 포트포워딩을 요청한다 (UPnP IGD, NAT-PMP).
// 공인 IP가 공유기에 있고 공유기가 이 기능을 켜 두었을 때만 된다. 공유기의 외부 주소가
// 사설·CGNAT 대역이면(통신사 NAT 뒤) 열어도 밖에서 닿지 않으므로 CGNAT로 알린다.
package portmap

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

// Mapping은 열 포트 하나 (외부 포트 = 내부 포트).
type Mapping struct {
	Proto string `json:"proto"` // tcp | udp
	Port  int    `json:"port"`
}

func (m Mapping) String() string { return fmt.Sprintf("%s/%d", m.Proto, m.Port) }

// Allowed는 Hub가 요청해도 열어 주는 포트 (입구 80/443, WireGuard 51820).
func Allowed(m Mapping) bool {
	switch m {
	case Mapping{"tcp", 80}, Mapping{"tcp", 443}, Mapping{"udp", 51820}:
		return true
	}
	return false
}

// Gateway는 포트포워딩을 받아 주는 공유기다.
type Gateway interface {
	Kind() string // upnp | natpmp
	ExternalIP(ctx context.Context) (net.IP, error)
	Add(ctx context.Context, m Mapping, internal net.IP, lease time.Duration, desc string) error
	Delete(ctx context.Context, m Mapping) error
}

// ErrNoGateway: 공유기를 찾지 못했거나 UPnP·NAT-PMP를 지원하지 않는다.
var ErrNoGateway = errors.New("포트포워딩을 지원하는 공유기를 찾지 못했습니다 (UPnP/NAT-PMP)")

// Options는 시험에서 주소를 바꿀 때 쓴다.
type Options struct {
	Gateway net.IP // 비우면 기본 경로의 게이트웨이
	NATPMP  string // 비우면 <게이트웨이>:5351
	SSDP    string // 비우면 239.255.255.250:1900
	Timeout time.Duration
}

// Discover는 NAT-PMP를 먼저(빠름), 안 되면 UPnP를 찾아본다.
func Discover(ctx context.Context, o Options) (Gateway, error) {
	if o.Timeout == 0 {
		o.Timeout = 3 * time.Second
	}
	gw := o.Gateway
	if gw == nil {
		var err error
		if gw, err = DefaultGateway("/proc/net/route"); err != nil {
			return nil, ErrNoGateway
		}
	}
	natpmpAddr := o.NATPMP
	if natpmpAddr == "" {
		natpmpAddr = net.JoinHostPort(gw.String(), "5351")
	}
	pm := &natPMP{addr: natpmpAddr}
	cctx, cancel := context.WithTimeout(ctx, o.Timeout/2)
	_, err := pm.ExternalIP(cctx)
	cancel()
	if err == nil {
		return pm, nil
	}
	ssdp := o.SSDP
	if ssdp == "" {
		ssdp = "239.255.255.250:1900"
	}
	cctx, cancel = context.WithTimeout(ctx, o.Timeout)
	defer cancel()
	if u, err := discoverUPnP(cctx, ssdp); err == nil {
		return u, nil
	}
	return nil, ErrNoGateway
}

// LocalIPFor는 dst로 나갈 때 쓰는 이 서버의 주소 (포트포워딩의 내부 주소).
func LocalIPFor(dst string) (net.IP, error) {
	c, err := net.Dial("udp", dst)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	return c.LocalAddr().(*net.UDPAddr).IP, nil
}

// IsCGNAT는 공유기 외부 주소가 인터넷에서 닿지 않는 대역인지 (통신사 NAT·이중 NAT).
func IsCGNAT(ip net.IP) bool {
	if ip == nil {
		return false
	}
	_, cgn, _ := net.ParseCIDR("100.64.0.0/10")
	return ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || cgn.Contains(ip) || ip.IsUnspecified()
}

// DefaultGateway는 /proc/net/route에서 기본 경로의 게이트웨이를 읽는다.
func DefaultGateway(path string) (net.IP, error) {
	b, err := readFile(path)
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(string(b), "\n")[1:] {
		f := strings.Fields(line)
		if len(f) < 3 || f[1] != "00000000" || f[2] == "00000000" {
			continue
		}
		var v uint32
		if _, err := fmt.Sscanf(f[2], "%x", &v); err != nil {
			continue
		}
		// 리틀 엔디언 hex
		return net.IPv4(byte(v), byte(v>>8), byte(v>>16), byte(v>>24)), nil
	}
	return nil, errors.New("기본 경로가 없습니다")
}
