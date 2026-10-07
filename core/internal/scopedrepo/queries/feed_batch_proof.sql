SELECT p.source_revision FROM scope_feed_selection_proofs p
 JOIN scope_feed_proof_clock c ON c.singleton=1 AND c.revision=p.source_revision
 WHERE p.authority_binding=? AND p.format_version=1
 AND p.policy_version=? AND p.projector_version=?
 AND p.audience_compared=1 AND p.lifecycle_compared=1
 AND p.payload_compared=1 AND p.counters_compared=1 AND p.disjoint=1
 AND typeof(p.source_revision)='integer' AND p.source_revision>0
