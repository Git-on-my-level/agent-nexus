const now = Date.now();

function iso(offsetMs) {
  return new Date(now + offsetMs).toISOString();
}

export function getKidsLemonadeStandSeedData() {
  return {
    actors: [
      {
        id: "actor-boss-kid",
        display_name: "Milo Bosserson",
        tags: ["kid", "boss", "manager"],
        created_at: iso(-14 * 24 * 60 * 60 * 1000),
      },
      {
        id: "actor-sales-kid",
        display_name: "Ruby Pitch",
        tags: ["kid", "sales", "front-stand"],
        created_at: iso(-13 * 24 * 60 * 60 * 1000),
      },
      {
        id: "actor-backoffice-kid",
        display_name: "Theo Squeeze",
        tags: ["kid", "prep", "supplies"],
        created_at: iso(-12 * 24 * 60 * 60 * 1000),
      },
    ],
    threads: [
      {
        id: "thread-kids-lemonade-main",
        type: "initiative",
        title: "Neighborhood Lemonade Stand Master Plan",
        status: "active",
        priority: "p1",
        tags: ["lemonade", "kids", "weekend"],
        key_artifacts: [
          "artifact-sales-scoreboard",
          "artifact-prep-checklist",
          "artifact-supply-stash",
        ],
        cadence: "daily",
        current_summary:
          "Sunny Saturday stand at the corner of Pine and 3rd. Ruby is working the front table, Theo is squeezing lemons and guarding the ice chest, and Milo is trying to be the boss without sounding too much like a tiny CEO. The big goal is to sell out one pitcher before the park soccer crowd goes home.",
        next_actions: [
          "Ruby tests a better chalkboard joke and price sign",
          "Theo chills the second batch before noon",
          "Milo checks whether cups and ice can survive the afternoon rush",
        ],
        next_check_in_at: iso(2 * 60 * 60 * 1000),
        updated_at: iso(-20 * 60 * 1000),
        updated_by: "actor-boss-kid",
        provenance: {
          sources: ["actor_statement:evt-kids-main-001"],
        },
      },
      {
        id: "thread-kids-lemonade-sales",
        type: "process",
        title: "Front Stand Sales, Smiles, and Sidewalk Pitches",
        status: "active",
        priority: "p2",
        tags: ["sales", "sign", "neighbors"],
        key_artifacts: [
          "artifact-sales-scoreboard",
          "artifact-sign-slogans",
        ],
        cadence: "hourly",
        current_summary:
          "Ruby noticed that kids stop when the sign is funny, but grown-ups only buy when the price is easy to read. The current chalkboard joke gets a laugh from exactly three dads and confuses everyone else. Free high-fives remain popular.",
        next_actions: [
          "Make the price line bigger than the joke line",
          "Try one silly slogan that is actually easy to understand",
          "Track which pitch gets the best reaction after soccer practice ends",
        ],
        next_check_in_at: iso(90 * 60 * 1000),
        updated_at: iso(-28 * 60 * 1000),
        updated_by: "actor-sales-kid",
        provenance: {
          sources: ["actor_statement:evt-kids-sales-001"],
        },
      },
      {
        id: "thread-kids-lemonade-backoffice",
        type: "process",
        title: "Kitchen Prep, Lemon Squeezing, and Supply Stash",
        status: "active",
        priority: "p2",
        tags: ["prep", "kitchen", "supplies"],
        key_artifacts: [
          "artifact-prep-checklist",
          "artifact-supply-stash",
          "artifact-weather-note",
        ],
        cadence: "hourly",
        current_summary:
          "Theo has one cold pitcher ready, another half-mixed batch on deck, and a very serious opinion about not wasting ice. Supply stash is okay for now, but if the weather stays hot they could run short on cups before the second park wave.",
        next_actions: [
          "Finish the second batch before the first pitcher gets low",
          "Count cups and ice before the lunch rush",
          "Keep extra lemon slices in the shade so they do not look sad",
        ],
        next_check_in_at: iso(75 * 60 * 1000),
        updated_at: iso(-26 * 60 * 1000),
        updated_by: "actor-backoffice-kid",
        provenance: {
          sources: ["actor_statement:evt-kids-backoffice-001"],
        },
      },
    ],
    artifacts: [
      {
        id: "artifact-sales-scoreboard",
        kind: "doc",
        thread_id: "thread-kids-lemonade-sales",
        summary: "Handwritten scoreboard of cups sold, favorite pitch, and neighbor reactions",
        refs: [
          "topic:kids-lemonade-sales",
          "topic:kids-lemonade-main",
        ],
        provenance: {
          sources: ["actor_statement:evt-kids-sales-001"],
        },
        created_at: iso(-32 * 60 * 1000),
        created_by: "actor-sales-kid",
        content_text: `# Scoreboard\n\n- Cups sold so far: 11\n- Best sales trick: "Ice-cold lemonade and one free high-five"\n- Funniest customer reaction: one toddler yelled "THIS IS A TINY RESTAURANT"\n- Current problem: the joke on the sign is bigger than the price\n`,
      },
      {
        id: "artifact-sign-slogans",
        kind: "doc",
        thread_id: "thread-kids-lemonade-sales",
        summary: "Draft chalkboard slogans and sign ideas for the front stand",
        refs: ["topic:kids-lemonade-sales"],
        provenance: {
          sources: ["actor_statement:evt-kids-sales-001"],
        },
        created_at: iso(-31 * 60 * 1000),
        created_by: "actor-sales-kid",
        content_text: `# Sign Ideas\n\n- Ice-cold lemonade, no boring drinks allowed\n- 50 cents a cup, 0 cents for a compliment to the stand\n- Fresh lemons, cold cups, excellent vibes\n- Make the price line giant so grown-ups stop squinting\n`,
      },
      {
        id: "artifact-prep-checklist",
        kind: "doc",
        thread_id: "thread-kids-lemonade-backoffice",
        summary: "Kitchen prep checklist for pitchers, lemons, sugar, and ice",
        refs: [
          "topic:kids-lemonade-backoffice",
          "topic:kids-lemonade-main",
        ],
        provenance: {
          sources: ["actor_statement:evt-kids-backoffice-001"],
        },
        created_at: iso(-30 * 60 * 1000),
        created_by: "actor-backoffice-kid",
        content_text: `# Prep Checklist\n\n1. Chill first pitcher before the sign goes outside\n2. Slice backup lemons before hands get sticky and chaotic\n3. Mix second batch by 11:45 so nobody has to panic-squeeze\n4. Keep ice chest closed unless absolutely necessary because Theo is right about the ice melting too fast\n`,
      },
      {
        id: "artifact-supply-stash",
        kind: "doc",
        thread_id: "thread-kids-lemonade-backoffice",
        summary: "Current supply count for lemons, sugar, cups, and ice",
        refs: [
          "topic:kids-lemonade-backoffice",
          "topic:kids-lemonade-main",
        ],
        provenance: {
          sources: ["actor_statement:evt-kids-backoffice-001"],
        },
        created_at: iso(-29 * 60 * 1000),
        created_by: "actor-backoffice-kid",
        content_text: `# Supply Stash\n\n- Lemons: enough for about 2.5 pitchers\n- Sugar: plenty\n- Cups: 18 left in the main stack, 10 backup cups in the garage box\n- Ice: one small cooler, probably enough if Theo stops opening it every minute to inspect it\n`,
      },
      {
        id: "artifact-weather-note",
        kind: "doc",
        thread_id: "thread-kids-lemonade-main",
        summary: "Weather and neighborhood timing note for the stand",
        refs: ["topic:kids-lemonade-main"],
        provenance: {
          sources: ["actor_statement:evt-kids-main-001"],
        },
        created_at: iso(-27 * 60 * 1000),
        created_by: "actor-boss-kid",
        content_text: `# Weather Note\n\n- Sunny and warm enough that cold drinks should keep moving\n- Soccer game at the park should end around 13:00, which is probably the next mini rush\n- Light breeze is good for customers and terrible for loose napkins\n`,
      },
    ],
    documents: [
      {
        id: "kid-boss-lemonade-plan",
        document: {
          id: "kid-boss-lemonade-plan",
          title: "Kid Boss Lemonade Plan",
          kind: "runbook",
          owner: "actor-boss-kid",
          status: "draft",
        },
        refs: [
          "topic:kids-lemonade-main",
          "topic:kids-lemonade-sales",
          "topic:kids-lemonade-backoffice",
          "artifact:artifact-sales-scoreboard",
          "artifact:artifact-prep-checklist",
        ],
        content_type: "text",
        content: `# Kid Boss Lemonade Plan\n\nStatus: draft\n\nToday's mission:\n- Sell enough lemonade to feel triumphant but not so much that Theo runs out of ice and starts yelling\n\nOpen questions:\n- Which sign joke should Ruby actually use?\n- When should the second pitcher be mixed?\n- What reminder does each kid need before the busy part of the day?\n`,
        actor_id: "actor-boss-kid",
      },
    ],
    events: [
      {
        id: "evt-kids-main-001",
        actor_id: "actor-boss-kid",
        type: "actor_statement",
        thread_id: "thread-kids-lemonade-main",
        refs: [
          "topic:kids-lemonade-main",
          "artifact:artifact-weather-note",
          "document:kid-boss-lemonade-plan",
        ],
        summary: "Boss kid kickoff: run a fun stand, avoid chaos, and sell out one pitcher before the park crowd",
        payload: {
          ask: "Sales kid should improve the pitch and sign. Backoffice kid should keep the pitchers cold and the cups stocked. Boss kid will make the final plan after both updates.",
          mood: "playful but still trying to be in charge",
        },
        provenance: {
          sources: ["inferred"],
        },
        ts: iso(-25 * 60 * 1000),
      },
      {
        id: "evt-kids-sales-001",
        actor_id: "actor-sales-kid",
        type: "actor_statement",
        thread_id: "thread-kids-lemonade-sales",
        refs: [
          "topic:kids-lemonade-sales",
          "artifact:artifact-sales-scoreboard",
          "artifact:artifact-sign-slogans",
        ],
        summary: "Sales kid note: funny signs help, but the price has to be impossible to miss",
        payload: {
          observation: "Customers stop for the funny sign, but adults buy faster when the price is giant and easy to read.",
          idea: "Lead with the cold lemonade pitch, then offer a free high-five if they smile.",
        },
        provenance: {
          sources: ["artifact:artifact-sales-scoreboard"],
        },
        ts: iso(-24 * 60 * 1000),
      },
      {
        id: "evt-kids-backoffice-001",
        actor_id: "actor-backoffice-kid",
        type: "actor_statement",
        thread_id: "thread-kids-lemonade-backoffice",
        refs: [
          "topic:kids-lemonade-backoffice",
          "artifact:artifact-prep-checklist",
          "artifact:artifact-supply-stash",
        ],
        summary: "Backoffice kid note: second batch timing matters more than dramatic speeches about success",
        payload: {
          observation: "One cold pitcher is ready, but the stand needs the second batch mixed before the first gets dangerously low.",
          warning: "Cups could get tight if the park crowd arrives hungry and thirsty at the same time.",
        },
        provenance: {
          sources: ["artifact:artifact-supply-stash"],
        },
        ts: iso(-23 * 60 * 1000),
      },
      {
        id: "evt-kids-main-002",
        actor_id: "actor-boss-kid",
        type: "actor_statement",
        thread_id: "thread-kids-lemonade-main",
        refs: [
          "thread:thread-kids-lemonade-main",
          "topic:kids-lemonade-main",
          "topic:kids-lemonade-sales",
          "topic:kids-lemonade-backoffice",
          "document:kid-boss-lemonade-plan",
        ],
        summary: "Boss kid reminder: nobody is allowed to act like this is a bank merger, but we are absolutely allowed to be organized",
        payload: {
          reminder: "Keep it fun, keep it cold, and keep the sign readable from the sidewalk.",
        },
        provenance: {
          sources: ["inferred"],
        },
        ts: iso(-18 * 60 * 1000),
      },
    ],
  };
}
