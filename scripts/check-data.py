#!/usr/bin/env python3
"""Check keel development data services without printing credentials or responses.

Default project: /opt/keel. Requires Python stdlib and the Docker Compose CLI.
MinIO smoke data is unique and deleted; the keel-dev-smoke bucket is retained.
This script intentionally prints only fixed check names and sanitized statuses.
"""

import argparse
import datetime
import hashlib
import hmac
import pathlib
import re
import shlex
import subprocess
import sys
import urllib.error
import urllib.parse
import urllib.request
import uuid

HTTP_TIMEOUT_SECONDS = 5
COMMAND_TIMEOUT_SECONDS = 12
MINIO_ENDPOINT = "http://127.0.0.1:9000"
MINIO_BUCKET = "keel-dev-smoke"


class CheckFailure(Exception):
    def __init__(self, step, status):
        self.step = step
        self.status = status
        super().__init__("data dependency check failed")


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def emit(ok, service, step, status):
    # All callers supply fixed labels, integers or controlled status constants.
    print(f"{'PASS' if ok else 'FAIL'} {service}:{step} status={status}", flush=True)


def read_minio_config(env_path):
    wanted = {
        "MINIO_ROOT_USER", "MINIO_ROOT_PASSWORD",
        "MINIO_REGION_NAME", "MINIO_REGION",
    }
    result = {}
    try:
        for line in env_path.read_text(encoding="utf-8").splitlines():
            line = line.strip()
            if not line or line.startswith("#"):
                continue
            if line.startswith("export "):
                line = line[7:].lstrip()
            key, separator, value = line.partition("=")
            key = key.strip()
            if not separator or key not in wanted:
                continue
            value = value.strip()
            if value.startswith(("'", '"')):
                parsed = shlex.split(value, comments=True, posix=True)
                if len(parsed) != 1:
                    raise CheckFailure("config", "INVALID_VALUE")
                # Compose expands dollars in double-quoted values. Refuse to
                # guess if the check would sign with a different credential.
                if value.startswith('"') and "$" in parsed[0]:
                    raise CheckFailure("config", "INTERPOLATION_UNSUPPORTED")
                result[key] = parsed[0]
            else:
                value = re.split(r"\s+#", value, maxsplit=1)[0].rstrip()
                if "$" in value:
                    raise CheckFailure("config", "INTERPOLATION_UNSUPPORTED")
                result[key] = value
    except CheckFailure:
        raise
    except (OSError, UnicodeError, ValueError):
        raise CheckFailure("config", "READ_OR_PARSE_ERROR") from None
    if not result.get("MINIO_ROOT_USER") or not result.get("MINIO_ROOT_PASSWORD"):
        raise CheckFailure("config", "MISSING_CREDENTIALS")
    return result


def sign(key, value):
    return hmac.new(key, value.encode("utf-8"), hashlib.sha256).digest()


