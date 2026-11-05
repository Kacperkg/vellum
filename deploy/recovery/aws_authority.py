"""Lambda adapters for the independently authorised production deployment."""

import base64
from datetime import datetime, timedelta, timezone
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
from urllib.parse import urlencode
from urllib.request import urlopen

from authority import API, Authority, ISSUER, PendingCommand, require, stamp, timestamp
import production_release
import release

MAX_RECEIPT = 32768


class GitHub:
    def __call__(self, path, raw=False):
        return release.api(path, raw=raw)

    def verify_release(self, sha, manifest):
        staging = self(f"{API}/git/ref/heads/staging")["object"]["sha"]
        tree = self(f"{API}/git/commits/{sha}")["tree"]["sha"]
        expected_bundle = {}
        for path in release.BUNDLE:
            item = self(f"{API}/contents/deploy/{path}?ref={sha}")
            require(item["encoding"] == "base64" and item["type"] == "file", "invalid trusted bundle file")
            expected_bundle[path] = hashlib.sha256(base64.b64decode(item["content"])).hexdigest()
        query = urlencode({"branch": "staging", "event": "push",
                           "created": f">={stamp(datetime.now(timezone.utc) - timedelta(days=90))}"})
        runs = production_release.paged(f"{production_release.STAGING_WORKFLOW}/runs?{query}",
                                        "workflow_runs", limit=1000, count=True)
        matched = []
        seen = set()
        for run in runs:
            run_id, attempt, source = run["id"], run["run_attempt"], run["head_sha"]
            require(release.positive(run_id) and run_id not in seen, "ambiguous staging history")
            seen.add(run_id)
            if (run["status"], run["conclusion"], run["event"], run["head_branch"]) != (
                "completed", "success", "push", "staging",
            ):
                continue
            production_release.exact_run(run, run_id, attempt, source)
            require(re.fullmatch(release.SHA, source), "invalid staging SHA")
            if self(f"{API}/git/commits/{source}")["tree"]["sha"] != tree:
                continue
            comparison = self(f"{API}/compare/{source}...{staging}?per_page=1")
            if comparison["status"] not in ("ahead", "identical") or comparison["merge_base_commit"]["sha"] != source:
                continue
            base = f"{API}/actions/runs/{run_id}"
            production_release.exact_run(self(base), run_id, attempt, source)
            production_release.exact_run(self(f"{base}/attempts/{attempt}"), run_id, attempt, source)
            if not production_release.check_jobs(base, attempt, run_id, source):
                continue
            artifact = production_release.verified_artifact(base, run_id, attempt, source,
                                                             run["repository"]["id"], expected_bundle)
            if artifact is not None:
                matched.append(artifact[2])
            require(len(matched) <= 1, "multiple eligible ordinary staging releases")
        require(matched == [manifest], "manifest is not the unique eligible staging release")
        require(self(f"{API}/git/ref/heads/staging")["object"]["sha"] == staging, "staging advanced during selection")

    def verify_latest_attempt(self, manifest):
        run_id, attempt, source = (manifest[key] for key in ("run_id", "run_attempt", "source_sha"))
        production_release.exact_run(self(f"{API}/actions/runs/{run_id}"), run_id, attempt, source)


