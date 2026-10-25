---
title: "Measure the report delay before buying more capacity?"
situation: |-
  We send a daily sales report to 24 shops so managers can plan their morning orders. Reports normally arrive within 10 minutes of closing, and that was still true last week. Since Monday, the same reports have taken about 45 minutes. All 24 eventually arrived on each of the last three days, so we have evidence of delay, not missing sales data.

  The slowdown began when we moved report generation to a shared queue. We do not yet know whether reports are waiting behind other jobs or taking longer to calculate once they start. That distinction determines whether buying more capacity would help.
impact: >-
  Managers now wait an extra 35 minutes before they can use the report. Two shops have told support that they placed orders without it. If the queue is the cause, changing when reports run could restore delivery without a recurring bill. If calculation itself is slower, moving the schedule would leave the underlying problem in place.
recommendation:
  action: >-
    Measure one daily report run before buying more capacity.
  why: >-
    Record the waiting time and calculation time separately tomorrow. This should tell us which change addresses the delay, using the timing tools we already have. The tradeoff is one more morning of slow reports while we gather evidence; we cannot promise recovery tomorrow.
alternatives:
  - action: "Add capacity now for an estimated $300 a month."
    consequence: >-
      It may reduce the delay immediately, but could leave us paying more while reports still wait behind other work.
if_no_answer: >-
  We will keep the current queue and capacity, and continue checking that every report arrives. The delay remains unresolved. Please choose before tomorrow’s run if you want the measurement step included; no purchase will happen without approval.
refs: []
evidence: []
# Fictional example. Replace it with a short proposal to a busy boss with no context.
# What's happening: explain the baseline, what changed and the key numbers.
# Why it matters: show who is affected and why the choice depends on those facts.
# What I recommend: give the action, reason and tradeoff, including known cost.
# Alternatives: give real choices and their consequences, in plain sentences.
# If no answer: say what happens or stays blocked, only when known.
# Explain what the work does before naming its implementation. Write connected
# paragraphs that show why these facts lead to this decision, not terse labels.
# Use enough context to explain the choice; do not squeeze it into a word quota.
# Preserve uncertainty, conditions and numbers. Never invent facts, urgency,
# costs, approvals or a default. Preserve the original choices and conditions;
# later progress is dated context, not approval or proof that a gate passed.
# Do not turn unexplained terms into assumed mechanisms. Keep source-supplied
# choices and conditions; add no choices, deadlines or dependencies. A next-step
# promise is not an ask deadline. Use clearly conditional consequences.
# Omit unused optional fields; do not add filler.
# Keep the title near 80 characters. Answer buttons must stand alone.
# Put technical Markdown in frontmatter details; link evidence. Both are collapsed.
# When asked for a draft from supplied context, use only that context without
# tool calls or preparation narration. Do not perform the underlying work or
# search for missing facts. State material unknowns.
# Return only the document, with every field between the two --- delimiters.
# YAML evidence entries are objects: {label: "Source", url: "https://..."}.
# Label=URL is an inline flag syntax, not the YAML evidence shape.
# Every named pull request, even in details, needs its own pull-request URL;
# a checks or commit URL does not qualify. Omit unrelated reference history.
# Preview: anx ask --from-file ask.md --preview
# Publish: add subject_ref: card:<your-card> to frontmatter, then anx ask --from-file ask.md.
---
