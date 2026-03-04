import { json } from "@sveltejs/kit";

import { createMockCommitment, listMockCommitments } from "$lib/mockCoreData";

export function GET({ url }) {
  const params = url.searchParams;
  const filters = {
    thread_id: params.get("thread_id") ?? undefined,
    owner: params.get("owner") ?? undefined,
    status: params.get("status") ?? undefined,
    due_before: params.get("due_before") ?? undefined,
    due_after: params.get("due_after") ?? undefined,
  };

  return json({
    commitments: listMockCommitments(filters),
  });
}

export async function POST({ request }) {
  const body = await request.json();

  if (
    !body?.actor_id ||
    !body?.commitment?.thread_id ||
    !body?.commitment?.title
  ) {
    return json(
      {
        error:
          "actor_id, commitment.thread_id, and commitment.title are required.",
      },
      { status: 400 },
    );
  }

  const created = createMockCommitment(body);
  return json({ commitment: created });
}
