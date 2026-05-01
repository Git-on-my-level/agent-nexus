import httpx
import pytest

from types import SimpleNamespace

from anx_agent_bridge.anx_client import ANXClient, ANXClientError, ANXStreamDisconnected


class DummyAuthManager:
    def __init__(self):
        self.state = SimpleNamespace(actor_id="actor-123")

    def access_token(self, _client):
        return "token"


def test_create_event_includes_actor_id_from_auth_state(monkeypatch):
    client = ANXClient("http://anx.test", auth_manager=DummyAuthManager())
    captured = {}

    def fake_raw_request(method, path, **kwargs):
        captured["method"] = method
        captured["path"] = path
        captured["body"] = kwargs["json_body"]
        return {}

    monkeypatch.setattr(client, "raw_request", fake_raw_request)

    client.create_event(event={"type": "message_posted"})

    assert captured["method"] == "POST"
    assert captured["path"] == "/events"
    assert captured["body"]["actor_id"] == "actor-123"


def test_create_document_includes_actor_id_from_auth_state(monkeypatch):
    client = ANXClient("http://anx.test", auth_manager=DummyAuthManager())
    captured = {}

    def fake_raw_request(method, path, **kwargs):
        captured["method"] = method
        captured["path"] = path
        captured["body"] = kwargs["json_body"]
        return {}

    monkeypatch.setattr(client, "raw_request", fake_raw_request)

    client.create_document(document={"document_id": "doc-1"}, content={"ok": True})

    assert captured["path"] == "/docs"
    assert captured["body"]["actor_id"] == "actor-123"


def test_create_document_strips_legacy_lifecycle_fields(monkeypatch):
    client = ANXClient("http://anx.test", auth_manager=DummyAuthManager())
    captured = {}

    def fake_raw_request(method, path, **kwargs):
        captured["method"] = method
        captured["path"] = path
        captured["body"] = kwargs["json_body"]
        return {}

    monkeypatch.setattr(client, "raw_request", fake_raw_request)

    client.create_document(
        document={"document_id": "doc-1", "title": "Title", "status": "active", "state": "trashed"},
        content={"ok": True},
    )

    assert captured["path"] == "/docs"
    assert captured["body"]["document"] == {"document_id": "doc-1", "title": "Title"}


def test_upsert_document_omits_document_id_on_patch(monkeypatch):
    client = ANXClient("http://anx.test", auth_manager=DummyAuthManager())
    captured = {}

    monkeypatch.setattr(client, "get_document", lambda _document_id: {"revision": {"revision_id": "rev-1"}})

    def fake_update_document(document_id, **kwargs):
        captured["document_id"] = document_id
        captured["kwargs"] = kwargs
        return {"ok": True}

    monkeypatch.setattr(client, "update_document", fake_update_document)

    client.upsert_document(
        "doc-1",
        document={"document_id": "doc-1", "title": "Title", "status": "active"},
        content={"ok": True},
    )

    assert captured["document_id"] == "doc-1"
    assert captured["kwargs"]["document"] == {"title": "Title"}
    assert captured["kwargs"]["if_base_revision"] == "rev-1"


def test_stream_events_wraps_transport_disconnect(monkeypatch):
    client = ANXClient("http://anx.test", auth_manager=DummyAuthManager())

    class BrokenResponse:
        status_code = 200
        headers = {"content-type": "text/event-stream"}

        def iter_lines(self):
            raise httpx.RemoteProtocolError("incomplete chunked read")

    class BrokenStream:
        def __enter__(self):
            return BrokenResponse()

        def __exit__(self, exc_type, exc, tb):
            return False

    monkeypatch.setattr("anx_agent_bridge.anx_client.httpx.stream", lambda *args, **kwargs: BrokenStream())

    with pytest.raises(ANXStreamDisconnected, match="incomplete chunked read"):
        list(client.stream_events())


def test_stream_events_preserves_connect_error(monkeypatch):
    client = ANXClient("http://anx.test", auth_manager=DummyAuthManager())

    def raise_connect_error(*args, **kwargs):
        raise httpx.ConnectError("dial failed")

    monkeypatch.setattr("anx_agent_bridge.anx_client.httpx.stream", raise_connect_error)

    with pytest.raises(httpx.ConnectError, match="dial failed"):
        list(client.stream_events())


def test_stream_events_http_error_reads_body_before_decode(monkeypatch):
    """Streaming responses must be fully read before .text/.json or httpx raises ResponseNotRead."""
    client = ANXClient("http://anx.test", auth_manager=DummyAuthManager())

    class ErrorStreamResponse:
        status_code = 400
        headers: dict[str, str] = {}

        def __init__(self) -> None:
            self._body = b'{"code":"forbidden","message":"nope"}'
            self._read = False

        def read(self) -> bytes:
            self._read = True
            return self._body

        @property
        def content(self) -> bytes:
            return self._body

        @property
        def text(self) -> str:
            if not self._read:
                raise httpx.ResponseNotRead("Attempted to read streaming response body")
            return self._body.decode()

        def json(self) -> dict:
            import json as json_lib

            return json_lib.loads(self.text)

    class ErrorStream:
        def __enter__(self):
            return ErrorStreamResponse()

        def __exit__(self, exc_type, exc, tb):
            return False

    monkeypatch.setattr("anx_agent_bridge.anx_client.httpx.stream", lambda *args, **kwargs: ErrorStream())

    with pytest.raises(ANXClientError) as caught:
        list(client.stream_events())
    assert caught.value.status_code == 400
    assert caught.value.code == "forbidden"
    assert "nope" in str(caught.value)
