import { proxyHostedControlPlaneRequest } from "$lib/server/hostedControlPlaneProxy.js";

/** @param {import('@sveltejs/kit').RequestEvent} event */
export function GET(event) {
  return proxyHostedControlPlaneRequest(event, "GET");
}

/** @param {import('@sveltejs/kit').RequestEvent} event */
export function POST(event) {
  return proxyHostedControlPlaneRequest(event, "POST");
}

/** @param {import('@sveltejs/kit').RequestEvent} event */
export function PATCH(event) {
  return proxyHostedControlPlaneRequest(event, "PATCH");
}

/** @param {import('@sveltejs/kit').RequestEvent} event */
export function DELETE(event) {
  return proxyHostedControlPlaneRequest(event, "DELETE");
}
