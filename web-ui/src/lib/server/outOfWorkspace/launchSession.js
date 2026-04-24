import { redirect } from "@sveltejs/kit";

/**
 * @param {import("./contract.js").LaunchInstruction} instruction
 */
export function handleLaunchInstruction(instruction) {
  if (instruction?.kind === "redirect") {
    throw redirect(303, instruction.finishUrl);
  }
  if (instruction?.kind === "needs_signin") {
    throw redirect(307, instruction.signInUrl);
  }
}
