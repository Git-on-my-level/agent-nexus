import { redirect } from "@sveltejs/kit";

import { writeControlInviteToken } from "$lib/server/controlSession.js";

export async function load(event) {
  const { params } = event;
  const inviteToken = String(params.invite_token ?? "").trim();
  if (!inviteToken) {
    throw redirect(307, "/invites");
  }

  writeControlInviteToken(event, inviteToken);
  throw redirect(
    307,
    `/auth?invite=1&redirect=${encodeURIComponent("/dashboard")}`,
  );
}
