import { json } from "@sveltejs/kit";

import { getMockThread } from "$lib/mockCoreData";

export function GET({ params }) {
  const thread = getMockThread(params.threadId);

  if (!thread) {
    return json({ error: "Thread not found." }, { status: 404 });
  }

  return json({ thread });
}
