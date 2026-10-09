package health

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			http.Redirect(w, r, "/x", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer ok.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(502) }))
	defer bad.Close()
	tlsSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer tlsSrv.Close()
	h := func(u string) string { return strings.TrimPrefix(strings.TrimPrefix(u, "http://"), "https://") }
	res := Run(context.Background(), []Check{
		{Upstream: h(ok.URL)}, {Upstream: h(ok.URL), Path: "/login"}, {Upstream: h(bad.URL)},
		{Upstream: "127.0.0.1:1"}, {Upstream: h(tlsSrv.URL), TLS: 2}, {Upstream: h(tlsSrv.URL), TLS: 1},
	})
	want := []bool{true, true, false, false, true, false}
	for i, r := range res {
		if r.OK != want[i] {
			t.Errorf("%d: %+v", i, r)
		}
	}
	if res[1].Status != 302 {
		t.Errorf("리다이렉트는 따라가지 않음: %+v", res[1])
	}
}
