#!/bin/sh
set -eu
command -v python3 >/dev/null 2>&1 || { echo 'Error: Python 3 is required for verified, recoverable installation.' >&2; exit 1; }
# Kept self-contained so the documented curl | sh installation still works.
exec python3 - "$@" <<'PY'
import contextlib
import datetime
import fcntl
import gzip
import hashlib
import io
import json
import os
from pathlib import Path, PurePosixPath
import platform
import re
import stat
import subprocess
import sys
import tarfile
import tempfile
import urllib.error
import urllib.parse
import urllib.request

if sys.version_info < (3, 8):
    sys.exit("Error: Python 3.8 or newer is required for verified installation.")

REPO = "Git-on-my-level/agent-nexus"
RELEASES = "https://github.com/" + REPO + "/releases"
API = "https://api.github.com/repos/" + REPO + "/releases/latest"
ASSET_HOSTS = {"release-assets.githubusercontent.com", "objects.githubusercontent.com", "github-releases.githubusercontent.com"}
LIMIT = 128 * 1024 * 1024
ENTRY_LIMIT = 1024
TAG = re.compile(r"v[0-9]+\.[0-9]+\.[0-9]+(?:[-+][A-Za-z0-9.-]+)?\Z")


def validate_redirect(entry, target, kind):
    origin, dest = urllib.parse.urlsplit(entry), urllib.parse.urlsplit(target)
    if dest.scheme != "https" or dest.username or dest.password or dest.port not in (None, 443):
        raise RuntimeError("Unsafe release redirect")
    if kind == "api":
        raise RuntimeError("Release API redirects are not accepted")
    if kind == "latest":
        prefix = "/" + REPO + "/releases/tag/"
        if dest.netloc != origin.netloc or not dest.path.startswith(prefix) or not TAG.fullmatch(dest.path[len(prefix):]):
            raise RuntimeError("Release discovery escaped the repository")
    elif kind == "asset":
        if dest.netloc == origin.netloc:
            if dest.path != origin.path:
                raise RuntimeError("Release asset escaped the repository or filename")
        elif dest.hostname not in ASSET_HOSTS:
            raise RuntimeError("Unapproved release asset host")


class ReleaseRedirect(urllib.request.HTTPRedirectHandler):
    def __init__(self, entry, kind):
        self.entry, self.kind, self.count = entry, kind, 0

    def redirect_request(self, req, fp, code, msg, headers, newurl):
        self.count += 1
        if self.count > 5:
            raise RuntimeError("Too many release redirects")
        validate_redirect(self.entry, newurl, self.kind)
        return super().redirect_request(req, fp, code, msg, headers, newurl)


def download(url, kind, limit=LIMIT):
    parsed = urllib.parse.urlsplit(url)
    if parsed.scheme != "https" or parsed.username or parsed.password:
        raise RuntimeError("Release endpoints must use HTTPS")
    opener = urllib.request.build_opener(ReleaseRedirect(url, kind))
    with opener.open(url, timeout=30) as response:
        body = response.read(limit + 1) if kind != "latest" else b""
        if len(body) > limit:
            raise RuntimeError("Release download exceeds size limit")
        return body, response.geturl()


def resolve_version():
    explicit = os.environ.get("VERSION", "")
    if explicit:
        version = explicit
    else:
        try:
            body, _ = download(API, "api", 1024 * 1024)
            version = json.loads(body)["tag_name"]
        except urllib.error.HTTPError as err:
            if err.code not in (403, 429):
                raise
            _, final = download(RELEASES + "/latest", "latest")
            validate_redirect(RELEASES + "/latest", final, "latest")
            version = urllib.parse.urlsplit(final).path.rsplit("/", 1)[1]
    if not TAG.fullmatch(version):
        raise RuntimeError("Invalid release tag")
    return version


