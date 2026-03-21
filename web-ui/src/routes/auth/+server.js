import { json } from "@sveltejs/kit";

import {
  finishControlRegistration,
  startControlPasskeyRegistration,
  startControlSession,
  finishControlSession,
  logoutControlSession,
  loadControlSession,
} from "$lib/server/controlSession.js";

export async function POST({ request, cookies }) {
  const body = await request.json();
  const action = body.action;

  if (action === "register-start") {
    const email = String(body.email ?? "").trim();
    const displayName = String(body.display_name ?? "").trim();

    if (!email || !displayName) {
      return json(
        {
          error: {
            code: "invalid_request",
            message: "Email and display name are required.",
          },
        },
        { status: 400 },
      );
    }

    try {
      const result = await startControlPasskeyRegistration({
        email,
        display_name: displayName,
      });

      return json(result);
    } catch (error) {
      return json(
        {
          error: {
            code: "registration_start_failed",
            message:
              error instanceof Error
                ? error.message
                : "Failed to start registration",
          },
        },
        { status: 400 },
      );
    }
  }

  if (action === "register-finish") {
    const registrationSessionId = body.registration_session_id;
    const credential = body.credential;

    if (!registrationSessionId || !credential) {
      return json(
        {
          error: {
            code: "invalid_request",
            message: "Registration session and credential are required.",
          },
        },
        { status: 400 },
      );
    }

    try {
      const result = await finishControlRegistration(
        { request: { cookies } },
        registrationSessionId,
        credential,
      );

      return json({
        account: result.account,
      });
    } catch (error) {
      return json(
        {
          error: {
            code: "registration_finish_failed",
            message:
              error instanceof Error
                ? error.message
                : "Failed to finish registration",
          },
        },
        { status: 400 },
      );
    }
  }

  if (action === "login-start") {
    const email = String(body.email ?? "").trim();

    if (!email) {
      return json(
        { error: { code: "invalid_request", message: "Email is required." } },
        { status: 400 },
      );
    }

    try {
      const result = await startControlSession({ email });

      return json(result);
    } catch (error) {
      return json(
        {
          error: {
            code: "login_start_failed",
            message:
              error instanceof Error ? error.message : "Failed to start login",
          },
        },
        { status: 400 },
      );
    }
  }

  if (action === "login-finish") {
    const sessionId = body.session_id;
    const credential = body.credential;

    if (!sessionId || !credential) {
      return json(
        {
          error: {
            code: "invalid_request",
            message: "Session and credential are required.",
          },
        },
        { status: 400 },
      );
    }

    try {
      const result = await finishControlSession(
        { request: { cookies } },
        sessionId,
        credential,
      );

      return json({
        account: result.account,
      });
    } catch (error) {
      return json(
        {
          error: {
            code: "login_finish_failed",
            message:
              error instanceof Error ? error.message : "Failed to finish login",
          },
        },
        { status: 400 },
      );
    }
  }

  return json(
    { error: { code: "invalid_action", message: "Unknown action" } },
    { status: 400 },
  );
}

export async function DELETE({ cookies }) {
  try {
    await logoutControlSession({ cookies });
  } catch {
    // Ignore errors during logout
  }

  return json({ revoked: true });
}

export async function GET({ cookies }) {
  const session = await loadControlSession({ cookies });

  if (session?.account) {
    return json({
      account: session.account,
    });
  }

  return json({
    account: null,
  });
}
