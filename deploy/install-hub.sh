#!/bin/bash
# Moat Hub 설치. 사용:
#   sudo ./install-hub.sh <moat-hub 바이너리> [init 옵션...]
# 예:
#   sudo ./install-hub.sh build/hub/moat-hub --public-url https://moat.example.com --email me@example.com \
#        --listen 10.200.0.2:8700 --trusted-proxy 10.200.0.1 \
#        --google-client-id ... --google-client-secret ...
# Agent 바이너리는 저장소의 dist/(make agent)가 있으면 /usr/local/share/moat/agent 로 함께 설치한다 (join.sh가 내려줌).
# 이미 /etc/moat/hub.json이 있으면 설정은 건드리지 않고 바이너리·서비스만 갱신한다.
# Ubuntu 26.04(sudo-rs, uutils) / Rocky 9 공통으로 동작하도록 install /dev/stdin 등을 쓰지 않는다.
set -euo pipefail
[ "$(id -u)" = 0 ] || { echo "sudo로 실행하세요"; exit 1; }
BIN=${1:?사용: sudo ./install-hub.sh <moat-hub 바이너리> [init 옵션...]}
shift
HERE=$(cd "$(dirname "$0")" && pwd)
[ -x "$BIN" ] || { echo "실행 파일이 아닙니다: $BIN"; exit 1; }
"$BIN" --version >/dev/null || { echo "이 시스템에서 실행할 수 없는 바이너리입니다"; exit 1; }

echo "[1/5] 바이너리 설치: /usr/local/bin/moat-hub ($("$BIN" --version))"
install -m 0755 -o root -g root "$BIN" /usr/local/bin/moat-hub.new
mv -f /usr/local/bin/moat-hub.new /usr/local/bin/moat-hub

echo "[2/5] Agent 바이너리 (서버 추가용)"
DIST="$HERE/../dist"
if [ -f "$DIST/SHA256SUMS" ] && [ -f "$DIST/VERSION" ]; then
  (cd "$DIST" && sha256sum -c --quiet SHA256SUMS) || { echo "dist/ 체크섬 불일치"; exit 1; }
  install -d -m 0755 -o root -g root /usr/local/share/moat/agent
  for f in moat-agent-linux-amd64 moat-agent-linux-arm64 SHA256SUMS VERSION; do
    install -m 0644 -o root -g root "$DIST/$f" "/usr/local/share/moat/agent/$f"
  done
  echo "  $(cat "$DIST/VERSION") 설치 — 접속 중인 Agent는 자동으로 이 버전으로 업데이트됨"
else
  echo "  dist/ 없음 — 건너뜀 (make agent 후 다시 실행하면 설치됨)"
fi

echo "[3/5] 설정 파일"
install -d -m 0700 -o root -g root /etc/moat
if [ -f /etc/moat/hub.json ]; then
  echo "  기존 /etc/moat/hub.json 유지"
elif [ $# -gt 0 ]; then
  /usr/local/bin/moat-hub init --output /etc/moat/hub.json "$@"
else
  echo "  /etc/moat/hub.json이 없습니다. init 옵션을 함께 주세요 (moat-hub init --help)"; exit 1
fi

echo "[4/5] systemd 유닛"
/usr/local/bin/moat-hub install-service
systemctl daemon-reload

echo "[5/5] 서비스 시작"
systemctl enable moat-hub >/dev/null 2>&1
systemctl restart moat-hub
sleep 1
if systemctl is-active -q moat-hub; then
  echo "완료: moat-hub 실행 중"
  journalctl -u moat-hub -n 3 --no-pager -o cat
else
  echo "!! 시작 실패:"; journalctl -u moat-hub -n 20 --no-pager -o cat; exit 1
fi
