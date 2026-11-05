"""Independent production admission. Only this service may dispatch automated SSM."""

import base64
from datetime import datetime, timedelta, timezone
import hashlib
import hmac
import json
import re
from urllib.parse import quote, urlencode
import uuid

import release

REPO = "kacperkg/vellum"
API = f"/repos/{REPO}"
WORKFLOW = ".github/workflows/release-production.yml"
ISSUER = "https://token.actions.githubusercontent.com"
ADMISSION = b"vellum-production-admission-v1\n"
READINESS = b"vellum-production-readiness-v1\n"
ATTEMPT_TTL = timedelta(minutes=35)
SHA = re.compile(release.SHA)
HEX = re.compile(release.HEX)
require = release.require
canonical = release.canonical


def digest(value):
    return hashlib.sha256(canonical(value)).hexdigest()


def stamp(now):
    return now.strftime("%Y-%m-%dT%H:%M:%SZ")


def timestamp(value):
    require(isinstance(value, str) and re.fullmatch(r"\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ", value),
            "invalid evidence timestamp")
    return datetime.fromisoformat(value[:-1] + "+00:00")


def uuid_string(value):
    return isinstance(value, str) and re.fullmatch(r"[a-f0-9-]{36}", value) and str(uuid.UUID(value)) == value


def b64url(value):
    require(re.fullmatch(r"[A-Za-z0-9_-]+", value), "invalid OIDC encoding")
    result = base64.urlsafe_b64decode(value + "=" * (-len(value) % 4))
    require(base64.urlsafe_b64encode(result).decode().rstrip("=") == value, "noncanonical OIDC encoding")
    return result


def oidc(token, jwks, audience, now):
    """Verify the GitHub JWT using only the fixed, HTTPS-retrieved key set."""
    require(isinstance(token, str) and 0 < len(token) <= 8192, "invalid OIDC token")
    header, claims, signature = token.split(".")
    metadata = json.loads(b64url(header))
    require(metadata["alg"] == "RS256" and isinstance(metadata["kid"], str), "unsupported OIDC signing algorithm")
    keys = [key for key in jwks["keys"] if key.get("kid") == metadata["kid"] and key.get("kty") == "RSA"]
    require(len(keys) == 1, "unknown OIDC signing key")
    key = keys[0]
    modulus = b64url(key["n"])
    exponent = int.from_bytes(b64url(key["e"]), "big")
    signature_bytes = b64url(signature)
    n = int.from_bytes(modulus, "big")
    s = int.from_bytes(signature_bytes, "big")
    require(256 <= len(modulus) <= 512 and len(signature_bytes) == len(modulus)
            and exponent == 65537 and 0 < s < n, "invalid OIDC RSA key or signature")
    encoded = pow(s, exponent, n).to_bytes(len(modulus), "big")
    digest_info = bytes.fromhex("3031300d060960864801650304020105000420")
    digest_info += hashlib.sha256(f"{header}.{claims}".encode("ascii")).digest()
    expected = b"\x00\x01" + b"\xff" * (len(modulus) - len(digest_info) - 3) + b"\x00" + digest_info
    require(hmac.compare_digest(encoded, expected), "invalid GitHub OIDC signature")
    value = json.loads(b64url(claims))
    epoch = int(now.timestamp())
    require(value["iss"] == ISSUER and value["aud"] == audience
            and all(type(value[name]) is int for name in ("iat", "nbf", "exp"))
            and epoch - 300 <= value["iat"] <= epoch + 15 and value["nbf"] <= epoch < value["exp"]
            and value["iat"] <= value["exp"] <= value["iat"] + 600
            and isinstance(value["jti"], str) and 0 < len(value["jti"]) <= 128,
            "stale or foreign GitHub OIDC token")
    return value


class PendingCommand(ValueError):
    """An accepted SSM invocation is not visible yet."""