class SSM:
    def __init__(self, client, config):
        self.client = client
        self.config = config

    def pins(self, environment):
        cfg = self.config
        pin = cfg[environment]
        expected = f"arn:aws:ssm:eu-west-2:{cfg['account']}:document/VellumDeploy-{environment}"
        require(pin["document"] == expected and re.fullmatch(r"[1-9][0-9]{0,8}", pin["version"])
                and re.fullmatch(release.HEX, pin["sha256"])
                and re.fullmatch(r"i-[0-9a-f]{8,17}", pin["instance"]), "invalid installed SSM pins")
        require(environment != "production" or int(pin["version"]) > 2, "automated document is not pinned")
        return pin

    def document(self, pin):
        description = self.client.describe_document(Name=pin["document"], DocumentVersion=pin["version"])["Document"]
        require(description["Owner"] == self.config["account"]
                and description["Name"] == pin["document"].rsplit("/", 1)[-1]
                and description["DocumentVersion"] == pin["version"]
                and description["HashType"] == "Sha256" and description["Hash"] == pin["sha256"]
                and description["Status"] == "Active", "AWS SSM document differs from its approved pins")

    def send(self, manifest):
        pin = self.pins("production")
        self.document(pin)
        result = self.client.send_command(InstanceIds=[pin["instance"]], DocumentName=pin["document"],
                                          DocumentVersion=pin["version"], DocumentHash=pin["sha256"],
                                          DocumentHashType="Sha256", Parameters=self.parameters(manifest, True),
                                          TimeoutSeconds=900)
        return result["Command"]["CommandId"]

    @staticmethod
    def parameters(manifest, production):
        result = {"Manifest": [base64.b64encode(release.canonical(manifest)).decode("ascii")]}
        if production:
            result["ApprovalMode"] = ["automated"]
        return result

    def verify_command(self, command, manifest, environment, status):
        pin = self.pins(environment)
        self.document(pin)
        listed = self.client.list_commands(CommandId=command)["Commands"]
        if not listed and environment == "production":
            raise PendingCommand("accepted SSM command not visible")
        require(len(listed) == 1, "SSM command unavailable")
        record = listed[0]
        name = pin["document"].rsplit("/", 1)[-1]
        require(record["CommandId"] == command and record["DocumentName"] in (name, pin["document"])
                and record["DocumentVersion"] == pin["version"] and record["InstanceIds"] == [pin["instance"]]
                and not record.get("Targets") and record["Parameters"] == self.parameters(manifest, environment == "production"),
                "SSM command target, parameters or pin mismatch")
        self.require_status(record["Status"], environment, status)
        plugin = "deployProduction" if environment == "production" else "deployStaging"
        try:
            item = self.client.get_command_invocation(CommandId=command, InstanceId=pin["instance"], PluginName=plugin)
        except self.client.exceptions.InvocationDoesNotExist:
            if environment == "production":
                raise PendingCommand("accepted SSM invocation not visible") from None
            raise
        require(item["CommandId"] == command and item["InstanceId"] == pin["instance"]
                and item["DocumentName"] in (name, pin["document"])
                and item["DocumentVersion"] == pin["version"]
                and item["PluginName"] == plugin, "SSM invocation differs from approved command")
        self.require_status(item["Status"], environment, status)

    @staticmethod
    def require_status(actual, environment, expected):
        if environment == "production" and actual in ("Pending", "Delayed"):
            raise PendingCommand("accepted SSM invocation has not started")
        require(actual == expected, "SSM invocation status differs")

    def verify_staging(self, manifest):
        candidate = manifest | {"staging": {"verified": False, "command_id": None}}
        self.verify_command(manifest["staging"]["command_id"], candidate, "staging", "Success")

    def verify_production(self, command, manifest):
        self.verify_command(command, manifest, "production", "InProgress")

    def verify_receipt(self, command, checksum):
        pin = self.pins("production")
        item = self.client.get_command_invocation(CommandId=command, InstanceId=pin["instance"],
                                                  PluginName="deployProduction")
        require(item["Status"] == "Success" and item["CommandId"] == command
                and item["InstanceId"] == pin["instance"] and item["PluginName"] == "deployProduction"
                and f"Verified production receipt {command} {checksum}" in item["StandardOutputContent"].splitlines(),
                "current production receipt has no successful independent SSM outcome")


