"""Host bridge: check in once, poll every configured derived agent, launch exact argv."""
from __future__ import annotations

import json
import logging
import os
import shutil
import subprocess
import threading
import time
import uuid

from .anx_client import Client, HostCLI
from .config import Config, Runtime

LOG = logging.getLogger(__name__)


class Bridge:
    def __init__(self, config: Config, cli: HostCLI | None = None, client: Client | None = None):
        self.config = config
        self.cli = cli or HostCLI(config)
        self.client = client or Client(config, self.cli)
        self.instance_id = "anx-bridge-" + uuid.uuid4().hex
        self.last_checkin = 0.0

    def validate_roster(self) -> list[str]:
        # Derive every configured agent before reading the host roster. A fresh
        # host may have no children until their first token requests.
        for name in self.config.runtimes:
            self.cli.token(name)
        first = next(iter(self.config.runtimes))
        host = self.client.host(first)
        if host.get("id") != self.config.host_id or host.get("slug") != self.config.host_slug or host.get("revoked_at"):
            raise ValueError("configured host does not match active enrollment")
        excluded = set(host.get("excluded_names") or [])
        enabled = {a["name"] for a in host.get("agents", []) if not a.get("revoked_at") and a["name"] not in excluded}
        configured = set(self.config.runtimes)
        if enabled != configured:
            raise ValueError(f"runtime set differs from enabled host agents: missing={sorted(enabled-configured)}, extra={sorted(configured-enabled)}")
        return sorted(enabled)

    def checkin(self) -> None:
        self.validate_roster()
        self.cli.checkin(self.instance_id)
        self.last_checkin = time.monotonic()

    def run_once(self, *, checkin: bool = True) -> int:
        if checkin and time.monotonic() - self.last_checkin >= self.config.checkin_seconds:
            self.checkin()
        count = 0
        for name in self.config.runtimes:
            for item in self.client.notifications(name):
                if item.get("delivery_status") != "requested":
                    continue
                if item.get("target_handle") != f"{name}.{self.config.host_slug}":
                    raise ValueError("notification target does not match derived handle")
                try:
                    self.handle(name, item)
                    count += 1
                except Exception:
                    LOG.exception("wake %s failed", item.get("wakeup_id"))
        return count

    def run_forever(self) -> None:
        threading.Thread(target=self._checkin_forever, name="anx-host-bridge-checkin", daemon=True).start()
        while True:
            try:
                self.run_once(checkin=False)
            except Exception:
                LOG.exception("bridge poll failed")
            time.sleep(self.config.poll_seconds)

    def _checkin_forever(self) -> None:
        while True:
            try:
                self.checkin()
            except Exception:
                LOG.exception("host bridge check-in failed")
            time.sleep(self.config.checkin_seconds)

    def handle(self, name: str, item: dict[str, Any]) -> None:
        wakeup_id = str(item["wakeup_id"])
        self.cli.wake("claim", wakeup_id, self.instance_id)
        try:
            packet = self.client.wake_packet(name, wakeup_id)
            self.launch(self.config.runtimes[name], packet, item)
            self.cli.wake("complete", wakeup_id, self.instance_id)
        except Exception as exc:
            self.cli.wake("fail", wakeup_id, self.instance_id, str(exc))
            raise

    def launch(self, runtime: Runtime, packet: dict[str, Any], item: dict[str, Any]) -> None:
        prompt = self._prompt(packet, item)
        env = os.environ.copy()
        for key in tuple(env):
            if key.startswith("AGENTCTL_"):
                env.pop(key)
        env.update(runtime.env)
        env["ANX_BASE_URL"] = self.config.base_url
        agentctl = shutil.which(self.config.agentctl)
        if agentctl:
            if runtime.name != runtime.adapter:
                env["ANX_AS"] = runtime.name
            else:
                env.pop("ANX_AS", None)
            execution = subprocess.run([agentctl, "id", "generate", "exec"], capture_output=True, text=True, check=True)
            execution_id = json.loads(execution.stdout)["result"]["id"]
            command = [agentctl, "run", "--execution-id", execution_id, "--adapter", runtime.adapter]
            card_slug = self._card_slug(packet)
            if card_slug:
                command += ["--label", f"anx.card.{card_slug}"]
            command += ["--prompt-stdin", "--prompt-delivery", "stdin", "--", *runtime.command]
            anx_bin = shutil.which(self.config.anx)
            if not anx_bin:
                raise RuntimeError("anx executable is not on PATH for subscription")
            subscription = [agentctl, "subscribe", "create", "--execution", execution_id,
                            "--destination", "command", "--target", anx_bin,
                            "--arg", "--as", "--arg", runtime.name,
                            "--arg", "runs", "--arg", "ingest"]
            # Subscriptions can bind a preallocated ID before the run exists.
            subprocess.run(subscription, cwd=runtime.cwd, env=env, capture_output=True, text=True, check=True)
            result = subprocess.run(command, input=prompt, cwd=runtime.cwd, env=env,
                                    capture_output=True, text=True)
            if result.returncode:
                raise RuntimeError(f"agentctl run failed ({result.returncode}): {result.stderr[-500:] or result.stdout[-500:]}")
            return
        env["ANX_AS"] = runtime.name
        result = subprocess.run(runtime.command, input=prompt, cwd=runtime.cwd, env=env,
                                capture_output=True, text=True)
        if result.returncode:
            raise RuntimeError(f"runtime failed ({result.returncode}): {result.stderr[-500:]}")

    @staticmethod
    def _card_slug(packet: dict[str, Any]) -> str:
        ref = str(packet.get("subject_ref") or packet.get("resolved_subject", {}).get("ref") or "")
        if ref.startswith("card:"):
            return ref[5:]
        return ""

    @staticmethod
    def _prompt(packet: dict[str, Any], item: dict[str, Any]) -> str:
        trigger = packet.get("trigger") or {}
        text = trigger.get("text") or item.get("trigger_text") or ""
        subject = packet.get("subject_ref") or packet.get("resolved_subject", {}).get("ref") or ""
        return (f"Agent Nexus wake for @{item['target_handle']}\n"
                f"Subject: {subject}\nThread: {item.get('thread_id', '')}\n"
                f"Trigger: {text}\n"
                "Use anx to read the context and post your response.\n")
