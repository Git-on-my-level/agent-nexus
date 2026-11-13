/**
 * A batch decision: several open asks shown as one decision. Asks filed
 * together through POST /asks/batch carry a `filed:` key and cannot be
 * ungrouped; an accepted curation batch can.
 *
 * A filed batch whose members carry their own proposals is answered as
 * numbered questions (`questions`): the shared context once on top, one
 * answer per question, and one submit that may leave some questions open.
 * Older payloads without per-member proposals keep the grouped view.
 *
 * Pure helpers: `normalizeBatch` takes brief.js's own normalizers so the two
 * files cannot drift, and `batchAnswers` builds the respond body.
 */

function text(value) {
  return String(value ?? "").trim();
}

function normalizeContext(raw) {
  if (!raw || typeof raw !== "object") return null;
  const context = {
    title: text(raw.title),
    situation: text(raw.situation),
    impact: text(raw.impact),
    details: text(raw.details),
    evidence: (Array.isArray(raw.evidence) ? raw.evidence : [])
      .filter((link) => link && text(link.url))
      .map((link) => ({ label: text(link.label), url: text(link.url) })),
  };
  return context.situation || context.impact || context.details
    ? context
    : null;
}

/**
 * @param {any} raw the brief decision's `batch`
 * @param {{ normalizeAskPresentation: Function, refTitle: Function }} helpers
 */
export function normalizeBatch(raw, { normalizeAskPresentation, refTitle }) {
  if (!raw || typeof raw !== "object") return null;
  const key = text(raw.key);
  if (!key) return null;
  const members = (Array.isArray(raw.members) ? raw.members : [])
    .filter((member) => member && typeof member === "object")
    .map((member) => {
      const eventRef =
        text(member.event_ref) ||
        text(member.eventRef) ||
        (text(member.event_id) ? `event:${text(member.event_id)}` : "");
      return {
        ref: text(member.ref),
        inboxItemId: text(member.inbox_item_id) || text(member.inboxItemId),
        ...(eventRef ? { eventRef } : {}),
        title: text(member.title),
        ...normalizeAskPresentation(member),
        subjectRef: text(member.subject_ref) || text(member.subjectRef),
        blocks: (Array.isArray(member.blocks) ? member.blocks : [])
          .map(refTitle)
          .filter(Boolean),
        proposals: (Array.isArray(member.proposals) ? member.proposals : [])
          .map(text)
          .filter(Boolean),
        position: Number(member.position) || 0,
        multiSelect: member.multi_select === true,
        note: text(member.note),
      };
    })
    .filter((member) => member.ref || member.title);
  const filed = key.startsWith("filed:");
  return {
    key,
    ungroupable: !filed,
    count: Math.max(Number(raw.count) || 0, members.length),
    members,
    context: normalizeContext(raw.context),
    questions:
      filed &&
      members.length > 0 &&
      members.every((member) => member.eventRef && member.proposals.length),
  };
}

/**
 * The respond body for the questions answered so far. `choices` maps an ask
 * ref to the chosen proposal indexes, `other` to the reader's own words,
 * which win over a choice. Unanswered questions are left out; when any are,
 * the body is partial and those questions stay open.
 *
 * @param {Array<{ eventRef: string, proposals: string[] }>} members
 * @param {Record<string, number[]>} choices
 * @param {Record<string, string>} other
 */
export function batchAnswers(members, choices, other) {
  const answers = [];
  for (const member of members) {
    const own = text(other[member.eventRef]);
    const picked = (choices[member.eventRef] ?? [])
      .filter((index) => index >= 0 && index < member.proposals.length)
      .sort((a, b) => a - b)
      .map((index) => member.proposals[index]);
    const responseText = own || picked.join("; ");
    if (responseText) {
      answers.push({
        ask_ref: member.eventRef,
        response_text: responseText,
        outcome: "answered",
      });
    }
  }
  if (!answers.length) return null;
  return answers.length < members.length
    ? { answers, partial: true }
    : { answers };
}
