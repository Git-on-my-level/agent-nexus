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
      {
        id: "actor-parent-operator",
        display_name: "Pat (Parent operator)",
        tags: ["human", "operator", "parent"],
        created_at: iso(-15 * 24 * 60 * 60 * 1000),
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
          "artifact-rival-stand-rumor",
        ],
        cadence: "daily",
        current_summary:
          "Sunny Saturday stand at the corner of Pine and 3rd. Ruby is working the front table, Theo is guarding the ice chest like a tiny dragon, and Milo has decided that today absolutely requires both a mission board and a dramatic speech. The soccer crowd should hit later, a rival cookie table rumor is floating around, and the team is debating whether a surprise mint special is genius or a reckless sugar accident.",
        next_actions: [
          "Milo creates a mission board before the stand turns into pure shouting",
          "Ruby tests one cleaner sign line and one fun gimmick for the soccer crowd",
          "Theo decides whether the mint special is operationally possible without melting the ice supply",
        ],
        next_check_in_at: iso(2 * 60 * 60 * 1000),
        updated_at: iso(-18 * 60 * 1000),
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
          "artifact-neighborhood-flyer",
        ],
        cadence: "hourly",
        current_summary:
          "Ruby noticed that kids stop when the sign is funny, but grown-ups only buy when the price is huge and impossible to miss. The current joke lands with exactly three dads and one confused toddler. Ruby also suspects the mint special might sound fancy enough to attract attention if the sign does not make it look suspicious.",
        next_actions: [
          "Make the price line bigger than the joke line",
          "Try one clear soccer-crowd pitch and one sillier backup pitch",
          "Ask Theo whether mint lemonade is a real option or a fantasy",
        ],
        next_check_in_at: iso(90 * 60 * 1000),
        updated_at: iso(-24 * 60 * 1000),
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
          "artifact-secret-menu-sketch",
        ],
        cadence: "hourly",
        current_summary:
          "Theo has one cold pitcher ready, another half-mixed batch on deck, and a deeply serious opinion about not wasting ice. Supply stash is okay for now, but cups could get tight after the soccer game. Theo thinks the mint special is possible only if the second batch is mixed first and nobody keeps opening the cooler to stare at it.",
        next_actions: [
          "Finish the second batch before the first pitcher gets low",
          "Count cups and ice before the lunch rush",
          "Decide whether the mint special is worth the extra prep chaos",
        ],
        next_check_in_at: iso(75 * 60 * 1000),
        updated_at: iso(-22 * 60 * 1000),
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
        created_at: iso(-34 * 60 * 1000),
        created_by: "actor-sales-kid",
        content_text: `# Scoreboard\n\n- Cups sold so far: 11\n- Best sales trick: "Ice-cold lemonade and one free high-five"\n- Funniest customer reaction: one toddler yelled "THIS IS A TINY RESTAURANT"\n- Current problem: the joke on the sign is bigger than the price\n- If the soccer crowd shows up fast, Ruby wants one louder sign and one louder voice\n`,
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
        created_at: iso(-33 * 60 * 1000),
        created_by: "actor-sales-kid",
        content_text: `# Sign Ideas\n\n- Ice-cold lemonade, no boring drinks allowed\n- 50 cents a cup, 0 cents for a compliment to the stand\n- Fresh lemons, cold cups, excellent vibes\n- Make the price line giant so grown-ups stop squinting\n- Maybe: "Soccer practice was hard, lemonade is easy"\n`,
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
        created_at: iso(-32 * 60 * 1000),
        created_by: "actor-backoffice-kid",
        content_text: `# Prep Checklist\n\n1. Chill first pitcher before the sign goes outside\n2. Slice backup lemons before hands get sticky and chaotic\n3. Mix second batch by 11:45 so nobody has to panic-squeeze\n4. Keep ice chest closed unless absolutely necessary because Theo is right about the ice melting too fast\n5. If mint special happens, prep it only after pitcher two is safe\n`,
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
        created_at: iso(-31 * 60 * 1000),
        created_by: "actor-backoffice-kid",
        content_text: `# Supply Stash\n\n- Lemons: enough for about 2.5 pitchers\n- Sugar: plenty\n- Cups: 18 left in the main stack, 10 backup cups in the garage box\n- Ice: one small cooler, probably enough if Theo stops opening it every minute to inspect it\n- Mint leaves: enough for a tiny test batch, not enough for a full fake-cafe empire\n`,
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
        created_at: iso(-30 * 60 * 1000),
        created_by: "actor-boss-kid",
        content_text: `# Weather Note\n\n- Sunny and warm enough that cold drinks should keep moving\n- Soccer game at the park should end around 13:00, which is probably the next mini rush\n- Light breeze is good for customers and terrible for loose napkins\n`,
      },
      {
        id: "artifact-rival-stand-rumor",
        kind: "doc",
        thread_id: "thread-kids-lemonade-main",
        summary: "Neighborhood rumor note about a cookie table possibly appearing across the street",
        refs: ["topic:kids-lemonade-main"],
        provenance: {
          sources: ["inferred"],
        },
        created_at: iso(-28 * 60 * 1000),
        created_by: "actor-boss-kid",
        content_text: `# Rival Table Rumor\n\n- Jamie might set up a cookie table nearby after lunch\n- Nobody knows if this is real or just cousin gossip\n- If it happens, Ruby wants a sharper sign and Milo wants to call it "healthy beverage competition"\n`,
      },
      {
        id: "artifact-secret-menu-sketch",
        kind: "doc",
        thread_id: "thread-kids-lemonade-backoffice",
        summary: "Messy sketch for a possible surprise mint lemonade special",
        refs: [
          "topic:kids-lemonade-backoffice",
          "topic:kids-lemonade-main",
        ],
        provenance: {
          sources: ["inferred"],
        },
        created_at: iso(-27 * 60 * 1000),
        created_by: "actor-backoffice-kid",
        content_text: `# Secret Menu Sketch\n\n- Name idea: "Cool Mint Mega Lemon"\n- Risk: if the mint bits look like yard clippings this whole thing is over\n- Theo says only a tiny test batch is allowed until pitcher two is safe\n`,
      },
      {
        id: "artifact-neighborhood-flyer",
        kind: "doc",
        thread_id: "thread-kids-lemonade-sales",
        summary: "Tiny handwritten flyer ideas for luring the park crowd over",
        refs: [
          "topic:kids-lemonade-sales",
          "topic:kids-lemonade-main",
        ],
        provenance: {
          sources: ["inferred"],
        },
        created_at: iso(-26 * 60 * 1000),
        created_by: "actor-sales-kid",
        content_text: `# Park Crowd Flyer\n\n- Ice-cold lemonade two minutes from the soccer field\n- Free high-five with every cup unless Ruby forgets\n- Maybe mention the mint special only if Theo says it is not fake news\n`,
      },
    ],
    documents: [
      {
        id: "kid-boss-lemonade-plan",
        document: {
          id: "kid-boss-lemonade-plan",
          title: "Kid Boss Lemonade Plan",
        },
        refs: [
          "topic:kids-lemonade-main",
          "topic:kids-lemonade-sales",
          "topic:kids-lemonade-backoffice",
        ],
        content_type: "text",
        content: `# Kid Boss Lemonade Plan\n\nStatus: draft\n\nToday's mission:\n- Sell enough lemonade to feel triumphant but not so much that Theo runs out of ice and starts yelling\n\nLive plot threads:\n- possible soccer-rush stampede later\n- possible rival cookie table across the street\n- possible mint special if it stops sounding suspicious\n\nOpen questions:\n- Which sign joke should Ruby actually use?\n- When should the second pitcher be mixed?\n- Is the mint special brave or ridiculous?\n`,
        actor_id: "actor-boss-kid",
      },
      {
        id: "kid-sales-pitch-notebook",
        document: {
          id: "kid-sales-pitch-notebook",
          title: "Ruby Pitch Notebook",
        },
        refs: [
          "topic:kids-lemonade-sales",
          "topic:kids-lemonade-main",
        ],
        content_type: "text",
        content: `# Ruby Pitch Notebook\n\nCurrent best line:\n- Ice-cold lemonade and one free high-five\n\nThings to test:\n- giant price, smaller joke\n- one line for tired soccer parents\n- maybe the mint special if Theo says it is not nonsense\n`,
        actor_id: "actor-sales-kid",
      },
      {
        id: "kid-prep-notebook",
        document: {
          id: "kid-prep-notebook",
          title: "Theo Prep Notebook",
        },
        refs: [
          "topic:kids-lemonade-backoffice",
          "topic:kids-lemonade-main",
        ],
        content_type: "text",
        content: `# Theo Prep Notebook\n\nCurrent batch plan:\n- pitcher one is cold\n- pitcher two needs mixing before the rush\n\nThings that could go wrong:\n- cups run low\n- ice melts because people keep peeking\n- mint special creates kitchen chaos for no reason\n`,
        actor_id: "actor-backoffice-kid",
      },
    ],
    events: [
      {
        id: "evt-kids-main-001",
        actor_id: "actor-boss-kid",
        type: "actor_statement",
        thread_id: "thread-kids-lemonade-main",
        refs: [
          "thread:thread-kids-lemonade-main",
          "topic:kids-lemonade-main",
          "artifact:artifact-weather-note",
          "artifact:artifact-rival-stand-rumor",
          "document:kid-boss-lemonade-plan",
        ],
        summary: "Boss kid kickoff: run a fun stand, survive the rush, and please act like a team instead of three separate lemonade goblins",
        payload: {
          ask: "Ruby should sharpen the sign and sidewalk pitch, Theo should lock in batch timing and supply reality, and Milo should turn all of this into an actual plan.",
          mood: "playful but trying extremely hard to sound in charge",
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
          "thread:thread-kids-lemonade-sales",
          "topic:kids-lemonade-sales",
          "artifact:artifact-sales-scoreboard",
          "artifact:artifact-sign-slogans",
          "document:kid-sales-pitch-notebook",
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
          "thread:thread-kids-lemonade-backoffice",
          "topic:kids-lemonade-backoffice",
          "artifact:artifact-prep-checklist",
          "artifact:artifact-supply-stash",
          "document:kid-prep-notebook",
        ],
        summary: "Backoffice kid note: second batch timing matters more than dramatic speeches about success",
        payload: {
          observation: "One cold pitcher is ready, but the stand needs the second batch mixed before the first gets dangerously low.",
          warning: "Cups could get tight if the park crowd arrives thirsty all at once.",
        },
        provenance: {
          sources: ["artifact:artifact-supply-stash"],
        },
        ts: iso(-23 * 60 * 1000),
      },
      {
        id: "evt-kids-main-message-001",
        actor_id: "actor-boss-kid",
        type: "message_posted",
        thread_id: "thread-kids-lemonade-main",
        refs: ["thread:thread-kids-lemonade-main"],
        summary: "Message: team huddle before things get weird",
        payload: {
          text: "Okay team, before the soccer crowd appears like a stampede of thirsty wildebeests, tell me two things: what is the sign plan and when is pitcher two safe.",
        },
        provenance: {
          sources: ["inferred"],
        },
        ts: iso(-21 * 60 * 1000),
      },
      {
        id: "evt-kids-sales-message-001",
        actor_id: "actor-sales-kid",
        type: "message_posted",
        thread_id: "thread-kids-lemonade-main",
        refs: [
          "thread:thread-kids-lemonade-main",
          "event:evt-kids-main-message-001",
        ],
        summary: "Message reply: sign is fixable if the price stops hiding",
        payload: {
          text: "Replying to Milo: I can fix the sign if the price gets giant letters and the joke gets demoted to supporting comedian. Also I need Theo to tell me whether I am allowed to whisper about mint lemonade or not.",
        },
        provenance: {
          sources: ["artifact:artifact-sign-slogans"],
        },
        ts: iso(-19 * 60 * 1000),
      },
      {
        id: "evt-kids-backoffice-message-001",
        actor_id: "actor-backoffice-kid",
        type: "message_posted",
        thread_id: "thread-kids-lemonade-main",
        refs: [
          "thread:thread-kids-lemonade-main",
          "event:evt-kids-main-message-001",
        ],
        summary: "Message reply: pitcher two first, mint drama second",
        payload: {
          text: "Replying to Milo: pitcher two is the real boss here. If I mix it by 11:45 and everyone stops staring into the cooler, then we can test a tiny mint batch without summoning chaos.",
        },
        provenance: {
          sources: ["artifact:artifact-prep-checklist"],
        },
        ts: iso(-17 * 60 * 1000),
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
        ts: iso(-15 * 60 * 1000),
      },
    ],
  };
}
