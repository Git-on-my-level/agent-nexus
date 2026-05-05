import { redirect } from "@sveltejs/kit";

export function load({ params }) {
  throw redirect(
    308,
    `/hosted/organizations/${encodeURIComponent(params.orgId)}/billing`,
  );
}
