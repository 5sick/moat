# 아키텍처

```
                    인터넷
                      │ 443
              ┌───────▼────────┐  입구(edge) 역할 Agent — 공인 IP가 있는 노드
              │ TLS·ACME 인증서 │
              │ 리버스 프록시    │──► 로그인 검증: Hub /auth/verify
              └───────┬────────┘
                      │ WireGuard 메시 (10.200.0.0/24)
     ┌────────────────┼──────────────────┐
     ▼                ▼                  ▼
  Hub (C++)        Agent (Go)         Agent (Go)
  로그인·대시보드   메트릭·systemd     서비스 탐지
  노드·서비스 목록  Docker·WG 상태     웹 터미널
  알림·감사 로그     ▲
  SQLite 파일 하나   └── WebSocket (Agent → Hub, outbound, 서명 인증)
```

## 구성 요소

### Hub (C++20, Drogon)
- **로그인 관문**: Google OIDC로 허용된 이메일만 통과 → 패스키 등록 후 패스키만으로 로그인.
  입구 프록시가 Hub의 `/auth/verify`를 호출해 자체 인증이 없는 서비스도 보호한다.
- **에이전트 게이트웨이**: Agent가 WebSocket으로 접속. 등록(join) 시 Agent가 만든 Ed25519 공개키를 저장하고,
  이후 접속은 서명 챌린지로 인증한다.
- **클러스터 관리**: 노드 목록, 메시 IP 할당, WireGuard 피어 구성 배포, 서비스(도메인→노드:포트) 목록.
- **저장소**: SQLite 단일 파일. 메트릭은 다운샘플링해 보관 기간을 제한한다.
- **외부 HTTPS**(Google, 텔레그램)는 libcurl. (Ubuntu의 Drogon HttpClient는 TLS가 없다.)
- **웹 UI**: 바이너리에 내장해 단일 파일로 배포.

### Agent (Go)
- 단일 정적 바이너리 (linux amd64/arm64, 메모리 10~20MB 목표). root로 실행.
- 수집: CPU·메모리·디스크·네트워크, systemd 유닛 상태, Docker 컨테이너, WireGuard 피어, 열린 포트.
- WireGuard: 기존 인터페이스는 채택(읽기만), 새 노드는 Hub가 준 설정으로 구성.
- 입구 역할(3단계): HTTPS 리버스 프록시 + ACME. 라우팅 표는 Hub에서 받아 로컬에 저장(Hub 장애에도 유지).
- Hub가 내려가 있어도 계속 동작하고 재접속한다.

## 서버 추가 흐름 (join)
1. 웹에서 "서버 추가" → Hub가 일회용 토큰(15분) 발급, 설치 명령 표시
2. 새 서버: `curl -fsSL https://<hub>/join.sh | sudo sh -s <토큰>` → 아키텍처에 맞는 Agent 다운로드·설치
3. Agent가 Ed25519 키·WireGuard 키 생성 → `POST /api/agent/enroll` (토큰 소모)
4. Hub가 노드 등록, 메시 IP 할당, 피어 목록 응답 → Agent가 wg 구성 → 이후 WebSocket 상시 접속

## 장애 시나리오
| 상황 | 결과 | 대응 |
|---|---|---|
| Hub 다운 | 로그인 필요한 서비스 접근 불가(fail-closed), 서버·터널·공개 서비스는 정상 | systemd 자동 재시작, 클라우드 시리얼 콘솔 |
| Agent 다운 | 해당 노드 상태 미표시, 서비스는 정상 | systemd 자동 재시작, Hub가 "응답 없음" 알림 |
| WireGuard 다운 | Hub↔Agent 단절 | Agent는 공개 Hub URL로도 접속 가능, 클라우드 시리얼 콘솔 |
