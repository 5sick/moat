// Package health는 이 서버의 서비스가 응답하는지 로컬에서 확인한다.
// (입구·Hub에서 직접 닿지 않는 서버도 확인할 수 있도록 Agent가 확인하고 결과만 보고)
package health

import (
	"context"
	"crypto/tls"
	"net/http"
	"sync"
	"time"
)

// Check는 Hub가 알려 준 확인 대상이다.
type Check struct {
	Upstream string `json:"upstream"`
	TLS      int    `json:"tls"` // 0 http, 1 https, 2 https(검증 안 함)
	Path     string `json:"path"`
}

type Result struct {
	Upstream string  `json:"upstream"`
	OK       bool    `json:"ok"`
	Status   int     `json:"status"`
	Error    string  `json:"error,omitempty"`
	Ms       float64 `json:"ms"`
}

var (
	strict   = &http.Client{Timeout: 5 * time.Second, CheckRedirect: noRedirect}
	insecure = &http.Client{Timeout: 5 * time.Second, CheckRedirect: noRedirect,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}} //nolint:gosec // 사용자가 고른 자체 서명 업스트림
)

func noRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

// Run은 대상마다 확인한다. 응답이 있고 5xx가 아니면 정상 (401·302도 살아 있음).
func Run(ctx context.Context, checks []Check) []Result {
	out := make([]Result, len(checks))
	var wg sync.WaitGroup
	for i, c := range checks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			scheme, client := "http://", strict
			if c.TLS > 0 {
				scheme = "https://"
			}
			if c.TLS == 2 {
				client = insecure
			}
			path := c.Path
			if path == "" {
				path = "/"
			}
			r := Result{Upstream: c.Upstream}
			start := time.Now()
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, scheme+c.Upstream+path, nil)
			resp, err := client.Do(req)
			r.Ms = float64(time.Since(start).Microseconds()) / 1000
			if err != nil {
				r.Error = err.Error()
			} else {
				resp.Body.Close()
				r.Status = resp.StatusCode
				r.OK = resp.StatusCode < 500
				if !r.OK {
					r.Error = resp.Status
				}
			}
			out[i] = r
		}()
	}
	wg.Wait()
	return out
}
