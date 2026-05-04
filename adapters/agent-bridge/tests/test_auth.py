import pytest

from anx_agent_bridge.auth import AuthManager, AuthState
from anx_agent_bridge.anx_client import ANXClientError


class TokenClient:
    def __init__(self, responses):
        self.responses = list(responses)
        self.requests = []

    def raw_request(self, method, path, **kwargs):
        self.requests.append({"method": method, "path": path, **kwargs})
        response = self.responses.pop(0)
        if isinstance(response, BaseException):
            raise response
        return response


def auth_manager(tmp_path):
    auth = AuthManager(tmp_path / "auth.json")
    auth.state = AuthState(
        username="agent",
        agent_id="agent-1",
        actor_id="actor-1",
        key_id="key-1",
        public_key_b64="public",
        private_key_b64="private",
        access_token="expired",
        refresh_token="stale-refresh",
        expires_at_epoch=0,
    )
    auth.assertion_payload = lambda: {"grant_type": "assertion", "agent_id": "agent-1", "key_id": "key-1"}  # type: ignore[method-assign]
    return auth


def token_response(access_token="new-access", refresh_token="new-refresh"):
    return {
        "tokens": {
            "access_token": access_token,
            "refresh_token": refresh_token,
            "token_type": "Bearer",
            "expires_in": 3600,
        }
    }


def test_refresh_invalid_refresh_token_falls_back_to_assertion(tmp_path):
    auth = auth_manager(tmp_path)
    client = TokenClient(
        [
            ANXClientError(401, "invalid_token", "token is invalid, expired, or revoked"),
            token_response(),
        ]
    )

    state = auth.refresh(client)

    assert [request["json_body"]["grant_type"] for request in client.requests] == ["refresh_token", "assertion"]
    assert state.access_token == "new-access"
    assert state.refresh_token == "new-refresh"


@pytest.mark.parametrize(
    "error",
    [
        ANXClientError(503, "internal_error", "failed to issue token"),
        ANXClientError(401, "session_ended_by_account_status", "session ended"),
        RuntimeError("transport failed"),
    ],
)
def test_refresh_preserves_non_refresh_credential_failures(tmp_path, error):
    auth = auth_manager(tmp_path)
    client = TokenClient([error])

    with pytest.raises(type(error)) as caught:
        auth.refresh(client)

    assert caught.value is error
    assert [request["json_body"]["grant_type"] for request in client.requests] == ["refresh_token"]
