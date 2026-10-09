package portmap

import (
	"bufio"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// upnp는 UPnP IGD (WANIPConnection / WANPPPConnection)의 SOAP 동작 세 가지만 쓴다.
type upnp struct {
	control string // 제어 URL
	service string // 서비스 형식 (urn:schemas-upnp-org:service:WANIPConnection:1 등)
	client  *http.Client
}

func (u *upnp) Kind() string { return "upnp" }

var upnpServices = []string{
	"urn:schemas-upnp-org:service:WANIPConnection:2",
	"urn:schemas-upnp-org:service:WANIPConnection:1",
	"urn:schemas-upnp-org:service:WANPPPConnection:1",
}

// discoverUPnP는 SSDP M-SEARCH로 공유기를 찾고 기기 설명에서 제어 URL을 읽는다.
func discoverUPnP(ctx context.Context, ssdp string) (*upnp, error) {
	pc, err := net.ListenPacket("udp4", ":0")
	if err != nil {
		return nil, err
	}
	defer pc.Close()
	dst, err := net.ResolveUDPAddr("udp4", ssdp)
	if err != nil {
		return nil, err
	}
	for _, st := range []string{"urn:schemas-upnp-org:device:InternetGatewayDevice:1", "urn:schemas-upnp-org:device:InternetGatewayDevice:2"} {
		msg := "M-SEARCH * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nMAN: \"ssdp:discover\"\r\nMX: 2\r\nST: " + st + "\r\n\r\n"
		_, _ = pc.WriteTo([]byte(msg), dst)
	}
	if d, ok := ctx.Deadline(); ok {
		_ = pc.SetReadDeadline(d)
	}
	buf := make([]byte, 2048)
	tried := map[string]bool{}
	for {
		k, _, err := pc.ReadFrom(buf)
		if err != nil {
			return nil, ErrNoGateway
		}
		resp, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(buf[:k])), nil)
		if err != nil {
			continue
		}
		loc := resp.Header.Get("Location")
		if loc == "" || tried[loc] {
			continue
		}
		tried[loc] = true
		if u, err := describe(ctx, loc); err == nil {
			return u, nil
		}
	}
}

type xmlService struct {
	ServiceType string `xml:"serviceType"`
	ControlURL  string `xml:"controlURL"`
}
type xmlDevice struct {
	Services []xmlService `xml:"serviceList>service"`
	Devices  []xmlDevice  `xml:"deviceList>device"`
}
type xmlRoot struct {
	URLBase string    `xml:"URLBase"`
	Device  xmlDevice `xml:"device"`
}

func findService(d xmlDevice) (xmlService, bool) {
	for _, want := range upnpServices {
		for _, s := range d.Services {
			if s.ServiceType == want {
				return s, true
			}
		}
	}
	for _, c := range d.Devices {
		if s, ok := findService(c); ok {
			return s, true
		}
	}
	return xmlService{}, false
}

func describe(ctx context.Context, loc string) (*upnp, error) {
	client := &http.Client{Timeout: 3 * time.Second}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, loc, nil)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var root xmlRoot
	if err := xml.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&root); err != nil {
		return nil, err
	}
	s, ok := findService(root.Device)
	if !ok {
		return nil, errors.New("WANIPConnection 서비스가 없습니다")
	}
	base, _ := url.Parse(loc)
	if root.URLBase != "" {
		if b, err := url.Parse(root.URLBase); err == nil {
			base = b
		}
	}
	ctl, err := base.Parse(s.ControlURL)
	if err != nil {
		return nil, err
	}
	// 제어 URL은 공유기(같은 사설망)여야 한다 — 설명 문서가 엉뚱한 곳을 가리키지 않게
	if ctl.Hostname() != base.Hostname() {
		return nil, errors.New("제어 URL이 공유기 주소가 아닙니다")
	}
	return &upnp{control: ctl.String(), service: s.ServiceType, client: client}, nil
}

type soapFault struct {
	Code        int    `xml:"Body>Fault>detail>UPnPError>errorCode"`
	Description string `xml:"Body>Fault>detail>UPnPError>errorDescription"`
}

func (u *upnp) soap(ctx context.Context, action string, args [][2]string) ([]byte, error) {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body>`)
	fmt.Fprintf(&b, `<u:%s xmlns:u="%s">`, action, u.service)
	for _, a := range args {
		b.WriteString("<" + a[0] + ">")
		_ = xml.EscapeText(&b, []byte(a[1]))
		b.WriteString("</" + a[0] + ">")
	}
	fmt.Fprintf(&b, `</u:%s></s:Body></s:Envelope>`, action)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, u.control, strings.NewReader(b.String()))
	req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
	req.Header.Set("SOAPAction", `"`+u.service+"#"+action+`"`)
	resp, err := u.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		var f soapFault
		if xml.Unmarshal(body, &f) == nil && f.Code != 0 {
			return nil, &upnpError{f.Code, f.Description}
		}
		return nil, fmt.Errorf("UPnP %s: HTTP %d", action, resp.StatusCode)
	}
	return body, nil
}

type upnpError struct {
	Code int
	Desc string
}

func (e *upnpError) Error() string { return fmt.Sprintf("UPnP 오류 %d %s", e.Code, e.Desc) }

func (u *upnp) ExternalIP(ctx context.Context) (net.IP, error) {
	body, err := u.soap(ctx, "GetExternalIPAddress", nil)
	if err != nil {
		return nil, err
	}
	var r struct {
		IP string `xml:"Body>GetExternalIPAddressResponse>NewExternalIPAddress"`
	}
	if err := xml.Unmarshal(body, &r); err != nil {
		return nil, err
	}
	ip := net.ParseIP(strings.TrimSpace(r.IP))
	if ip == nil {
		return nil, errors.New("공유기가 외부 주소를 알려 주지 않습니다")
	}
	return ip, nil
}

func (u *upnp) Add(ctx context.Context, m Mapping, internal net.IP, lease time.Duration, desc string) error {
	args := func(l time.Duration) [][2]string {
		return [][2]string{
			{"NewRemoteHost", ""},
			{"NewExternalPort", strconv.Itoa(m.Port)},
			{"NewProtocol", strings.ToUpper(m.Proto)},
			{"NewInternalPort", strconv.Itoa(m.Port)},
			{"NewInternalClient", internal.String()},
			{"NewEnabled", "1"},
			{"NewPortMappingDescription", desc},
			{"NewLeaseDuration", strconv.Itoa(int(l / time.Second))},
		}
	}
	_, err := u.soap(ctx, "AddPortMapping", args(lease))
	var ue *upnpError
	if errors.As(err, &ue) && ue.Code == 725 { // OnlyPermanentLeasesSupported
		_, err = u.soap(ctx, "AddPortMapping", args(0))
	}
	if errors.As(err, &ue) && ue.Code == 718 { // ConflictInMappingEntry
		return fmt.Errorf("공유기에서 %s 포트를 이미 다른 기기가 쓰고 있습니다", m)
	}
	return err
}

func (u *upnp) Delete(ctx context.Context, m Mapping) error {
	_, err := u.soap(ctx, "DeletePortMapping", [][2]string{
		{"NewRemoteHost", ""}, {"NewExternalPort", strconv.Itoa(m.Port)}, {"NewProtocol", strings.ToUpper(m.Proto)},
	})
	return err
}