def extract_binary(compressed):
    # Expand within a global cap before examining any tar members. No archive
    # path is ever written to disk, including ignored bundled skill files.
    with gzip.GzipFile(fileobj=io.BytesIO(compressed)) as stream:
        expanded = stream.read(LIMIT + 1)
    if len(expanded) > LIMIT:
        raise RuntimeError("Release expanded bytes exceed 128 MiB")
    # tarfile hides extension headers while iterating. Count physical entries
    # too, before interpreting PAX/GNU metadata chains.
    offset, entries = 0, 0
    while offset + 512 <= len(expanded):
        header = expanded[offset:offset + 512]
        if not any(header):
            break
        entries += 1
        if entries > ENTRY_LIMIT:
            raise RuntimeError("Too many release entries")
        size = int(header[124:136].strip(b" \x00"), 8)
        if size < 0 or size > LIMIT:
            raise RuntimeError("Invalid or oversized release entry")
        offset += 512 + (size + 511) // 512 * 512
    binary = None
    with tarfile.open(fileobj=io.BytesIO(expanded), mode="r:") as archive:
        for count, entry in enumerate(archive):
            if count >= ENTRY_LIMIT:
                raise RuntimeError("Too many release entries")
            name = entry.name.rstrip("/")
            parts = PurePosixPath(name).parts
            if not name or name.startswith("/") or ".." in parts or "\\" in name or not (entry.isfile() or entry.isdir()):
                raise RuntimeError("Unsafe release entry")
            if name == "anx":
                if binary is not None or not entry.isfile():
                    raise RuntimeError("Invalid or duplicate release binary")
                with archive.extractfile(entry) as member:
                    binary = member.read(LIMIT + 1)
    if not binary:
        raise RuntimeError("Release has no anx binary")
    return binary


def sync_dir(directory):
    fd = os.open(str(directory), os.O_RDONLY)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def durable_file(path, data, mode=0o600):
    with open(path, "wb") as out:
        out.write(data)
        os.fchmod(out.fileno(), mode)
        out.flush()
        os.fsync(out.fileno())


def atomic_json(path, data):
    fd, temporary = tempfile.mkstemp(prefix=".anx-record-", dir=path.parent)
    os.close(fd)
    try:
        durable_file(temporary, (json.dumps(data) + "\n").encode())
        os.replace(temporary, path)
        sync_dir(path.parent)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def rename_durable(source, dest):
    os.replace(source, dest)
    sync_dir(dest.parent)
    if source.parent != dest.parent:
        sync_dir(source.parent)


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def recovery_digest(path):
    try:
        info = path.lstat()
    except FileNotFoundError:
        return ""
    if not stat.S_ISREG(info.st_mode):
        raise RuntimeError("Transaction path is not a regular file")
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(fd, "rb") as source:
        opened = os.fstat(source.fileno())
        if not stat.S_ISREG(opened.st_mode) or (info.st_dev, info.st_ino) != (opened.st_dev, opened.st_ino):
            raise RuntimeError("Transaction file changed while opening")
        checksum = hashlib.sha256()
        for block in iter(lambda: source.read(1024 * 1024), b""):
            checksum.update(block)
        return checksum.hexdigest()


def valid_digest(value):
    return isinstance(value, str) and re.fullmatch(r"[0-9a-f]{64}", value) is not None


def require_fields(value, fields):
    if not isinstance(value, dict) or set(value) != set(fields) or any(item is None for item in value.values()):
        raise RuntimeError("Incomplete recovery transaction object")


RECEIPT_FIELDS = ("managed_by", "version", "sha256", "installed_at")


def validate_receipt(record):
    require_fields(record, RECEIPT_FIELDS)
    if any(not isinstance(value, str) for value in record.values()):
        raise RuntimeError("Invalid recovery receipt field type")
    if record["managed_by"] != "anx" or not TAG.fullmatch(record["version"]) or not valid_digest(record["sha256"]):
        raise RuntimeError("Invalid recovery receipt identity")
    if not re.fullmatch(r"[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]+)?(?:Z|[+-][0-9]{2}:[0-9]{2})", record["installed_at"]):
        raise RuntimeError("Invalid recovery receipt timestamp")
    datetime.datetime.fromisoformat(record["installed_at"].replace("Z", "+00:00"))


