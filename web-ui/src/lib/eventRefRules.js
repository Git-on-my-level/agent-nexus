import eventRefRulesData from "./generated/event_ref_rules.json";

let cachedRules = null;

function getRules() {
  if (cachedRules === null) {
    cachedRules = eventRefRulesData;
  }
  return cachedRules;
}

export function getEventRefRule(eventType) {
  const rules = getRules();
  return rules.rules?.[eventType] ?? null;
}

export function hasEventRefRule(eventType) {
  const rules = getRules();
  return !!rules.rules?.[eventType];
}

export function validateEventRefRule(eventType, refs, payload = {}) {
  const rule = getEventRefRule(eventType);
  if (!rule) {
    return { valid: true, error: "" };
  }

  if (rule.thread_id === "required") {
    if (!payload.thread_id) {
      return {
        valid: false,
        error: `event.thread_id is required for event.type="${eventType}"`,
      };
    }
  }

  if (rule.refs_must_include?.length > 0) {
    const refsByPrefix = new Map();
    for (const ref of refs) {
      const colonIndex = ref.indexOf(":");
      if (colonIndex > 0) {
        const prefix = ref.slice(0, colonIndex);
        const count = refsByPrefix.get(prefix) ?? 0;
        refsByPrefix.set(prefix, count + 1);
      }
    }

    for (const pattern of rule.refs_must_include) {
      const colonIndex = pattern.indexOf(":");
      if (colonIndex > 0) {
        const prefix = pattern.slice(0, colonIndex);
        const requiredCount = pattern.split(",").filter((p) => p.trim().startsWith(prefix + ":")).length;
        const actualCount = refsByPrefix.get(prefix) ?? 0;
        if (actualCount < requiredCount && requiredCount === 1) {
          return {
            valid: false,
            error: `event.refs must include a "${prefix}:<id>" typed ref for event.type="${eventType}"`,
          };
        }
        if (actualCount < requiredCount) {
          return {
            valid: false,
            error: `event.refs must include at least ${requiredCount} refs with prefix "${prefix}" for event.type="${eventType}"`,
          };
        }
      }
    }
  }

  if (rule.conditional_refs?.length > 0) {
    const refsByPrefix = new Map();
    for (const ref of refs) {
      const colonIndex = ref.indexOf(":");
      if (colonIndex > 0) {
        const prefix = ref.slice(0, colonIndex);
        refsByPrefix.set(prefix, true);
      }
    }

    for (const cond of rule.conditional_refs) {
      const payloadValue = payload[cond.when.payload_field];
      if (String(payloadValue) !== cond.when.equals) {
        continue;
      }

      let hasRequired = false;
      for (const req of cond.must_have) {
        if (refsByPrefix.get(req.prefix)) {
          hasRequired = true;
          break;
        }
      }

      if (!hasRequired) {
        const required = cond.must_have.map((r) => `${r.prefix} prefix`).join(cond.condition === "or" ? " or " : " and ");
        return {
          valid: false,
          error: `event.refs must include ${required} when event.type="${eventType}" and payload.${cond.when.payload_field}="${cond.when.equals}"`,
        };
      }
    }
  }

  return { valid: true, error: "" };
}

export function validateCommitmentStatusRef(status, refValue) {
  const nextStatus = String(status ?? "").trim();
  const ref = String(refValue ?? "").trim();

  if (nextStatus !== "done" && nextStatus !== "canceled") {
    return { valid: true, error: "" };
  }

  if (!ref) {
    if (nextStatus === "done") {
      return {
        valid: false,
        error:
          "Status done requires a typed ref: artifact:<receipt_id> or event:<decision_event_id>.",
      };
    }

    return {
      valid: false,
      error: "Status canceled requires a typed ref: event:<decision_event_id>.",
    };
  }

  const refsByPrefix = new Map();
  const colonIndex = ref.indexOf(":");
  if (colonIndex > 0) {
    const prefix = ref.slice(0, colonIndex);
    refsByPrefix.set(prefix, true);
  } else {
    return {
      valid: false,
      error: "Status evidence ref must be a valid typed ref (<prefix>:<value>).",
    };
  }

  if (nextStatus === "done") {
    if (refsByPrefix.get("artifact") || refsByPrefix.get("event")) {
      return { valid: true, error: "" };
    }

    return {
      valid: false,
      error:
        "Status done requires artifact:<receipt_id> or event:<decision_event_id>.",
    };
  }

  if (!refsByPrefix.get("event")) {
    return {
      valid: false,
      error: "Status canceled requires event:<decision_event_id>.",
    };
  }

  return { valid: true, error: "" };
}
