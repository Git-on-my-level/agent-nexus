package pm

// Exact predicates match the partial indexes installed by NewStore. Equality
// fixes every index prefix; implicit rowid ordering needs neither scan nor sort.
const proposalCandidateSQL = `SELECT body FROM pm_records WHERE kind='decision' AND workspace_id=? AND actor_id=? AND json_extract(body,'$.work_ref')=? AND json_extract(body,'$.scope')=? AND json_extract(body,'$.status')='awaiting_answer' ORDER BY rowid LIMIT 1`

const pendingHumanProposalSQL = `SELECT id FROM pm_records WHERE kind='decision' AND workspace_id=? AND actor_id=? AND json_extract(body,'$.work_ref')=? AND json_extract(body,'$.scope')=? AND json_extract(body,'$.status')='awaiting_answer' AND json_extract(body,'$.origin_kind')='human' ORDER BY rowid LIMIT 1`