def validate_transaction(path, tx):
    require_fields(tx, ("schema_version", "phase", "backup_path", "old_sha256", "old_record", "old_record_exists", "new_record"))
    if type(tx["schema_version"]) is not int or tx["schema_version"] != 1 or tx["phase"] not in ("prepared", "committed"):
        raise RuntimeError("Invalid recovery transaction")
    if not isinstance(tx["backup_path"], str) or not isinstance(tx["old_sha256"], str) or type(tx["old_record_exists"]) is not bool:
        raise RuntimeError("Invalid recovery transaction field type")
    validate_receipt(tx["new_record"])
    if tx["old_record_exists"]:
        validate_receipt(tx["old_record"])
        if tx["old_record"]["sha256"] != tx["old_sha256"]:
            raise RuntimeError("Original receipt differs from transaction digest")
    else:
        # Go encodes an absent receipt as a typed object with empty strings.
        if tx["old_record"] != {}:
            require_fields(tx["old_record"], RECEIPT_FIELDS)
            if any(value != "" for value in tx["old_record"].values()):
                raise RuntimeError("Unexpected original receipt in recovery transaction")
    if not tx["old_sha256"]:
        if tx["backup_path"] or tx["old_record_exists"]:
            raise RuntimeError("Inconsistent first-install transaction")
    else:
        backup = Path(tx["backup_path"])
        if not valid_digest(tx["old_sha256"]) or not backup.is_absolute() or os.path.normpath(tx["backup_path"]) != tx["backup_path"]:
            raise RuntimeError("Invalid original digest or rollback path")
        if backup.parent.resolve() != path.parent.resolve() or backup.name == path.name or not backup.name.startswith(".anx-rollback-"):
            raise RuntimeError("Invalid recovery transaction backup")


def inspect_transaction_files(path, tx):
    current = recovery_digest(path)
    if current and current not in (tx["old_sha256"], tx["new_record"]["sha256"]):
        raise RuntimeError("Binary differs from both transaction digests; preserving recovery evidence")
    if tx["phase"] == "committed" and current != tx["new_record"]["sha256"]:
        raise RuntimeError("Committed binary differs from transaction")
    if tx["backup_path"]:
        saved = recovery_digest(Path(tx["backup_path"]))
        if saved and saved != tx["old_sha256"]:
            raise RuntimeError("Rollback backup changed")
        if tx["phase"] == "prepared" and current != tx["old_sha256"] and not saved:
            raise RuntimeError("Rollback backup missing")
    return current


def receipt_path(path):
    return Path(str(path) + ".anx-install.json")


def journal_path(path):
    return Path(str(path) + ".anx-transaction.json")


@contextlib.contextmanager
def install_lock(path):
    # Shared with Go's filelock.TryLock. Stable inode, never age-based stealing
    # or unlinking. The kernel releases ownership on close, exit, or a crash.
    fd = os.open(str(path) + ".anx-update.lock", os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW, 0o600)
    try:
        if not stat.S_ISREG(os.fstat(fd).st_mode):
            raise RuntimeError("Invalid update lock")
        try:
            fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise RuntimeError("Another installer or updater owns the process lock")
        yield
    finally:
        os.close(fd)


def cleanup_transaction(path, tx):
    validate_transaction(path, tx)
    inspect_transaction_files(path, tx)
    if tx["backup_path"]:
        Path(tx["backup_path"]).unlink(missing_ok=True)
    journal_path(path).unlink(missing_ok=True)
    sync_dir(path.parent)


def recover(path):
    if not journal_path(path).exists():
        return
    tx = json.loads(journal_path(path).read_text())
    validate_transaction(path, tx)
    current = inspect_transaction_files(path, tx)
    backup = Path(tx["backup_path"]) if tx["backup_path"] else None
    if tx["phase"] == "committed":
        atomic_json(receipt_path(path), tx["new_record"])
    else:
        if not tx["old_sha256"]:
            # A complete first-install record authorizes removing its candidate
            # only, never unrelated bytes subsequently placed at this path.
            if current:
                path.unlink()
        elif current != tx["old_sha256"]:
            rename_durable(backup, path)
        if tx["old_record_exists"]:
            atomic_json(receipt_path(path), tx["old_record"])
        else:
            receipt_path(path).unlink(missing_ok=True)
            sync_dir(path.parent)
    cleanup_transaction(path, tx)


def probe(path, version):
    result = subprocess.run([str(path), "--json", "version"], capture_output=True, timeout=10, check=True)
    envelope = json.loads(result.stdout)
    if not envelope.get("ok") or envelope.get("result", {}).get("cli_version") != version:
        raise RuntimeError("Candidate architecture/version probe failed")