class Ledger:
    def __init__(self, client, table):
        self.client = client
        self.table = table

    def insert(self, key, value, until):
        self.client.put_item(TableName=self.table, Item={
            "key": {"S": key}, "value": {"S": json.dumps(value)}, "expiry": {"N": str(int(until.timestamp()))},
        }, ConditionExpression="attribute_not_exists(#key)", ExpressionAttributeNames={"#key": "key"})

    def reserve(self, attempt, until):
        self.insert(f"attempt:{attempt}", {}, until)

    def record(self, command, value, until):
        self.insert(f"command:{command}", value, until)

    def lookup(self, command):
        item = self.client.get_item(TableName=self.table, Key={"key": {"S": f"command:{command}"}},
                                    ConsistentRead=True).get("Item")
        return json.loads(item["value"]["S"]) if item else None

    def baseline(self, checksum):
        item = self.client.get_item(TableName=self.table, Key={"key": {"S": f"baseline:{checksum}"}},
                                    ConsistentRead=True).get("Item")
        if item is None:
            return None
        return {"command": item["command"]["S"], "confirmed": item.get("confirmed", {}).get("BOOL", False)}

    def confirm_baseline(self, checksum, command):
        self.client.update_item(TableName=self.table, Key={"key": {"S": f"baseline:{checksum}"}},
                                UpdateExpression="SET confirmed = :yes",
                                ConditionExpression="#command = :command",
                                ExpressionAttributeNames={"#command": "command"},
                                ExpressionAttributeValues={":command": {"S": command}, ":yes": {"BOOL": True}})

    def remember_baseline(self, checksum, command):
        self.client.put_item(TableName=self.table, Item={
            "key": {"S": f"baseline:{checksum}"}, "command": {"S": command},
        })

    def consume(self, command):
        self.client.update_item(TableName=self.table, Key={"key": {"S": f"command:{command}"}},
                                UpdateExpression="SET consumed = :yes",
                                ConditionExpression="attribute_exists(#key) AND attribute_not_exists(consumed)",
                                ExpressionAttributeNames={"#key": "key"}, ExpressionAttributeValues={":yes": {"BOOL": True}})


class Recovery:
    def __init__(self, store, config):
        self.store = store
        self.config = config

    def receipt(self, kind):
        cfg = self.config
        name = {"backup": "verified-backup", "drill": "restore-drill"}[kind]
        prefix = f"production/recovery/{name}/"
        bucket = {"Bucket": cfg["receipt_bucket"], "ExpectedBucketOwner": cfg["receipt_account"]}
        today = datetime.now(timezone.utc)
        # Bound listings by UTC day. Five-minute verification receipts can exceed
        # one listing across the retention period.
        keys = []
        for day in range(1 if kind == "backup" and today.hour else 8):
            date = (today - timedelta(days=day)).strftime("%Y%m%d")
            page = self.store.list_objects_v2(**bucket, Prefix=prefix + date, MaxKeys=1000)
            require(not page["IsTruncated"], "ambiguous worker receipt listing")
            keys = [item["Key"] for item in page.get("Contents", [])]
            if keys:
                break
        require(keys and all(re.fullmatch(re.escape(prefix) + r"[0-9]{8}T[0-9]{6}Z-[a-f0-9]{32}\.json", key)
                             for key in keys), "missing or invalid worker receipt key")
        key = max(keys)
        response = self.store.get_object(**bucket, Key=key)
        require(response.get("VersionId") not in (None, "", "null"), "worker receipt bucket is not versioned")
        with response["Body"] as source:
            data = source.read(MAX_RECEIPT + 1)
        require(len(data) <= MAX_RECEIPT, "oversized recovery receipt")
        receipt = json.loads(data)
        require(set(receipt) == {"evidence", "signature"} and re.fullmatch(r"[a-f0-9]{128}", receipt["signature"]),
                "invalid signed worker receipt")
        evidence = receipt["evidence"]
        domain = {"drill": "vellum-production-restore-drill-v1",
                  "backup": "vellum-production-verified-backup-v1"}[kind]
        require(evidence["schema"] == 1 and evidence["issuer"] == domain
                and evidence["profile_sha256"] == cfg["worker_profile_sha256"], "worker receipt profile differs")
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            (root / "message").write_bytes(domain.encode("ascii") + b"\n" + release.canonical(evidence))
            (root / "signature").write_bytes(bytes.fromhex(receipt["signature"]))
            (root / "public.pem").write_text(cfg["worker_public_keys"][kind])
            subprocess.run(["/usr/bin/openssl", "pkeyutl", "-verify", "-rawin", "-pubin",
                            "-inkey", str(root / "public.pem"), "-in", str(root / "message"),
                            "-sigfile", str(root / "signature")], stdout=subprocess.DEVNULL,
                           stderr=subprocess.DEVNULL, timeout=5, check=True)
        return evidence

    def available(self, now):
        backup = self.receipt("backup")["backup"]
        drill_evidence = self.receipt("drill")
        drill = drill_evidence["drill"]
        require("baseline-backend-read-only-smoke" in drill_evidence["checks"],
                "restore did not exercise the approved backend")
        require(now - timedelta(hours=24) < timestamp(backup["created_at"]) <= now
                and now - timedelta(minutes=10) < timestamp(backup["verified_at"]) <= now
                and now - timedelta(days=7) < timestamp(drill["finished_at"]) <= now,
                "fresh backup verification or restore drill is unavailable")
        return backup, drill

    def evidence(self, live, now):
        cfg = self.config
        backup, drill = self.available(now)
        require(live["risk"] == "low", "changed backend or deployment bundle requires reviewed maintenance")
        for key, value in cfg["live_policy"].items():
            require(live[key] == value, "production baseline differs from approved recovery policy")
        require(set(cfg["live_policy"]) == {"postgres", "schema_sha256", "migration_contract_sha256",
                                          "database_source_sha256", "proxy_sha256", "backup_contract",
                                          "execution_sha256", "runtime_sha256"}, "incomplete independent baseline policy")
        require(backup["contract"] == drill["contract"] == live["backup_contract"]
                and backup["source_sha256"] == drill["source_sha256"] == live["database_source_sha256"]
                and drill["postgres"] == live["postgres"]
                and all(drill[key] == live[key] for key in ("schema_sha256", "execution_sha256", "runtime_sha256")),
                "recovery evidence does not cover the live baseline")
        return backup, drill


