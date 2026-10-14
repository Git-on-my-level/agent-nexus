# Design

Principles that every surface and every module is built against. Product
position lives in [architecture/foundation.md](architecture/foundation.md).

## Principles

1. **Agents state claims, evidence and promises. The product computes state.**
   Nothing on a human screen is authored status. Authored prose always carries
   an author and an as-of time.
2. **One clock: promise versus evidence.** A thing is quiet when it has passed
   the next signal its own author promised. No workspace-tuned thresholds, and
   no health taxonomy that needs an explainer.
3. **Judgment lives in agents, not formulas.** Merging, mooting, batching and
   ranking edge cases are PM jobs with an undoable trail. Formulas are the
   normal case whenever no PM is running.
4. **Feedback at the point of action, not rules in the prompt.** A write that
   degrades the human view returns a warning with the corrected command. The
   participant skill shrinks as the warnings grow, and an outcome eval proves
   the shrink did not cost behavior.
5. **Attention is account-level; truth is workspace-level.** A hosted shell may
   aggregate decisions across workspaces. Core never learns about another
   workspace. Self-host is one workspace.
