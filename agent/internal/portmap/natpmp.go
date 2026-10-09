package portmap

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"time"
)

var readFile = os.ReadFile

// natPMP는 RFC 6886 NAT-PMP (UDP 5351). 요청·응답이 몇 바이트짜리라 직접 구현한다.
type natPMP struct{ addr string }

func (n *natPMP) Kind() string { return "natpmp" }

// call은 요청을 보내고 응답을 기다린다 (250ms부터 두 배씩, ctx가 끝날 때까지 재전송).
func (n *natPMP) call(ctx context.Context, req []byte, wantOp byte, size int) ([]byte, error) {
	c, err := net.Dial("udp", n.addr)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	wait := 250 * time.Millisecond
	buf := make([]byte, 64)
	for {
		if _, err := c.Write(req); err != nil {
			return nil, err
		}
		dl := time.Now().Add(wait)
		if d, ok := ctx.Deadline(); ok && d.Before(dl) {
			dl = d
		}
		_ = c.SetReadDeadline(dl)
		k, err := c.Read(buf)
		if err == nil && k >= size && buf[0] == 0 && buf[1] == wantOp {
			if code := binary.BigEndian.Uint16(buf[2:4]); code != 0 {
				return nil, fmt.Errorf("NAT-PMP 오류 코드 %d", code)
			}
			return buf[:k], nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err != nil {
			var ne net.Error
			if !errors.As(err, &ne) || !ne.Timeout() {
				return nil, err // 연결 거부 등: 공유기가 NAT-PMP를 안 함
			}
		}
		wait *= 2
	}
}

func (n *natPMP) ExternalIP(ctx context.Context) (net.IP, error) {
	r, err := n.call(ctx, []byte{0, 0}, 128, 12)
	if err != nil {
		return nil, err
	}
	return net.IPv4(r[8], r[9], r[10], r[11]), nil
}

func (n *natPMP) mapReq(m Mapping, lease time.Duration) ([]byte, byte, error) {
	var op byte
	switch m.Proto {
	case "udp":
		op = 1
	case "tcp":
		op = 2
	default:
		return nil, 0, fmt.Errorf("알 수 없는 프로토콜 %q", m.Proto)
	}
	req := make([]byte, 12)
	req[1] = op
	binary.BigEndian.PutUint16(req[4:], uint16(m.Port))
	binary.BigEndian.PutUint16(req[6:], uint16(m.Port))
	binary.BigEndian.PutUint32(req[8:], uint32(lease/time.Second))
	return req, op, nil
}

func (n *natPMP) Add(ctx context.Context, m Mapping, _ net.IP, lease time.Duration, _ string) error {
	req, op, err := n.mapReq(m, lease)
	if err != nil {
		return err
	}
	r, err := n.call(ctx, req, 128+op, 16)
	if err != nil {
		return err
	}
	if got := int(binary.BigEndian.Uint16(r[10:12])); got != m.Port {
		// 공유기가 다른 외부 포트를 줬다: 쓸 수 없으니 되돌린다
		_ = n.Delete(ctx, Mapping{m.Proto, m.Port})
		return fmt.Errorf("공유기가 외부 포트 %d 대신 %d를 줬습니다 (이미 다른 기기가 사용 중)", m.Port, got)
	}
	return nil
}

func (n *natPMP) Delete(ctx context.Context, m Mapping) error {
	req, op, err := n.mapReq(m, 0) // 수명 0 = 삭제
	if err != nil {
		return err
	}
	binary.BigEndian.PutUint16(req[6:], 0)
	_, err = n.call(ctx, req, 128+op, 16)
	return err
}
