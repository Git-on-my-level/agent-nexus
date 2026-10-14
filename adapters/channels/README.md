# Human notification channel adapters

Host-local Python 3 senders that pull pending deliveries from core, send them
over a channel, and post receipts. Core owns the queue, leases, retry and
backoff; these scripts own only the channel credentials. Run them on an enrolled
host as the agent that owns the adapter declaration.

Declare the adapter first (human or explicitly granted auth-admin identity):

```json
{
  "name": "email",
  "description": "Email notifications for overdue asks and daily digests",
  "agent_id": "notifier.my-host",
  "expected_interval": "1m",
  "series": [],
  "delivers": ["email"]
}
```

Save as `email.json`, then `anx adapters declare --body-file email.json`. Use
`"delivers": ["telegram"]` for Telegram.

Humans choose their channel address (email address or Telegram chat id) and the
push/digest toggles in the workspace notification settings
(`PUT /me/notification-settings`). Deliveries with no configured address are not
queued for the channel.

Each run claims up to 10 deliveries (`anx deliveries claim`), sends them, and
posts `anx deliveries receipt` per delivery. Transient failures (timeouts, 5xx,
429, SMTP 4xx) are reported retryable and core reschedules them; bad addresses
and auth failures are permanent. One failing delivery never blocks the others.
Messages are short plain text rendered from the payload; hidden titles show as
"(details hidden)".

Cron, once per minute:

```cron
* * * * * ANX_AS=notifier ANX_SMTP_HOST=smtp.example.com ANX_SMTP_FROM=anx@example.com /usr/bin/python3 /opt/anx-adapters/email_adapter.py >> /var/log/anx-email.log 2>&1
* * * * * ANX_AS=notifier /usr/bin/python3 /opt/anx-adapters/telegram_adapter.py >> /var/log/anx-telegram.log 2>&1
```

Environment (secrets come from the environment only, never from files in git):

| Variable | Adapter | Notes |
| --- | --- | --- |
| `ANX_SMTP_HOST`, `ANX_SMTP_PORT` | email | port defaults to 587 |
| `ANX_SMTP_USERNAME`, `ANX_SMTP_PASSWORD` | email | optional login |
| `ANX_SMTP_FROM` | email | sender address |
| `ANX_SMTP_STARTTLS` | email | default on; `0` disables |
| `ANX_TELEGRAM_BOT_TOKEN` | telegram | never logged or put in errors |

Test: `python3 -m unittest adapters/channels/test_channels.py`.