class Authority:
    def __init__(self, config, github, ssm, state, recovery, sign, now=None):
        self.config = config
        self.github = github
        self.ssm = ssm
        self.state = state
        self.recovery = recovery
        self.sign = sign
        self.now = now or (lambda: datetime.now(timezone.utc))

    def approval(self, value):
        cfg = self.config
        require(value["repository"].lower() == REPO and value["repository_id"] == str(cfg["repository_id"])
                and value["ref"] == "refs/heads/main" and value["event_name"] == "workflow_dispatch"
                and value["environment"] == "production"
                and value["workflow_ref"].lower() == f"{REPO}/{WORKFLOW}@refs/heads/main"
                and isinstance(value["sha"], str) and SHA.fullmatch(value["sha"])
                and value["workflow_sha"] == value["sha"], "OIDC token is outside the production job")
        for key in ("run_id", "run_attempt", "check_run_id"):
            require(isinstance(value[key], str) and re.fullmatch(r"[1-9][0-9]{0,17}", value[key]),
                    "invalid GitHub job identity")
        return {key: int(value[key]) for key in ("run_id", "run_attempt", "check_run_id")} | {"sha": value["sha"]}

    def recheck(self, approval):
        sha, run_id, attempt, job_id = (approval[key] for key in ("sha", "run_id", "run_attempt", "check_run_id"))
        gh = self.github
        branch = gh(f"{API}/branches/main")
        require(branch["protected"] is True and branch["commit"]["sha"] == sha,
                "protected main advanced or its protection changed")
        run = gh(f"{API}/actions/runs/{run_id}")
        require(run["id"] == run_id and run["run_attempt"] == attempt
                and run["repository"]["id"] == self.config["repository_id"]
                and run["head_repository"]["id"] == self.config["repository_id"]
                and run["event"] == "workflow_dispatch" and run["head_branch"] == "main"
                and run["path"] in (WORKFLOW, WORKFLOW + "@main", WORKFLOW + "@refs/heads/main") and run["head_sha"] == sha
                and run["status"] == "in_progress" and run["conclusion"] is None,
                "production approval is cancelled, obsolete or unverifiable")
        jobs = gh(f"{API}/actions/runs/{run_id}/attempts/{attempt}/jobs?per_page=100")
        require(jobs["total_count"] == len(jobs["jobs"]) <= 100, "incomplete production attempt")
        matches = [job for job in jobs["jobs"] if job["id"] == job_id]
        require(len(matches) == 1, "production job is outside the approved attempt")
        job = matches[0]
        require(job["name"] == "promote" and job["run_id"] == run_id and job["head_sha"] == sha
                and job["status"] == "in_progress" and job["conclusion"] is None,
                "protected production job is unavailable")
        # A signed token can only be obtained after this exact, hash-pinned job
        # starts. Its server-side condition requires the true dispatch checkbox.
        workflow = gh(f"{API}/contents/{WORKFLOW}?ref={sha}")
        require(hashlib.sha256(base64.b64decode(workflow["content"])).hexdigest() == self.config["workflow_sha256"],
                "production checkbox workflow changed; re-review issuer policy")
        environment = gh(f"{API}/environments/production")
        policy = environment["deployment_branch_policy"]
        require(policy["custom_branch_policies"] is True and policy["protected_branches"] is False,
                "production environment must restrict dispatch to main")
        require(digest(environment["protection_rules"]) == self.config["environment_protection_sha256"],
                "production environment protection rules changed")
        if any(rule["type"] == "required_reviewers" for rule in environment["protection_rules"]):
            # The GitHub review-history endpoint is run-scoped. Do not carry an
            # old Environment approval across reruns.
            require(attempt == 1, "Environment review on a rerun needs a new dispatch")
            reviews = gh(f"{API}/actions/runs/{run_id}/approvals")
            decisions = [item for item in reviews if any(
                env["id"] == environment["id"] for env in item["environments"])]
            require(len(decisions) == 1 and decisions[0]["state"] == "approved",
                    "no unambiguous production Environment review")
        branches = gh(f"{API}/environments/production/deployment-branch-policies")
        require(branches["total_count"] == 1
                and [(item["name"], item["type"]) for item in branches["branch_policies"]] == [("main", "branch")],
                "unexpected production environment deployment refs")

    def dispatch(self, event, jwks):
        require(set(event) == {"token", "manifest"}, "unexpected production dispatch fields")
        now = self.now()
        approval = self.approval(oidc(event["token"], jwks, self.config["audience"], now))
        self.recheck(approval)
        manifest = release.validate_manifest(event["manifest"], REPO, verified=True)
        self.github.verify_release(approval["sha"], manifest)
        self.ssm.verify_staging(manifest)
        self.recovery.available(now)
        self.recheck(approval)
        lease = f"{approval['run_id']}:{approval['run_attempt']}"
        # Reserve before invoking the non-idempotent SendCommand API. An
        # ambiguous acceptance burns this attempt and cannot trigger a resend.
        until = now + ATTEMPT_TTL
        self.state.reserve(lease, until)
        command = self.ssm.send(manifest)
        require(uuid_string(command), "invalid accepted SSM command ID")
        self.state.record(command, {
            "approval": approval, "manifest_sha256": digest(manifest),
            "expires_at": stamp(until),
        }, until)
        return {"command_id": command}

    def finish(self, event, jwks):
        require(set(event) == {"token", "command_id", "manifest_sha256"}, "unexpected production outcome fields")
        command, checksum = event["command_id"], event["manifest_sha256"]
        require(uuid_string(command) and isinstance(checksum, str) and HEX.fullmatch(checksum),
                "invalid production outcome identity")
        approval = self.approval(oidc(event["token"], jwks, self.config["audience"], self.now()))
        record = self.state.lookup(command)
        require(record and record["approval"] == approval and record["manifest_sha256"] == checksum,
                "production outcome does not cover this workflow attempt")
        if not self.ssm.verify_receipt(command, checksum, allow_pending=True):
            return {"pending": True}
        self.state.confirm_baseline(checksum, command)
        return {"verified": True}

    def host(self, event):
        method = event.get("method")
        require(method in ("admit", "sign") and set(event) == {"method", "request"}, "unexpected host request")
        request = event["request"]
        fields = {"schema", "nonce", "command_id", "manifest"}
        require(set(request) == fields | ({"live"} if method == "sign" else set())
                and type(request["schema"]) is int and request["schema"] == 2
                and isinstance(request["nonce"], str) and HEX.fullmatch(request["nonce"])
                and uuid_string(request["command_id"]), "invalid invocation challenge")
        manifest = release.validate_manifest(request["manifest"], REPO, verified=True)
        command = request["command_id"]
        record = self.state.lookup(command)
        if record is None and method == "admit":
            raise PendingCommand("accepted invocation not visible")
        now = self.now()
        require(record and timestamp(record["expires_at"]) > now
                and record["manifest_sha256"] == digest(manifest), "missing or mismatched authorised invocation")
        self.recheck(record["approval"])
        self.ssm.verify_production(command, manifest)
        self.github.verify_latest_attempt(manifest)
        payload = {"schema": 2, "request_sha256": digest(request)}
        if method == "admit":
            payload |= {"expires_at": stamp(now + timedelta(minutes=4))}
            return {"admission": payload, "signature": self.sign(ADMISSION + canonical(payload))}
        live = request["live"]
        baseline = live["current_manifest_sha256"]
        require(isinstance(baseline, str) and HEX.fullmatch(baseline), "invalid production receipt hash")
        require(baseline != digest(manifest), "production already has this release")
        if baseline != self.config["initial_manifest_sha256"]:
            previous = self.state.baseline(baseline)
            require(previous is not None, "unknown current production receipt")
            if not previous["confirmed"]:
                self.ssm.verify_receipt(previous["command"], baseline)
                self.state.confirm_baseline(baseline, previous["command"])
        backup, drill = self.recovery.evidence(live, now)
        self.recheck(record["approval"])
        self.github.verify_latest_attempt(manifest)
        self.ssm.verify_production(command, manifest)
        now = self.now()
        require(timestamp(record["expires_at"]) > now, "invocation authorisation expired")
        payload |= {"issuer": "vellum-production-recovery-v1", "issued_at": stamp(now),
                    "expires_at": stamp(now + timedelta(minutes=2)), "backup": backup,
                    "drill": drill, "rehearsal": None}
        self.state.consume(command)
        self.state.remember_baseline(digest(manifest), command)
        return {"evidence": payload, "signature": self.sign(READINESS + canonical(payload))}