def install(path, binary, version):
    path.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix=".anx-update-", dir=path.parent) as staging:
        candidate = Path(staging) / "anx"
        durable_file(candidate, binary, 0o755)
        sync_dir(candidate.parent)
        sync_dir(path.parent)
        probe(candidate, version)  # Before acquiring ownership or modifying a target.
        with install_lock(path):
            recover(path)
            if path.is_symlink():
                raise RuntimeError("Refusing a symlink installation target")
            old_sha = recovery_digest(path)
            old_record_exists = receipt_path(path).exists()
            old_record_bytes = receipt_path(path).read_bytes() if old_record_exists else b""
            old_record = json.loads(old_record_bytes) if old_record_exists else {}
            reenroll = False
            if old_record_exists:
                validate_receipt(old_record)
                # Explicit installation may enroll changed bytes only after
                # strict recovery has finished. A stale receipt cannot serve as
                # rollback ownership for those bytes; preserve it separately.
                reenroll = bool(old_sha and old_record["sha256"] != old_sha)
                if reenroll:
                    old_record, old_record_exists = {}, False
            backup = ""
            if old_sha:
                fd, backup = tempfile.mkstemp(prefix=".anx-rollback-", dir=path.parent)
                os.close(fd)
                durable_file(backup, path.read_bytes(), stat.S_IMODE(path.stat().st_mode))
            sync_dir(path.parent)
            record = {"managed_by": "anx", "version": version, "sha256": hashlib.sha256(binary).hexdigest(),
                      "installed_at": datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")}
            tx = {"schema_version": 1, "phase": "prepared", "backup_path": backup, "old_sha256": old_sha,
                  "old_record": old_record, "old_record_exists": old_record_exists, "new_record": record}
            validate_transaction(path, tx)
            if reenroll:
                inspect_transaction_files(path, tx)
                evidence = Path(tempfile.mkdtemp(prefix=".anx-reenroll-", dir=path.parent))
                durable_file(evidence / "anx", Path(backup).read_bytes(), stat.S_IMODE(Path(backup).stat().st_mode))
                durable_file(evidence / "anx.anx-install.json", old_record_bytes)
                sync_dir(evidence)
                sync_dir(path.parent)
                print(f"Re-enrolling changed binary; previous binary and stale receipt preserved in {evidence}", file=sys.stderr)
            atomic_json(journal_path(path), tx)
            try:
                inspect_transaction_files(path, tx)
                rename_durable(candidate, path)
                probe(path, version)
                atomic_json(receipt_path(path), record)
                tx["phase"] = "committed"
                atomic_json(journal_path(path), tx)
            except Exception:
                recover(path)
                raise
            cleanup_transaction(path, tx)
        # Release install lock before skill delivery. Sync has a finite timeout
        # and cannot be mistaken for successful binary verification.
        result = subprocess.run([str(path), "--json", "skills", "sync"], capture_output=True, timeout=60, check=True)
        envelope = json.loads(result.stdout)
        if not envelope.get("ok") or any(skill.get("state") in ("conflict", "outdated", "drifted") for skill in envelope.get("result", {}).get("skills", [])):
            raise RuntimeError("Binary installed; managed skills need attention: run anx skills status")


def main():
    os_name = {"Darwin": "darwin", "Linux": "linux"}.get(platform.system())
    arch = {"x86_64": "amd64", "amd64": "amd64", "aarch64": "arm64", "arm64": "arm64"}.get(platform.machine())
    if not os_name or not arch:
        raise RuntimeError("Unsupported OS/architecture")
    version = resolve_version()
    archive = f"anx_{version}_{os_name}_{arch}.tar.gz"
    base = RELEASES + "/download/" + version
    content, _ = download(base + "/" + archive, "asset")
    checksums, _ = download(base + "/checksums.txt", "asset", 1024 * 1024)
    matches = [row.split()[0] for row in checksums.decode().splitlines() if len(row.split()) == 2 and row.split()[1] == archive]
    if len(matches) != 1 or matches[0] != hashlib.sha256(content).hexdigest():
        raise RuntimeError("Release checksum mismatch or missing manifest entry")
    path = Path(os.environ.get("INSTALL_DIR", str(Path.home() / ".local" / "bin"))).expanduser().resolve() / "anx"
    install(path, extract_binary(content), version)
    print(f"anx {version} installed to {path}; managed skills synchronized.")
    print("Next: anx update status; anx host enroll --name <host-slug>; anx doctor")


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        print(f"Error: {error}", file=sys.stderr)
        sys.exit(1)
PY
