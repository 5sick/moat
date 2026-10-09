#!/usr/bin/env python3
"""E2E 테스트용 가짜 Google OIDC 서버.

/auth  : 인가 요청을 받아 즉시 code와 state를 붙여 redirect_uri로 돌려보낸다 (사용자 동의 생략)
/token : code → RS256 서명된 id_token 발급 (인가 때 받은 nonce, PKCE 검증 포함)
/certs : JWKS

환경변수 MOCK_EMAIL 로 로그인할 이메일, MOCK_TAMPER=1 이면 서명을 망가뜨린다.
"""
import base64, hashlib, json, os, secrets, sys, time, urllib.parse
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import jwt
from cryptography.hazmat.primitives.asymmetric import rsa

KEY = rsa.generate_private_key(public_exponent=65537, key_size=2048)
KID = "mock-" + secrets.token_hex(4)  # 실제 Google처럼 키가 바뀌면 kid도 바뀐다
CLIENT_ID = os.environ.get("MOCK_CLIENT_ID", "mock-client")
CODES = {}  # code -> (nonce, challenge, redirect_uri)


def b64(n: int) -> str:
    raw = n.to_bytes((n.bit_length() + 7) // 8, "big")
    return base64.urlsafe_b64encode(raw).rstrip(b"=").decode()


class H(BaseHTTPRequestHandler):
    def log_message(self, *a):
        pass

    def send(self, code, body=b"", ctype="application/json", headers=None):
        self.send_response(code)
        self.send_header("Content-Type", ctype)
        for k, v in (headers or {}).items():
            self.send_header(k, v)
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        u = urllib.parse.urlparse(self.path)
        q = dict(urllib.parse.parse_qsl(u.query))
        if u.path == "/auth":
            assert q["client_id"] == CLIENT_ID and q["code_challenge_method"] == "S256"
            code = secrets.token_urlsafe(16)
            CODES[code] = (q["nonce"], q["code_challenge"], q["redirect_uri"])
            loc = q["redirect_uri"] + "?" + urllib.parse.urlencode({"code": code, "state": q["state"]})
            return self.send(302, headers={"Location": loc})
        if u.path == "/certs":
            n = KEY.public_key().public_numbers()
            body = {"keys": [{"kty": "RSA", "alg": "RS256", "use": "sig", "kid": KID, "n": b64(n.n), "e": b64(n.e)}]}
            return self.send(200, json.dumps(body).encode(), headers={"Cache-Control": "public, max-age=600"})
        self.send(404)

    def do_POST(self):
        if self.path != "/token":
            return self.send(404)
        form = dict(urllib.parse.parse_qsl(self.rfile.read(int(self.headers["Content-Length"])).decode()))
        entry = CODES.pop(form.get("code"), None)
        if not entry:
            return self.send(400, b'{"error":"invalid_grant"}')
        nonce, challenge, redirect_uri = entry
        pkce_ok = base64.urlsafe_b64encode(hashlib.sha256(form["code_verifier"].encode()).digest()).rstrip(b"=").decode() == challenge
        if not pkce_ok or form["redirect_uri"] != redirect_uri or form["client_id"] != CLIENT_ID:
            return self.send(400, b'{"error":"invalid_request"}')
        now = int(time.time())
        claims = {"iss": "https://accounts.google.com", "aud": CLIENT_ID, "sub": "42",
                  "email": os.environ.get("MOCK_EMAIL", "test@example.com"), "email_verified": True,
                  "nonce": nonce, "iat": now, "exp": now + 3600}
        tok = jwt.encode(claims, KEY, algorithm="RS256", headers={"kid": KID})
        if os.environ.get("MOCK_TAMPER") == "1":
            tok = tok[:-4] + ("AAAA" if not tok.endswith("AAAA") else "BBBB")
        self.send(200, json.dumps({"id_token": tok, "access_token": "x", "token_type": "Bearer"}).encode())


if __name__ == "__main__":
    port = int(sys.argv[1]) if len(sys.argv) > 1 else 18801
    ThreadingHTTPServer(("127.0.0.1", port), H).serve_forever()
