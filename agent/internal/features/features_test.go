package features

import "testing"

func TestApplyKeepsMissingAsOn(t *testing.T) {
	if !Get().Terminal {
		t.Fatal("기본은 켜짐")
	}
	if err := Apply([]byte(`{"terminal":false,"security":{"sudo":false}}`)); err != nil {
		t.Fatal(err)
	}
	s := Get()
	if s.Terminal || !s.Monitoring || s.Security.Sudo || !s.Security.SSH {
		t.Fatalf("%+v", s)
	}
	if s.SecurityKind("su") || !s.SecurityKind("ssh_login") || !s.SecurityKind("unknown") {
		t.Fatal("SecurityKind")
	}
	if Apply([]byte(`not json`)) == nil || Get().Terminal {
		t.Fatal("잘못된 JSON은 무시하고 기존 값 유지")
	}
	Apply([]byte(`{}`))
}
