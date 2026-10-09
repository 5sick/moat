// Package features는 Hub가 정한 기능 켜기/끄기를 들고 있다 (welcome·config 메시지로 갱신).
// Hub에 접속하기 전에는 모두 켜진 기본값을 쓴다.
package features

import (
	"encoding/json"
	"sync/atomic"
)

type Set struct {
	Monitoring    bool `json:"monitoring"`
	ServiceChecks bool `json:"service_checks"`
	Terminal      bool `json:"terminal"`
	Security      struct {
		SSH     bool `json:"ssh"`
		Sudo    bool `json:"sudo"`
		Account bool `json:"account"`
		Files   bool `json:"files"`
		Ports   bool `json:"ports"`
	} `json:"security"`
}

func Default() Set {
	var s Set
	s.Monitoring, s.ServiceChecks, s.Terminal = true, true, true
	s.Security.SSH, s.Security.Sudo, s.Security.Account, s.Security.Files, s.Security.Ports = true, true, true, true, true
	return s
}

var current atomic.Pointer[Set]

func init() {
	d := Default()
	current.Store(&d)
}

// Get은 지금 설정이다.
func Get() Set { return *current.Load() }

// Apply는 Hub가 보낸 features JSON을 적용한다 (빠진 값은 켜짐).
func Apply(raw json.RawMessage) error {
	s := Default()
	if err := json.Unmarshal(raw, &s); err != nil {
		return err
	}
	current.Store(&s)
	return nil
}

// SecurityKind는 보안 사건 종류가 켜져 있는지 본다.
func (s Set) SecurityKind(kind string) bool {
	switch kind {
	case "ssh_login", "ssh_fail":
		return s.Security.SSH
	case "sudo", "su":
		return s.Security.Sudo
	case "account":
		return s.Security.Account
	case "file_changed":
		return s.Security.Files
	case "port_opened":
		return s.Security.Ports
	}
	return true
}
