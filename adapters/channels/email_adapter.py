"""Email delivery adapter. SMTP settings come from the environment only."""
import os
import smtplib
import ssl
import sys
from email.message import EmailMessage
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))
import common


def smtp_transport(msg):
    host = os.environ["ANX_SMTP_HOST"]
    port = int(os.environ.get("ANX_SMTP_PORT", "587"))
    starttls = os.environ.get("ANX_SMTP_STARTTLS", "1").lower() not in ("0", "false", "no", "off")
    with smtplib.SMTP(host, port, timeout=30) as s:
        if starttls:
            s.starttls(context=ssl.create_default_context())
        user = os.environ.get("ANX_SMTP_USERNAME")
        if user:
            s.login(user, os.environ.get("ANX_SMTP_PASSWORD", ""))
        s.send_message(msg)


def make_sender(transport=smtp_transport, sender_addr=None):
    def send(address, subject, body):
        if not address or "@" not in address or any(c in address for c in "\r\n"):
            raise common.SendError("invalid recipient address")
        msg = EmailMessage()
        msg["From"] = sender_addr or os.environ.get("ANX_SMTP_FROM", "")
        msg["To"] = address
        msg["Subject"] = subject.replace("\r", " ").replace("\n", " ")
        msg.set_content(body)
        try:
            transport(msg)
        except smtplib.SMTPRecipientsRefused:
            raise common.SendError("recipient refused", False) from None
        except smtplib.SMTPAuthenticationError:
            raise common.SendError("smtp authentication failed", False) from None
        except smtplib.SMTPResponseException as e:
            raise common.SendError(f"smtp error {e.smtp_code}", 400 <= e.smtp_code < 500) from None
        except (smtplib.SMTPException, OSError) as e:
            raise common.SendError(f"smtp transport error {type(e).__name__}", True) from None
    return send


if __name__ == "__main__":
    common.run_once("email", make_sender())