def check_minio(env_path):
    config = read_minio_config(env_path)
    access_key = config["MINIO_ROOT_USER"]
    secret_key = config["MINIO_ROOT_PASSWORD"]
    region = config.get("MINIO_REGION_NAME") or config.get("MINIO_REGION") or "us-east-1"
    endpoint = urllib.parse.urlsplit(MINIO_ENDPOINT)
    # Ignore ambient HTTP proxies and forbid forwarding signed credentials on
    # redirects, including unexpected redirects from the loopback endpoint.
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())

    def request(method, path, payload=b"", expected_length=0):
        now = datetime.datetime.now(datetime.timezone.utc)
        amz_date = now.strftime("%Y%m%dT%H%M%SZ")
        date = now.strftime("%Y%m%d")
        body_hash = hashlib.sha256(payload).hexdigest()
        canonical_uri = urllib.parse.quote(path, safe="/-_.~")
        canonical_headers = (
            f"host:{endpoint.netloc}\n"
            f"x-amz-content-sha256:{body_hash}\n"
            f"x-amz-date:{amz_date}\n"
        )
        signed_headers = "host;x-amz-content-sha256;x-amz-date"
        canonical = "\n".join([
            method, canonical_uri, "", canonical_headers, signed_headers, body_hash,
        ])
        scope = f"{date}/{region}/s3/aws4_request"
        string_to_sign = "\n".join([
            "AWS4-HMAC-SHA256", amz_date, scope,
            hashlib.sha256(canonical.encode("utf-8")).hexdigest(),
        ])
        key = sign(("AWS4" + secret_key).encode("utf-8"), date)
        key = sign(sign(sign(key, region), "s3"), "aws4_request")
        signature = hmac.new(key, string_to_sign.encode("utf-8"), hashlib.sha256).hexdigest()
        authorization = (
            f"AWS4-HMAC-SHA256 Credential={access_key}/{scope}, "
            f"SignedHeaders={signed_headers}, Signature={signature}"
        )
        req = urllib.request.Request(
            MINIO_ENDPOINT + canonical_uri,
            data=payload if method == "PUT" else None,
            method=method,
            headers={
                "Host": endpoint.netloc,
                "x-amz-content-sha256": body_hash,
                "x-amz-date": amz_date,
                "Authorization": authorization,
            },
        )
        try:
            with opener.open(req, timeout=HTTP_TIMEOUT_SECONDS) as response:
                # Read only a bounded object response; never print HTTP bodies.
                body = response.read(expected_length + 1) if method == "GET" else b""
                return response.status, body
        except urllib.error.HTTPError as error:
            status = error.code
            error.close()
            return status, b""
        except (urllib.error.URLError, OSError, TimeoutError, ValueError):
            raise CheckFailure(method, "TRANSPORT_ERROR") from None

    status, _ = request("PUT", f"/{MINIO_BUCKET}")
    if status not in (200, 409):
        raise CheckFailure("bucket", f"HTTP_{status}")
    emit(True, "minio", "bucket", f"HTTP_{status}")
    object_path = f"/{MINIO_BUCKET}/probe-{uuid.uuid4().hex}"
    payload = ("keel-dev-smoke:" + uuid.uuid4().hex).encode("ascii")
    errors = []
    try:
        status, _ = request("PUT", object_path, payload)
        if status != 200:
            raise CheckFailure("PUT", f"HTTP_{status}")
        emit(True, "minio", "PUT", "HTTP_200")
        status, actual = request("GET", object_path, expected_length=len(payload))
        if status != 200:
            raise CheckFailure("GET", f"HTTP_{status}")
        if actual != payload:
            raise CheckFailure("GET", "CONTENT_MISMATCH")
        emit(True, "minio", "GET", "CONTENT_MATCH")
    except CheckFailure as error:
        errors.append(error)
    finally:
        # Even a timed-out PUT may have succeeded. Deleting this unique key is
        # safe and prevents probes from accumulating after partial failures.
        try:
            status, _ = request("DELETE", object_path)
            if status not in (200, 204):
                raise CheckFailure("DELETE", f"HTTP_{status}")
            emit(True, "minio", "DELETE", f"HTTP_{status}")
        except CheckFailure as error:
            errors.append(error)
    if errors:
        for error in errors:
            emit(False, "minio", error.step, error.status)
        return False
    return True


def check_command(project_dir, service, command, expected):
    args = [
        "docker", "compose", "--project-directory", str(project_dir),
        "--env-file", str(project_dir / ".env"), "exec", "-T", service,
    ] + command
    try:
        completed = subprocess.run(
            args, cwd=str(project_dir), stdin=subprocess.DEVNULL,
            stdout=subprocess.PIPE, stderr=subprocess.PIPE,
            text=True, timeout=COMMAND_TIMEOUT_SECONDS, check=False,
        )
    except subprocess.TimeoutExpired:
        raise CheckFailure("query", "TIMEOUT") from None
    except (OSError, UnicodeError):
        raise CheckFailure("query", "COMMAND_UNAVAILABLE") from None
    if completed.returncode != 0:
        raise CheckFailure("query", f"EXIT_{completed.returncode}")
    if completed.stdout.strip() != expected:
        raise CheckFailure("query", "UNEXPECTED_RESPONSE")
    emit(True, service, "query", "EXPECTED_RESPONSE")
    return True


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--project-dir", type=pathlib.Path, default=pathlib.Path("/opt/keel"))
    args = parser.parse_args()
    project_dir = args.project_dir.resolve()
    checks = [
        ("minio", lambda: check_minio(project_dir / ".env")),
        ("postgres", lambda: check_command(project_dir, "postgres", [
            "sh", "-c",
            'PGPASSWORD="$POSTGRES_PASSWORD" PGCONNECT_TIMEOUT=4 '
            'PGOPTIONS="-c statement_timeout=4000" '
            'exec psql -X --host=127.0.0.1 --username="$POSTGRES_USER" '
            '--dbname="$POSTGRES_DB" --no-password --tuples-only --no-align '
            '--command="SELECT 1"',
        ], "1")),
        ("redis", lambda: check_command(project_dir, "redis", [
            "sh", "-c",
            'if [ -n "${REDIS_PASSWORD:-}" ]; then '
            'REDISCLI_AUTH="$REDIS_PASSWORD" exec redis-cli -h 127.0.0.1 -p 6379 --raw PING; '
            'else exec redis-cli -h 127.0.0.1 -p 6379 --raw PING; fi',
        ], "PONG")),
    ]
    ok = True
    for service, check in checks:
        try:
            ok = check() and ok
        except CheckFailure as error:
            emit(False, service, error.step, error.status)
            ok = False
        except Exception:
            # No traceback or exception text: upstream libraries may embed URLs,
            # headers, subprocess arguments or response bodies in their errors.
            emit(False, service, "check", "INTERNAL_ERROR")
            ok = False
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
