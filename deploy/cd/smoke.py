#!/usr/bin/env python3
"""Small HTTPS checks through the existing Caddy; never create test accounts."""

import json
import urllib.error
import urllib.request
import uuid


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *args, **kwargs):
        return None


def request(origin, path, body=None, method=None, headers=None):
    data = None if body is None else json.dumps(body).encode()
    request_headers = {"Content-Type": "application/json", "Origin": origin}
    request_headers.update(headers or {})
    req = urllib.request.Request(
        origin + path, data=data, method=method, headers=request_headers
    )
    opener = urllib.request.build_opener(NoRedirect())
    try:
        response = opener.open(req, timeout=10)
    except urllib.error.HTTPError as error:
        response = error
    with response:
        payload = response.read((2 << 20) + 1)
        return response.status, response.headers, payload


def smoke(origin, staging=False, login_requests=10):
    status, headers, body = request(origin, "/ping")
    if status != 200 or json.loads(body) != {"message": "pong"}:
        raise ValueError("API health smoke failed")
    for path in ("/", "/login"):
        status, headers, body = request(origin, path)
        if status != 200 or "text/html" not in headers.get("Content-Type", ""):
            raise ValueError("frontend smoke failed")
    for path in ("/login", "/register", "/refresh", "/logout"):
        status, headers, body = request(origin, path, {})
        if status not in (400, 429) or "error" not in json.loads(body):
            raise ValueError("POST auth routing smoke failed")
    status, headers, body = request(
        origin,
        "/login",
        method="OPTIONS",
        headers={
            "Access-Control-Request-Method": "POST",
            "Access-Control-Request-Headers": "content-type",
        },
    )
    # Caddy preserves Host. Gin deliberately skips CORS for same-origin
    # requests, so this artificial OPTIONS normally reaches its 404 handler.
    # Browsers do not preflight same-origin auth. Cross-origin CORS is covered
    # by router tests and requires a separately allowed origin to test live.
    if status not in (204, 404):
        raise ValueError("unexpected same-origin OPTIONS response")
    if staging:
        if not 1 <= login_requests <= 100:
            raise ValueError(
                "automated staging smoke supports login limits 1..100; review larger limits manually"
            )
        email = "deployment-smoke-" + uuid.uuid4().hex + "@example.invalid"
        for attempt in range(login_requests + 1):
            # Caddy must replace XFF and Gin must ignore client-supplied XReal.
            status, headers, body = request(
                origin,
                "/login",
                {"email": email, "password": "invalid-smoke-password"},
                headers={
                    "X-Forwarded-For": f"198.51.100.{attempt % 250 + 1}",
                    "X-Real-IP": f"203.0.113.{attempt % 250 + 1}",
                },
            )
            expected = 401 if attempt < login_requests else 429
            if status != expected:
                raise ValueError("staging rate-limit/proxy-header smoke failed")
            if status == 429 and (
                json.loads(body) != {"error": "rate limit exceeded"}
                or not headers.get("Retry-After", "").isdigit()
                or int(headers["Retry-After"]) < 1
            ):
                raise ValueError("staging 429 contract failed")
