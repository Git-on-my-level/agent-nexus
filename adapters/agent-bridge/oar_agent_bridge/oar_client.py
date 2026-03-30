from __future__ import annotations

import json
import logging
import time
from dataclasses import dataclass
from typing import Any, Iterable, Iterator
from urllib.parse import urlencode

import httpx

LOGGER = logging.getLogger(__name__)


class OARClientError(RuntimeError):
    def __init__(self, status_code: int, code: str, message: str, payload: Any | None = None) -> None:
        self.status_code = status_code
        self.code = code
        self.payload = payload
        super().__init__(message)


class OARStreamDisconnected(RuntimeError):
    pass


@dataclass(slots=True)
class SSEMessage:
    event_id: str | None
    event: str | None
    data: str


class OARClient:
    def __init__(self, base_url: str, verify_ssl: bool = True, auth_manager: "AuthManager | None" = None, timeout_seconds: int = 60) -> None:
        self.base_url = base_url.rstrip("/")
        self.verify_ssl = verify_ssl
        self.auth_manager = auth_manager
        self.timeout_seconds = timeout_seconds
        self._http = httpx.Client(base_url=self.base_url, verify=self.verify_ssl, timeout=timeout_seconds)

    def close(self) -> None:
        self._http.close()

    def _headers(self, authenticated: bool = True, extra: dict[str, str] | None = None) -> dict[str, str]:
        headers = {"Accept": "application/json"}
        if authenticated and self.auth_manager is not None:
            token = self.auth_manager.access_token(self)
            headers["Authorization"] = f"Bearer {token}"
        if extra:
            headers.update(extra)
        return headers

    def _actor_id(self) -> str | None:
        if self.auth_manager is None or self.auth_manager.state is None:
            return None
        actor_id = str(self.auth_manager.state.actor_id).strip()
        return actor_id or None

    def raw_request(
        self,
        method: str,
        path: str,
        *,
        params: dict[str, Any] | None = None,
        json_body: Any | None = None,
        authenticated: bool = True,
        headers: dict[str, str] | None = None,
    ) -> Any:
        response = self._http.request(
            method,
            path,
            params=params,
            json=json_body,
            headers=self._headers(authenticated=authenticated, extra=headers),
        )
        return self._decode_response(response)

    def _decode_response(self, response: httpx.Response) -> Any:
        if response.status_code >= 400:
            message = response.text
            code = "http_error"
            payload: Any | None = None
            try:
                payload = response.json()
                if isinstance(payload, dict):
                    code = str(payload.get("code", code))
                    message = str(payload.get("message", message))
            except Exception:
                pass
            raise OARClientError(response.status_code, code, message, payload)
        if response.headers.get("content-type", "").startswith("application/json"):
            return response.json()
        if not response.content:
            return None
        return response.text

    def list_principals(self, limit: int = 200) -> list[dict[str, Any]]:
        principals: list[dict[str, Any]] = []
        cursor: str | None = None
        while True:
            params: dict[str, Any] = {"limit": limit}
            if cursor:
                params["cursor"] = cursor
            payload = self.raw_request("GET", "/auth/principals", params=params)
            principals.extend(payload.get("principals", []))
            cursor = payload.get("next_cursor")
            if not cursor:
                break
        return principals

    def get_current_agent(self) -> dict[str, Any]:
        return self.raw_request("GET", "/agents/me")

    def get_document(self, document_id: str) -> dict[str, Any]:
        return self.raw_request("GET", f"/docs/{document_id}")

    def create_document(self, *, document: dict[str, Any], content: Any, content_type: str = "structured", request_key: str | None = None) -> dict[str, Any]:
        body: dict[str, Any] = {
            "document": document,
            "content": content,
            "content_type": content_type,
        }
        actor_id = self._actor_id()
        if actor_id:
            body["actor_id"] = actor_id
        if request_key:
            body["request_key"] = request_key
        return self.raw_request("POST", "/docs", json_body=body)

    def update_document(self, document_id: str, *, if_base_revision: str, document: dict[str, Any] | None = None, content: Any | None = None, content_type: str = "structured") -> dict[str, Any]:
        body: dict[str, Any] = {
            "if_base_revision": if_base_revision,
            "content": content,
            "content_type": content_type,
        }
        actor_id = self._actor_id()
        if actor_id:
            body["actor_id"] = actor_id
        if document is not None:
            body["document"] = document
        return self.raw_request("PATCH", f"/docs/{document_id}", json_body=body)

    def upsert_document(self, document_id: str, *, document: dict[str, Any], content: Any, content_type: str = "structured", request_key: str | None = None) -> dict[str, Any]:
        try:
            current = self.get_document(document_id)
        except OARClientError as exc:
            if exc.status_code != 404:
                raise
            document = dict(document)
            document.setdefault("document_id", document_id)
            return self.create_document(document=document, content=content, content_type=content_type, request_key=request_key)
        revision = current["revision"]
        update_document = dict(document)
        update_document.pop("document_id", None)
        return self.update_document(document_id, if_base_revision=str(revision["revision_id"]), document=update_document, content=content, content_type=content_type)

    def create_event(self, *, event: dict[str, Any], request_key: str | None = None) -> dict[str, Any]:
        body: dict[str, Any] = {"event": event}
        actor_id = self._actor_id()
        if actor_id:
            body["actor_id"] = actor_id
        if request_key:
            body["request_key"] = request_key
        return self.raw_request("POST", "/events", json_body=body)

    def get_event(self, event_id: str) -> dict[str, Any]:
        return self.raw_request("GET", f"/events/{event_id}")

    def create_artifact(self, *, artifact: dict[str, Any], content: Any, content_type: str = "structured") -> dict[str, Any]:
        body: dict[str, Any] = {"artifact": artifact, "content": content, "content_type": content_type}
        actor_id = self._actor_id()
        if actor_id:
            body["actor_id"] = actor_id
        return self.raw_request("POST", "/artifacts", json_body=body)

    def get_artifact(self, artifact_id: str) -> dict[str, Any]:
        return self.raw_request("GET", f"/artifacts/{artifact_id}")

    def get_artifact_content(self, artifact_id: str) -> Any:
        response = self._http.get(
            f"/artifacts/{artifact_id}/content",
            headers=self._headers(),
        )
        if response.status_code >= 400:
            return self._decode_response(response)
        content_type = response.headers.get("content-type", "")
        if content_type.startswith("application/json"):
            return response.json()
        text = response.text
        try:
            return json.loads(text)
        except Exception:
            return text

    def get_thread_workspace(self, thread_id: str, *, include_artifact_content: bool = False, include_related_event_content: bool = False) -> dict[str, Any]:
        params = {
            "include_artifact_content": str(include_artifact_content).lower(),
            "include_related_event_content": str(include_related_event_content).lower(),
        }
        return self.raw_request("GET", f"/threads/{thread_id}/workspace", params=params)

    def list_agent_notifications(self, *, statuses: Iterable[str] | None = None, order: str = "desc") -> list[dict[str, Any]]:
        params: list[tuple[str, str]] = []
        if statuses:
            for status in statuses:
                normalized = str(status).strip()
                if normalized:
                    params.append(("status", normalized))
        if order:
            params.append(("order", str(order).strip()))
        path = "/agent-notifications"
        if params:
            path = f"{path}?{urlencode(params)}"
        payload = self.raw_request("GET", path)
        items = payload.get("items") if isinstance(payload, dict) else None
        if not isinstance(items, list):
            return []
        return [item for item in items if isinstance(item, dict)]

    def mark_agent_notification_read(self, wakeup_id: str) -> dict[str, Any]:
        body: dict[str, Any] = {"wakeup_id": wakeup_id}
        actor_id = self._actor_id()
        if actor_id:
            body["actor_id"] = actor_id
        return self.raw_request("POST", "/agent-notifications/read", json_body=body)

    def dismiss_agent_notification(self, wakeup_id: str) -> dict[str, Any]:
        body: dict[str, Any] = {"wakeup_id": wakeup_id}
        actor_id = self._actor_id()
        if actor_id:
            body["actor_id"] = actor_id
        return self.raw_request("POST", "/agent-notifications/dismiss", json_body=body)

    def stream_events(self, *, types: Iterable[str] | None = None, thread_id: str | None = None, last_event_id: str | None = None, heartbeat_timeout_seconds: int = 120) -> Iterator[dict[str, Any]]:
        params_list: list[tuple[str, str]] = []
        if thread_id:
            params_list.append(("thread_id", thread_id))
        if types:
            for item in types:
                params_list.append(("type", item))
        if last_event_id:
            params_list.append(("last_event_id", last_event_id))
        query = urlencode(params_list)
        path = "/events/stream"
        if query:
            path = f"{path}?{query}"

        headers = self._headers()
        if last_event_id:
            headers["Last-Event-ID"] = last_event_id
        timeout = httpx.Timeout(connect=10.0, read=heartbeat_timeout_seconds, write=10.0, pool=10.0)
        with httpx.stream("GET", f"{self.base_url}{path}", headers=headers, verify=self.verify_ssl, timeout=timeout) as response:
            if response.status_code >= 400:
                raise self._decode_response(response)
            current_id: str | None = None
            current_event: str | None = None
            data_lines: list[str] = []
            try:
                for raw_line in response.iter_lines():
                    line = raw_line if isinstance(raw_line, str) else raw_line.decode("utf-8")
                    if line == "":
                        if data_lines:
                            data = "\n".join(data_lines)
                            yield {
                                "id": current_id,
                                "event": current_event,
                                "data": data,
                            }
                        current_id = None
                        current_event = None
                        data_lines = []
                        continue
                    if line.startswith(":"):
                        continue
                    field, _, value = line.partition(":")
                    value = value.lstrip(" ")
                    if field == "id":
                        current_id = value
                    elif field == "event":
                        current_event = value
                    elif field == "data":
                        data_lines.append(value)
            except (httpx.ReadError, httpx.RemoteProtocolError) as exc:
                raise OARStreamDisconnected(str(exc)) from exc
            if data_lines:
                yield {"id": current_id, "event": current_event, "data": "\n".join(data_lines)}


from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from .auth import AuthManager