def handler(event, _context):
    """Deploy separate immutable Lambda aliases with distinct IAM invoke rights."""
    try:
        import boto3
        from botocore.config import Config

        # Keep the GitHub token out of the Lambda event and immutable image.
        cfg = json.loads(os.environ["VELLUM_AUTHORITY_CONFIG"])
        session = boto3.session.Session(region_name="eu-west-2")
        service = Config(connect_timeout=3, read_timeout=10, retries={"total_max_attempts": 1})
        client = lambda name: session.client(name, config=service)
        secret = client("secretsmanager").get_secret_value(SecretId=cfg["github_secret"])["SecretString"]
        os.environ["GH_TOKEN"] = secret

        def sign(message):
            require(0 < len(message) <= 4096, "signed message exceeds the issuer budget")
            return client("kms").sign(KeyId=cfg["issuer_key_arn"], Message=message,
                                      MessageType="RAW", SigningAlgorithm="ED25519_SHA_512")["Signature"].hex()

        authority = Authority(cfg, GitHub(), SSM(client("ssm"), cfg), Ledger(client("dynamodb"), cfg["table"]),
                              Recovery(client("s3"), cfg), sign)
        role = os.environ["VELLUM_AUTHORITY_ENDPOINT"]
        if role == "dispatch":
            with urlopen(ISSUER + "/.well-known/jwks", timeout=5) as response:
                data = response.read(65537)
            require(len(data) <= 65536 and response.geturl() == ISSUER + "/.well-known/jwks",
                    "invalid GitHub identity key response")
            jwks = json.loads(data)
            if "command_id" in event:
                return authority.finish(event, jwks)
            return authority.dispatch(event, jwks)
        require(role == "host", "invalid issuer endpoint")
        return authority.host(event)
    except PendingCommand:
        return {"retry": True}
    except Exception:
        # API errors and events can carry credentials or SSM parameters.
        return {"error": "independent recovery admission or evidence unavailable"}
