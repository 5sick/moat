#!/bin/bash
# 웹 설정 E2E: 텔레그램 설정 저장(패스키 재확인·형식 검사·비밀값 숨김) → 가짜 텔레그램으로 테스트 발송 → 초대 링크.
# 사용: tests/e2e/settings_flow.sh [moat-hub 경로]
set -u
export MOAT_LANG=ko # CLI 출력 언어 고정 (영어 확인은 따로)
HUB=${1:-build/hub/moat-hub}
HP=18860; TP=18861
BASE="http://localhost:$HP"
DIR=$(mktemp -d); trap 'kill $(jobs -p) 2>/dev/null; rm -rf "$DIR"' EXIT
PASS=0; FAIL=0
ok()   { echo "  ok   $1"; PASS=$((PASS+1)); }
bad()  { echo "  FAIL $1"; FAIL=$((FAIL+1)); }
for p in $HP $TP; do ss -ltn | grep -q ":$p " && { echo "포트 $p 사용 중"; exit 1; }; done

# 가짜 텔레그램: 받은 요청을 파일에 남긴다. chat_id=bad면 400
cat > "$DIR/tg.py" <<'PY'
import sys, urllib.parse
from http.server import BaseHTTPRequestHandler, HTTPServer
class H(BaseHTTPRequestHandler):
    def do_POST(self):
        body = self.rfile.read(int(self.headers.get("Content-Length", 0))).decode()
        q = urllib.parse.parse_qs(body)
        open(sys.argv[2], "a").write(self.path + " " + q.get("chat_id", [""])[0] + " " + q.get("text", [""])[0] + "\n")
        code = 400 if q.get("chat_id", [""])[0] == "-1" else 200
        self.send_response(code); self.end_headers(); self.wfile.write(b'{"ok":true}')
    def log_message(self, *a): pass
HTTPServer(("127.0.0.1", int(sys.argv[1])), H).serve_forever()
PY
python3 "$DIR/tg.py" $TP "$DIR/tg.log" &
cat > "$DIR/hub.json" <<JSON
{"public_url":"$BASE","cookie_domain":"localhost","database_path":"$DIR/hub.db","listen_port":$HP,
 "telegram":{"api_url":"http://127.0.0.1:$TP"}}
JSON
"$HUB" --config "$DIR/hub.json" >"$DIR/hub.log" 2>&1 &
for _ in $(seq 50); do curl -sf "$BASE/healthz" >/dev/null && break; sleep 0.1; done
python3 - "$DIR/hub.db" <<'PY'
import hashlib, sqlite3, sys, time
db = sqlite3.connect(sys.argv[1]); now = int(time.time())
db.execute("INSERT INTO users (id, email, created_at, allowed) VALUES (1, 'me@example.com', ?, 1)", (now,))
for tok, re in (("set-fresh-session-0123456789abcdef", now), ("set-stale-session-0123456789abcdef", now - 3600)):
    db.execute("INSERT INTO sessions VALUES (?, 1, 'passkey', ?, ?, ?, ?, ?, '', '', '')",
               (hashlib.sha256(tok.encode()).digest(), now, now, now + 3600, now + 7200, re))
db.commit()
PY
post() { curl -s -X POST -H "Cookie: moat_session=$1" -H "Origin: $BASE" -H 'Content-Type: application/json' -d "$3" "$BASE$2"; }
F=set-fresh-session-0123456789abcdef; S=set-stale-session-0123456789abcdef
TOKEN=123456789:AAAbbbCCCdddEEEfffGGGhhhIIIjjjKKK

echo "[settings_flow]"
out=$(post $S /api/settings/save "{\"telegram\":{\"bot_token\":\"$TOKEN\",\"chat_id\":\"42\"}}")
case "$out" in *reauth_required*) ok "설정 저장은 패스키 재확인 필요" ;; *) bad "재확인 ($out)";; esac
out=$(post $F /api/settings/save '{"telegram":{"bot_token":"nope","chat_id":"42"}}')
case "$out" in *error*) ok "잘못된 봇 토큰 거부" ;; *) bad "형식 검사 ($out)";; esac
out=$(post $F /api/settings/save "{\"telegram\":{\"bot_token\":\"$TOKEN\",\"chat_id\":\"42\"}}")
case "$out" in *'"ok"'*) ok "텔레그램 설정 저장" ;; *) bad "저장 ($out)";; esac
got=$(curl -s -H "Cookie: moat_session=$F" "$BASE/api/settings")
echo "$got" | grep -q "$TOKEN" && bad "봇 토큰이 API에 노출됨" || ok "봇 토큰은 일부만 표시"
echo "$got" | grep -q '"enabled":true' && ok "알림 활성 표시" || bad "활성 표시 ($got)"
out=$(post $F /api/settings/telegram-test '{}')
case "$out" in *'"ok"'*) ok "테스트 발송 성공" ;; *) bad "테스트 발송 ($out)";; esac
grep -q "/bot$TOKEN/sendMessage 42 \[Moat\] 테스트" "$DIR/tg.log" && ok "가짜 텔레그램이 받은 내용" || bad "수신 ($(cat "$DIR/tg.log"))"
post $F /api/settings/save '{"telegram":{"chat_id":"-1"}}' >/dev/null
out=$(post $F /api/settings/telegram-test '{}')
case "$out" in *chat*) ok "잘못된 chat id 안내" ;; *) bad "실패 안내 ($out)";; esac
out=$(post $F /api/settings/save '{"telegram":{"clear":true}}')
grep -q '"enabled": false' <<<"$(curl -s -H "Cookie: moat_session=$F" "$BASE/api/settings" | python3 -c 'import json,sys; print(json.dumps(json.load(sys.stdin)["telegram"]))')" \
    && ok "텔레그램 끄기" || bad "끄기"
out=$(post $F /api/invites/create '{"email":"friend@example.com"}')
case "$out" in *"$BASE/invite#"*) ok "웹에서 초대 링크 생성" ;; *) bad "초대 ($out)";; esac
ev=$(sqlite3 "$DIR/hub.db" "SELECT group_concat(event, ',') FROM audit_log")
case "$ev" in *settings_changed*invite_created*) ok "감사 로그" ;; *) bad "감사 로그 ($ev)";; esac
curl -s "$BASE/auth/methods" | grep -q '"google":false' && ok "Google 미설정 표시" || bad "methods"

echo "$PASS passed, $FAIL failed"
[ "$FAIL" = 0 ]
