import { getKidsLemonadeStandSeedData as getKidsLemonadeStandBaseSeedData } from "./kids-lemonade-stand-data.mjs";

export const KIDS_LEMONADE_STAND_CHAPTER_IDS = ["chapter-1", "chapter-2"];

const documentThreadIds = {
  "kid-boss-lemonade-plan": "thread-kids-lemonade-main",
  "kid-sales-pitch-notebook": "thread-kids-lemonade-sales",
  "kid-prep-notebook": "thread-kids-lemonade-backoffice",
};

const chapterDocumentContents = {
  "chapter-1": {
    "kid-boss-lemonade-plan": {
      created_by: "actor-boss-kid",
      content: `# Kid Boss Lemonade Plan

Status: final plan locked
Mood: organized chaos, the fun kind, with actual decisions made

Today's mission:
- Sell enough lemonade to feel triumphant but not so much that Theo runs out of ice and starts yelling
- Beat the rival cookie table (if it shows up) by being the stand people actually remember
- Keep the team talking to each other instead of guessing

Live plot threads - RESOLVED:
- Soccer crowd stampede: expected later, be ready with full pitchers
- Rival cookie table rumor: we counter with personality, a sharper sign, and the secret menu gimmick
- Mint special: APPROVED for a 4-cup test batch after pitcher two is safe

Sales game plan (Ruby's lane):
- Giant price in red chalk, visible from across the street
- Joke underneath as the warm-up act, not the headliner
- 11 cups sold so far, free high-fives are the best hook
- If mint test works: add 'secret menu available' whisper to the sign corner
- If mint is gross: we never mention it again, no embarrassment

Kitchen and supply plan (Theo's lane):
- Pitcher one already cold in backup spot
- Pitcher two mixed at 11:45 sharp, before the soccer stampede
- Supplies: 28 cups (18 main + 10 garage backup), lemons for 2.5 pitchers, sugar fine
- Ice in ONE small cooler under personal bodyguard protection - do NOT open for fun
- Mint test: 4 cups max, only after pitcher two is done and safe

Boss decisions (Milo's lane):
- Mint test is GO - four cups, conditional on pitcher two being safe first
- Sign direction is locked: giant price, small joke, secret menu whisper if mint works
- Sign must be done before noon, soccer crowd will not wait
- If cookie table appears, we pivot to personality and fun, not a price war

End-of-day goal:
- Empty pitchers, full scoreboard, zero meltdowns, and at least one good story to tell at school on Monday
- Bonus: the mint special turns out to be genius and becomes a neighborhood legend
`,
    },
    "kid-sales-pitch-notebook": {
      created_by: "actor-sales-kid",
      content: `# Ruby Pitch Notebook

## Best hook right now
- "Ice-cold lemonade and one free high-five" - sold 11 cups with this one
- Toddlers love it, parents smile, but they need to see the PRICE
- Milo approved the giant price direction. Making it the star of the sign.

## Final sign layout (confirmed)
- GIANT 50 cents in red chalk - the headliner
- Small joke underneath: "Ice-cold lemonade, no boring drinks allowed"
- If mint gets the green light: tiny whisper line - "psst... ask about the secret menu"
- Soccer crowd backup: "Soccer practice was hard, lemonade is easy"
- The joke is now the warm-up act. The price is the headliner.

## Mint lemonade status
- Theo says: four-cup test batch ONLY after pitcher two is safe (11:45)
- If mint looks like yard clippings, we pretend it never happened
- I will not whisper about mint to anyone until Theo hands me the cups
- If the test goes well, requesting a second tiny batch for the soccer rush
- Calling it "Cool Mint Mega Lemon" if it survives

## Customer reactions so far
- 11 cups sold, best closer is the free high-five
- Funniest moment: toddler yelled "THIS IS A TINY RESTAURANT"
- Parents respond to visible prices, kids respond to silliness
- The rival cookie table rumor is real - we need to be the memorable stand

## Cooler protection pact
- Theo is guarding the ice with his life
- Ruby is not touching that cooler unless it is for business
- This is the most serious either of us has ever been

## What I need from the team
- Milo: final sign direction picked (DONE - giant price approved)
- Theo: mint test batch after pitcher two (IN PROGRESS - waiting on 11:45)
`,
    },
    "kid-prep-notebook": {
      created_by: "actor-backoffice-kid",
      content: `# Theo Prep Log

## Pitcher Timing
- Pitcher one: DONE, chilling in the backup spot
- Pitcher two: mix by 11:45, no exceptions, no delays, no "wait let me check the sign first"
- If soccer rush hits at 13:00 we need pitcher two on the table by 12:00 at the latest
- Backup plan: if lemons run low after pitcher 2.5, send Milo to the corner store with two dollars and zero attitude

## Supply Watch
- Lemons: enough for 2.5 pitchers - that is NOT enough for a full afternoon, we need a restock plan
- Sugar: plenty, not worried
- Cups: 18 main + 10 garage backup = 28 total. That is maybe 20 servings if people do not double-cup. WATCH THIS.
- Ice: ONE small cooler. I am not joking about the ice. Every minute the lid is open is a minute closer to lukewarm disappointment.
- Mint leaves: enough for a 4-cup test batch, not enough for a whole menu item

## Kitchen Drama
- The mint special is approved for a TINY test batch ONLY after pitcher two is safe
- If mint bits look like yard clippings we cancel the whole thing and pretend it never happened
- The biggest risk is the cup count - 28 cups is fine for a slow hour, terrible for a soccer stampede

## Message to the Team
- @ruby: mint test batch is 4 cups max, and only after I say pitcher two is safe. Also please do not tell customers about it until we know it does not taste like a lawnmower.
- @milo: sign needs to be done before noon. I cannot squeeze lemons AND hold a marker. Also please stop suggesting we charge five dollars. This is a lemonade stand, not a restaurant.
`,
    },
  },
  "chapter-2": {
    "kid-boss-lemonade-plan": {
      created_by: "actor-boss-kid",
      content: `# Kid Boss Lemonade Plan

Status: CHAPTER 2 - lunch rush live, decisions locked, cup backup in progress
Mood: bossy but effective. The team is actually talking to each other and the board is not being ignored.

What happened in chapter 2:
- Soccer crowd arrived and started buying. The stand is officially busy.
- Ruby tested TWO sign versions and both worked. Version B (neat columns, readable from far away) won because a mom brought her whole squad after reading it from the sidewalk.
- Cups went from 28 to 24 usable (bike helmet crushed four garage cups). Cup emergency declared.
- Theo is the official mint test batch owner now. Clear ownership, no committee meetings.
- Milo deployed Triage Rules (one cup per customer, no samples, no double-cupping) and is biking to the corner store for paper cups.

Resolved decisions:
- SIGN: Version B wins - LEMONADE / ice cold / 50 cents in neat columns, one small joke underneath, 'Worth Every Penny' in tiny letters. Propped higher for sidewalk visibility.
- MINT: Theo owns it. Four cups max, only after pitcher two is safe, Theo tastes first. If thumbs up, Ruby whispers 'secret menu.' If thumbs down, we bury it.
- CUPS: 24 usable. Milo getting paper cups from corner store. Backup plan B: Mrs. Chen's party supplies. Triage rules in effect until backup arrives.
- Mint test cups only happen if paper cups arrive before pitcher two is ready. Otherwise cups stay in regular rotation.

Team assignments - CURRENT:
- Ruby: sign Version B going up NOW. No sample cups. Sell like every cup is the last cup (because it might be). Stand by for mint taste test report.
- Theo: cup monitoring, pitcher two completion, mint test batch execution. Ice cooler still sacred. Update board card with cup count after every five sales.
- Milo (boss): corner store cup run, board updates, master plan, making sure nobody panics. Will update doc again after cup run results.

Sales game plan (Ruby's lane):
- Sign Version B locked: LEMONADE / ice cold / 50 cents in columns
- One joke line, max eight words, underneath
- 'Worth Every Penny' in tiny letters
- Soccer kids care about giant prices and bright jokes, adults want readable menus from far away
- No sample cups during triage

Kitchen and supply plan (Theo's lane):
- Pitcher one cold, pitcher two in progress
- 24 usable cups (18 main + 6 garage) - DANGER ZONE
- Corner store paper cups incoming from Milo
- Ice under personal bodyguard protection
- Mint test: 4 cups max, Theo tastes first, only if cup backup arrives

Boss decisions:
- Sign: Version B, final answer, no more revisions
- Mint: Theo owns it, conditional on cup supply
- Cups: triage rules + corner store run + Mrs. Chen backup
- Cookie table: still rumored, we stay ready with personality and the secret menu gimmick

End-of-chapter-2 goal:
- Paper cups secured, sign up, mint verdict delivered, zero meltdowns
- Ruby sells with confidence because the sign finally works
- Theo does not have to serve lemonade in cupped hands
- Milo updates the board one more time and feels smug about being organized
`,
    },
    "kid-sales-pitch-notebook": {
      created_by: "actor-sales-kid",
      content: `# Ruby Pitch Notebook

## Best hook right now
- "Ice-cold lemonade and one free high-five" - sold 15 cups total now (4 more during chapter 2 rush)
- Soccer kids: giant visible prices are the #1 reason they walk over. They see 50 cents from the field and come running.
- Adults: they want to read the whole menu from the sidewalk. No guessing games.
- The free high-five is still the best closer but the SIGN is what gets them to stop.
- Funniest new moment: a dad asked if we take credit cards and then bought three cups with a handful of quarters.

## Sign testing results (chapter 2 - FINAL)
- Version A: GIANT 50 cents at top, small joke underneath - soccer kids LOVE it, 3 cups sold in 10 minutes
  - Soccer kid quote: "finally a sign I can read without squinting"
- Version B: neat columns ("LEMONADE - ice cold - 50 cents - no boring drinks") - adults love it
  - Mom brought her whole squad after reading it from across the sidewalk
- AWAITING final boss decision from Milo. Both versions work but for different crowds.
- Recommendation to Milo: go with Version A (giant price) since the soccer rush is the bigger wave, and the neat-column version is just the giant price spelled out differently anyway. Giant 50 cents IS the menu.

## Final sign layout (pending Milo's FINAL call)
- Top: GIANT 50 cents in red chalk - the headliner, the star, the whole reason people stop
- Middle: "Ice-cold lemonade" in smaller but clear letters
- Bottom: small joke as warm-up act ("no boring drinks allowed")
- Corner (IF mint survives the test): tiny whisper "psst... secret menu"
- MUST be readable from across the sidewalk - a parent confirmed the old sign was too hard to read

## Mint lemonade status
- Theo approved: four-cup test batch ONLY after pitcher two is safe
- Only enough mint leaves for ONE test batch - no second batch unless ice survives AND the kitchen is still standing
- If it tastes like toothpaste: bury it forever and never speak of it
- Not whispering about mint to ANYONE until Theo hands me the cups and gives the nod
- Name idea: "Cool Mint Mega Lemon" - standing by, ready to deploy

## Cup emergency (CHAPTER 2 - CRITICAL)
- Started with 28 cups (18 main + 10 garage backup)
- Sold 4 more in chapter 2 = ~24 cups remaining
- Soccer rush at full stampede could wipe us out in 30 minutes flat
- Asked Theo for backup plan: store run? paper cups? thermos? very clean bucket?
- Will NOT oversell if cups run low - not going to emotionally damage myself by turning away thirsty soccer kids
- If Theo says no backup is coming, I will slow-roll the pitch to stretch what we have

## Customer reactions - chapter 2 full update
- 15 cups sold total
- Soccer kids: bright giant prices + high-fives = instant customers
- Adults: clear menu readable from the sidewalk = trust and purchases
- Dad tried to pay with a credit card, bought three cups with quarters instead
- Rival cookie table still just a rumor but I am watching the street like a hawk
- Best hook for kids: giant 50 cents they can see from the soccer field
- Best hook for adults: knowing exactly what is for sale before they walk over

## Board card updated
- Card: "Rewrite the sign so the price is giant and the joke is small" - updated with chapter 2 testing results
- Status: in_progress, awaiting final boss decision on sign version
- Also flagged cup emergency on the card for visibility

## Cooler protection pact (still active, still sacred)
- Theo is guarding the ice with his life
- Ruby is not touching that cooler unless it is for business
- Do not open the cooler for fun. Theo will be dramatic about it.
- This pact has not been broken and it will not be broken today.

## What I need from the team (chapter 2 - still waiting)
- Milo: FINAL sign decision - Version A or Version B? Chalk is literally crumbling as I write this!
- Theo: cup backup plan before the soccer stampede + green light timing for the mint test batch
- Both: tell me if I should scale back the pitch when cups get critical - I will not overpromise
`,
    },
    "kid-prep-notebook": {
      created_by: "actor-backoffice-kid",
      content: `# Theo Prep Log - Chapter 2 Final Update

## Pitcher Timing
- Pitcher one: SERVED - almost gone, sold about 14 cups from it
- Pitcher two: DONE AND ON THE TABLE. Mixed, iced, ready to go. This is the pitcher that saves the rush.
- Pitcher three is NOT happening unless we get more lemons. We are at maybe half a pitcher's worth of lemon juice left.
- Bottom line: two full pitchers for the soccer rush. That is about 20-22 servings if nobody double-cups. Every cup is precious.

## Supply Watch - STILL A CUP EMERGENCY
- CUPS: ~20 remaining after pitcher one sales. The situation is better than before only because sales have been steady, not because we got more cups.
- Milo: I do not know if you saw my cup message but CORNER STORE. I will accept any cup-shaped object at this point. Paper, plastic, foam, I do not care. Even cones. Give me cones.
- Lemons: half a pitcher's worth left. After pitcher two we are scraping the rinds.
- Sugar: still plenty, sugar is the one thing we over-prepared
- Ice: cooler still sealed, still cold, still sacred. I have used exactly three scoops for pitcher two and that is the budget until the rush.
- Mint leaves: ready, waiting, washed, covered. They are patient.

## Mint Test Batch - OFFICIAL GO
- Pitcher two is safe, so the mint test is NOW ACTIVE
- Process: tear leaves, muddle with sugar, mix into 4 cups of lemonade, taste, decide
- If it is good: Ruby gets four labeled cups and a whispered secret menu briefing
- If it is bad: we pour it out, compost the evidence, and the word 'mint' is banned from this stand forever
- Ruby, come get your test cups when I give the signal. The signal is me holding up a cup and nodding.

## Rush Prep Checklist
- [x] Pitcher one on the table
- [x] Pitcher two mixed and iced
- [ ] Cup restock from corner store (BLOCKED ON MILO)
- [ ] Mint test batch taste test (IN PROGRESS)
- [ ] Ice rationing plan communicated to team
- [ ] Backup plan if we run out of everything: close the stand and say 'we sold out, come back tomorrow' which is actually the best marketing move possible

## Kitchen Drama
- The soccer crowd is visible from the kitchen window. I can hear them. They are loud and they are thirsty.
- If someone brings a cookie table across the street I will personally walk over and hand them a cup of lemonade as a peace offering because we cannot afford a rivalry right now.
- The bike helmet has been moved. The garage cups are in a box. The system is IMPROVING.

## Message to the Team
- @ruby: mint test is GO. Come get your cups after the taste test. Also please PLEASE track how many cups you have used because I cannot count from the kitchen.
- @milo: I need cup reinforcements before the soccer stampede. This is the one thing that could actually sink the stand. The lemonade is great, the sign will be great, but we cannot serve air.

## Chapter 2 Summary
- Pitcher two: DONE
- Cup crisis: DECLARED but not resolved
- Mint test: IN PROGRESS
- Ice cooler: STILL SACRED
- Kitchen morale: DETERMINED AND SLIGHTLY HYPER
`,
    },
  },
};

const chapterBoardStates = {
  "chapter-1": {
    boards: [
      {
        id: "991a7ede-c737-4040-b44a-ca225a20b496",
        title: "Saturday Lemonade Stand Mission Board",
        status: "active",
        created_by: "actor-boss-kid",
        updated_by: "actor-boss-kid",
        document_refs: ["document:kid-boss-lemonade-plan"],
        pinned_refs: [
          "artifact:artifact-sales-scoreboard",
          "artifact:artifact-prep-checklist",
          "artifact:artifact-supply-stash",
          "artifact:artifact-rival-stand-rumor",
        ],
        primary_topic_ref: "topic:thread-kids-lemonade-main",
        provenance: {
          sources: ["inferred"],
          notes: "Created during the Pi dogfood scenario to track the stand plan.",
        },
      },
    ],
    cards: [
      {
        id: "0cfdd886-64dd-4d08-9ded-551605254d71",
        board_id: "991a7ede-c737-4040-b44a-ca225a20b496",
        column_key: "in_progress",
        summary: "Boss kid coordination: afternoon rush game plan",
        title: "Boss kid coordination: afternoon rush game plan",
        related_refs: [
          "artifact:artifact-prep-checklist",
          "artifact:artifact-rival-stand-rumor",
          "artifact:artifact-sales-scoreboard",
          "artifact:artifact-supply-stash",
          "thread:thread-kids-lemonade-main",
          "topic:thread-kids-lemonade-main",
        ],
        assignee_refs: ["actor:actor-boss-kid"],
        risk: "medium",
        created_by: "actor-boss-kid",
        updated_by: "actor-boss-kid",
      },
      {
        id: "3c552c4c-ddf4-4e76-84ca-aebd7d8d8cc4",
        board_id: "991a7ede-c737-4040-b44a-ca225a20b496",
        column_key: "in_progress",
        summary: "Kitchen Prep: pitcher two by 11:45, ice lockdown, mint test batch",
        title: "Kitchen Prep: pitcher two by 11:45, ice lockdown, mint test batch",
        related_refs: [
          "artifact:artifact-prep-checklist",
          "artifact:artifact-supply-stash",
          "thread:thread-kids-lemonade-backoffice",
          "topic:thread-kids-lemonade-backoffice",
        ],
        assignee_refs: ["actor:actor-backoffice-kid"],
        risk: "medium",
        created_by: "actor-backoffice-kid",
        updated_by: "actor-backoffice-kid",
      },
      {
        id: "3d051f3d-3dcc-4e8e-9ccd-6b5f1c524195",
        board_id: "991a7ede-c737-4040-b44a-ca225a20b496",
        column_key: "in_progress",
        summary: "Rewrite the sign so the price is giant and the joke is small",
        title: "Rewrite the sign so the price is giant and the joke is small",
        related_refs: [
          "artifact:artifact-sales-scoreboard",
          "artifact:artifact-sign-slogans",
          "thread:thread-kids-lemonade-sales",
          "topic:thread-kids-lemonade-sales",
        ],
        assignee_refs: ["actor:actor-sales-kid"],
        risk: "medium",
        created_by: "actor-sales-kid",
        updated_by: "actor-sales-kid",
      },
    ],
  },
};

const chapterEvents = {
  "chapter-1": [
    {
      id: "evt-kids-ch1-main-message-001",
      actor_id: "actor-boss-kid",
      type: "message_posted",
      thread_id: "thread-kids-lemonade-main",
      refs: [
        "thread:thread-kids-lemonade-main",
        "board:991a7ede-c737-4040-b44a-ca225a20b496",
      ],
      summary: "Mission board is UP, team huddle time!",
      payload: {
        text: "TEAM. The mission board is live. I made it because sticky notes keep blowing away and nobody can read my handwriting anyway. Here is the plan:\n\n1. Ruby - your sign, your pitch, your vibes. Get the price in BIG letters. The joke is the warm-up act, not the headliner. Also tell me if the mint thing sounds fun or like a science experiment gone wrong.\n\n2. Theo - pitcher two timing is YOUR call. If 11:45 works, go for it. Protect that ice like it is treasure. And yeah, if you can sneak-test a tiny mint batch without destroying the kitchen, I am officially curious.\n\n3. The soccer crowd is coming and I heard a cookie table might show up across the street. We need to be the stand people remember, not the one next to the cookies.\n\nNow post your updates on the board cards so I can actually track who is doing what. No more yelling across the yard!",
      },
      provenance: {
        sources: ["inferred"],
      },
      ts: "2026-04-10T16:53:28.248608Z",
    },
    {
      id: "evt-kids-ch1-main-message-002",
      actor_id: "actor-sales-kid",
      type: "message_posted",
      thread_id: "thread-kids-lemonade-main",
      refs: ["thread:thread-kids-lemonade-main"],
      summary: "Ruby here: sign rewrite plan and mint lemonade polling",
      payload: {
        text: "Hey Milo and Theo! Ruby reporting from the front line. Eleven cups sold and the best hook so far is the free high-five. But here is the problem: the joke on the sign is SO big that people are laughing and walking away without buying. I am rewriting the sign today. New plan: GIANT price in red chalk, small joke underneath. Also I asked three kids if they would try a mint lemonade and two of them said 'what is mint' and one said 'yes obviously.' So Theo, I need that tiny test batch please, and Milo, I need you to pick one final sign direction so I stop crossing things out. The soccer crowd is coming and I want the sign to say something they can read from across the street.",
      },
      provenance: {
        sources: [
          "artifact:artifact-sales-scoreboard",
          "artifact:artifact-sign-slogans",
        ],
      },
      ts: "2026-04-10T16:53:32.653938Z",
    },
    {
      id: "evt-kids-ch1-main-reply-001",
      actor_id: "actor-boss-kid",
      type: "message_posted",
      thread_id: "thread-kids-lemonade-main",
      refs: [
        "thread:thread-kids-lemonade-main",
        "event:evt-kids-sales-message-001",
      ],
      summary: "Ruby: YES to giant price, mint is pending Theo's verdict",
      payload: {
        text: "Ruby I love the giant price energy. Make that $1 the biggest thing on the sign - people need to see it from the sidewalk. And yeah, the mint whispering is on hold until Theo tells us whether it is doable without the kitchen becoming a sticky disaster zone. If he says yes, you can add a little 'psst... ask about the secret menu' whisper line to the sign. If he says no, we pretend the mint never happened and nobody gets embarrassed.",
      },
      provenance: {
        sources: ["inferred"],
      },
      ts: "2026-04-10T16:53:48.799247Z",
    },
    {
      id: "evt-kids-ch1-main-message-003",
      actor_id: "actor-backoffice-kid",
      type: "message_posted",
      thread_id: "thread-kids-lemonade-main",
      refs: ["thread:thread-kids-lemonade-main"],
      summary: "Message: supply report - the cooler situation is under control (for now)",
      payload: {
        text: "Okay here is the real update from the kitchen. Pitcher one is already cold and chilling in the backup spot. Pitcher two gets mixed at 11:45 sharp - that gives us a full pitcher ready before the soccer crowd stampede.\n\nSupply count: 28 cups total (18 main stack + 10 garage backup), lemons for about 2.5 pitchers, sugar is fine, and the ice is in ONE small cooler that I am personally guarding with my life. If anyone opens that cooler for fun instead of business I will notice and I will be dramatic about it.\n\nAbout the mint thing: Ruby, I hear you want to whisper about it to customers. Here is my ruling - we can do a TINY test batch, like four cups max, but ONLY after pitcher two is done and safe. If the mint bits look like yard clippings we pretend it never happened. Deal?\n\nMilo - please tell the sign situation is sorted before noon because I do not want to be squeezing lemons while also holding a marker.",
      },
      provenance: {
        sources: [
          "artifact:artifact-prep-checklist",
          "artifact:artifact-supply-stash",
        ],
      },
      ts: "2026-04-10T16:54:05.745221Z",
    },
    {
      id: "evt-kids-ch1-main-reply-002",
      actor_id: "actor-sales-kid",
      type: "message_posted",
      thread_id: "thread-kids-lemonade-main",
      refs: [
        "thread:thread-kids-lemonade-main",
        "event:evt-kids-ch1-main-message-003",
      ],
      summary: "Ruby to Theo: DEAL on the mint, four cups is perfect for a taste test",
      payload: {
        text: "Theo, DEAL. Four cups is exactly enough to find out if this is genius or gross. I will not mention mint to a single customer until you hand me those cups and give the nod. Also I respect the cooler bodyguard energy - nobody is touching that ice on my watch either. One question though: if the mint test goes well, can you make another tiny batch for the soccer crowd? Because those kids are gonna want the secret menu if it slaps.",
      },
      provenance: {
        sources: ["artifact:artifact-secret-menu-sketch"],
      },
      ts: "2026-04-10T16:55:01.895798Z",
    },
    {
      id: "evt-kids-ch1-main-reply-003",
      actor_id: "actor-boss-kid",
      type: "message_posted",
      thread_id: "thread-kids-lemonade-main",
      refs: [
        "thread:thread-kids-lemonade-main",
        "event:evt-kids-ch1-main-message-003",
      ],
      summary: "Theo: you are doing amazing, sign is sorted, mint is a GO",
      payload: {
        text: "Theo you absolute legend. Pitcher one cold, pitcher two at 11:45, ice under personal bodyguard protection - that is exactly the kind of kitchen leadership this stand needs. 28 cups with a garage backup is solid.\n\nOkay team decisions from the boss:\n1. Mint test batch is APPROVED - four cups, only after pitcher two is safe. If it tastes like yard clippings we bury the evidence and never speak of it.\n2. Ruby, the sign direction is: giant $1 (or 50 cents if that is what you are going with) in red chalk, joke underneath, and IF the mint test works, add a tiny 'secret menu available' whisper in the corner.\n3. Sign gets done before noon. No excuses. The soccer crowd will not wait for us to find a marker.\n\nI am updating the plan doc now. This stand is going to be legendary.",
      },
      provenance: {
        sources: ["inferred"],
      },
      ts: "2026-04-10T16:55:34.760974Z",
    },
    {
      id: "evt-kids-ch1-main-reply-004",
      actor_id: "actor-backoffice-kid",
      type: "message_posted",
      thread_id: "thread-kids-lemonade-main",
      refs: [
        "thread:thread-kids-lemonade-main",
        "event:evt-kids-ch1-main-reply-002",
      ],
      summary: "Message reply: second mint batch depends on whether we survive the first one",
      payload: {
        text: "Ruby, listen. I appreciate the ambition but here is the math: we have enough mint leaves for ONE tiny test batch and that is it. If the test goes well AND we still have ice left after the soccer crowd, I will try to squeeze out maybe four more cups. But I am not promising a second batch until I see how the first one goes and whether the kitchen is still standing.\n\nAlso - and this is important - if the mint test tastes like toothpaste mixed with lemon juice, we bury this idea forever and never speak of it again. Agreed? Good.\n\nNow go sell some regular lemonade and leave the cooler alone.",
      },
      provenance: {
        sources: [
          "artifact:artifact-secret-menu-sketch",
          "artifact:artifact-supply-stash",
        ],
      },
      ts: "2026-04-10T16:55:40.302717Z",
    },
    {
      id: "evt-kids-ch1-main-actor-001",
      actor_id: "actor-sales-kid",
      type: "actor_statement",
      thread_id: "thread-kids-lemonade-main",
      refs: [
        "thread:thread-kids-lemonade-main",
        "thread:thread-kids-lemonade-sales",
        "document:kid-sales-pitch-notebook",
        "artifact:artifact-sales-scoreboard",
        "artifact:artifact-sign-slogans",
        "artifact:artifact-weather-note",
        "artifact:artifact-neighborhood-flyer",
        "artifact:artifact-secret-menu-sketch",
      ],
      summary: "Ruby's front-stand report: sign rewrite done, mint test pending, soccer crowd prep ready",
      payload: {
        evidence: [
          "11 cups sold so far, best hook is 'Ice-cold lemonade and one free high-five'",
          "Customers laugh at the big joke but walk away without buying - the price was invisible",
          "Milo approved the giant-price, small-joke direction",
          "Theo committed to a four-cup mint test batch after pitcher two is mixed at 11:45",
          "Mint polling: 2 of 3 kids said 'what is mint', 1 said 'yes obviously' - demand is real but niche",
          "Best customer reaction: toddler yelled 'THIS IS A TINY RESTAURANT'",
          "Rival cookie table rumor is floating - we need to be the stand people remember",
        ],
        follow_ups: [
          "Ruby: finish chalk rewrite of the sign before noon",
          "Ruby: test the new sign on the next 5 customers and count laugh-to-buy ratio",
          "Theo: deliver the four-cup mint test batch after pitcher two is safe",
          "Ruby: if mint passes, add the secret menu whisper line to the sign",
          "All three: prep for soccer crowd rush around 13:00 with the backup sign line ready",
        ],
        recommendation: "The sign is getting a full rewrite: giant 50 cents in red chalk as the headliner, with the joke demoted to supporting comedian underneath. The free high-five stays as the closer because it actually moves cups. If Theo's mint test batch survives the 11:45 deadline, add a whisper line ('psst... ask about the secret menu') to the sign - but only after Theo gives the green light. For the soccer rush, have a second sign-ready line: 'Soccer practice was hard, lemonade is easy.'",
      },
      provenance: {
        sources: [
          "artifact:artifact-sales-scoreboard",
          "artifact:artifact-sign-slogans",
          "artifact:artifact-secret-menu-sketch",
          "artifact:artifact-weather-note",
        ],
      },
      ts: "2026-04-10T16:56:19.560234Z",
    },
    {
      id: "evt-kids-ch1-main-actor-002",
      actor_id: "actor-backoffice-kid",
      type: "actor_statement",
      thread_id: "thread-kids-lemonade-main",
      refs: [
        "thread:thread-kids-lemonade-main",
        "thread:thread-kids-lemonade-backoffice",
        "document:kid-prep-notebook",
        "artifact:artifact-prep-checklist",
        "artifact:artifact-supply-stash",
        "artifact:artifact-weather-note",
        "artifact:artifact-rival-stand-rumor",
        "artifact:artifact-secret-menu-sketch",
        "board:991a7ede-c737-4040-b44a-ca225a20b496",
        "card:3c552c4c-ddf4-4e76-84ca-aebd7d8d8cc4",
      ],
      summary: "Backoffice kid final: prep plan locked, supply cliffs named, mint verdict delivered",
      payload: {
        evidence: [
          "Pitcher one is confirmed cold in the backup chill spot",
          "Pitcher two timing locked at 11:45 based on soccer rush ETA of ~13:00",
          "Supply stash: 28 cups (18+10), lemons for 2.5 pitchers, sugar fine, one small cooler of ice",
          "Mint leaves only enough for a 4-cup test batch per artifact-secret-menu-sketch",
          "Ruby sold 11 cups already per her front-line report, so cup burn rate is real",
          "Weather note confirms warm sunny day - cold drinks will move fast, ice will vanish faster if mistreated",
          "Created prep task card on mission board tied to backoffice role thread",
          "Updated kid-prep-notebook to revision 2 with full batch timing, supply watch, and kitchen drama notes",
        ],
        follow_ups: [
          "Ruby: test the mint batch only after Theo gives the green light, and keep a straight face even if it tastes weird",
          "Milo: figure out a cup restock plan - 28 cups will not last through a soccer rush",
          "Everyone: stay away from the cooler unless you are actively filling a pitcher. This is not negotiable.",
          "After the rush: if supplies held, update the prep notebook with what we actually used vs planned",
        ],
        recommendation: "The kitchen is as ready as it is going to get. Pitcher one is cold, pitcher two mixes at 11:45, and the mint test batch is approved for four cups only after pitcher two is safe. The biggest supply risk is cups - 28 total will not survive a real soccer stampede, so Milo or Ruby need a backup plan for getting more. Ice is critical: one cooler, guarded, no peeking. The mint special is a fun experiment but cannot become a menu item without more mint leaves and more ice. Keep the focus on regular lemonade quality and speed.",
      },
      provenance: {
        sources: [
          "artifact:artifact-prep-checklist",
          "artifact:artifact-supply-stash",
          "artifact:artifact-weather-note",
          "document:kid-prep-notebook",
        ],
      },
      ts: "2026-04-10T16:56:20.712099Z",
    },
    {
      id: "evt-kids-ch1-main-actor-003",
      actor_id: "actor-boss-kid",
      type: "actor_statement",
      thread_id: "thread-kids-lemonade-main",
      refs: [
        "thread:thread-kids-lemonade-main",
        "thread:thread-kids-lemonade-sales",
        "thread:thread-kids-lemonade-backoffice",
        "document:kid-boss-lemonade-plan",
        "artifact:artifact-sales-scoreboard",
        "artifact:artifact-prep-checklist",
        "artifact:artifact-supply-stash",
        "artifact:artifact-rival-stand-rumor",
        "artifact:artifact-secret-menu-sketch",
        "board:991a7ede-c737-4040-b44a-ca225a20b496",
        "card:0cfdd886-64dd-4d08-9ded-551605254d71",
      ],
      summary: "Boss kid final statement: the plan is locked, the team talked to each other, and we are ready for the soccer crowd",
      payload: {
        evidence: [
          "Created the Saturday Lemonade Stand Mission Board (991a7ede) with 3 role-scoped cards, all in_progress",
          "Ruby (sales kid) posted 2 messages and created 1 card for sign rewrite - confirmed giant price direction and mint polling results (2/3 kids curious about mint)",
          "Theo (backoffice kid) posted 2 messages and created 1 card for kitchen prep - confirmed pitcher two at 11:45, 28 cups available, ice under protection, mint test possible",
          "Replied directly to both Ruby and Theo in-thread to close the loop on decisions",
          "Updated kid-boss-lemonade-plan through 3 revisions: draft -> in-progress -> final plan locked",
          "Used propose-update + apply workflow for document updates, reviewed diff each time",
          "All plot threads resolved: soccer crowd prep, cookie counter-strategy, mint special verdict",
        ],
        follow_ups: [
          "Ruby: rewrite the sign before noon with giant price in red chalk - your card is on the board",
          "Theo: mix pitcher two at 11:45, then prep the 4-cup mint test - your card has the full checklist",
          "Milo: keep watching the board for card updates from Ruby and Theo, nudge if sign is not done by noon",
          "If the mint test works: add 'secret menu available' to the sign and consider a second small batch for the soccer crowd",
          "If the cookie table appears: pivot to fun and personality, do not get into a price war with cookies",
        ],
        recommendation: "The lemonade stand plan is locked and the team is coordinated. Three decisions were made in-thread: (1) mint test batch approved for 4 cups after pitcher two is safe, (2) sign direction locked to giant price with small joke, (3) cookie table counter-strategy is personality over price. The mission board has three role-scoped cards all in-progress. The plan document went through three revisions from draft to final. The team communicated through message_posted events and replies rather than only formal updates.",
      },
      provenance: {
        sources: ["inferred"],
      },
      ts: "2026-04-10T16:57:16.065922Z",
    },
  ],
  "chapter-2": [
    {
      id: "evt-kids-ch2-main-message-001",
      actor_id: "actor-backoffice-kid",
      type: "message_posted",
      thread_id: "thread-kids-lemonade-main",
      refs: ["thread:thread-kids-lemonade-main"],
      summary: "CUP EMERGENCY and the ice cooler is sacred - Theo chapter 2 update",
      payload: {
        text: "TEAM. I just did a recount and the cup situation is worse than I thought. We have maybe 22 usable cups left because four of the garage backup ones got crushed under a bike helmet. So that is 18 main plus 6 salvageable garage cups = 24 total, and if the soccer crowd brings even FIFTEEN kids we are done. Done done. Drinking out of hands done.\n\nHere is what I need RIGHT NOW:\n- @milo: can someone bike to the corner store for paper cups? Even a pack of 20 would save us. This is not optional.\n- @ruby: every cup matters now. No double-cupping, no 'oops I dropped one' situations. Serve like every cup is the last cup.\n\nIce update: the cooler is still frozen and I have not opened it since pitcher two went in. But ice melts faster when you are stressed, so nobody breathe near it.\n\nMint test batch: still happening after I get the all-clear on pitcher two. Four cups ONLY. But honestly if we run out of cups I am using all four mint cups for regular lemonade because survival beats fancy.\n\nGet me cups. This is not a drill.",
      },
      provenance: {
        sources: ["inferred"],
      },
      ts: "2026-04-10T17:22:31.431481Z",
    },
    {
      id: "evt-kids-ch2-main-message-002",
      actor_id: "actor-sales-kid",
      type: "message_posted",
      thread_id: "thread-kids-lemonade-main",
      refs: ["thread:thread-kids-lemonade-main"],
      summary: "Message: sign v2 tested - soccer kids want big prices, adults want readable menus",
      payload: {
        text: "OKAY TEAM. Ruby here with an urgent front-line update. The sign rewrite is DONE and I just tested two versions on actual humans walking by:\n\nVersion A: GIANT 50 cents at the top, small joke underneath. Three soccer kids walked over from the sidewalk and bought cups immediately. One of them said 'finally a sign I can read without squinting.' So yeah, the giant price thing is WORKING.\n\nVersion B: I tried adding 'LEMONADE - ice cold - 50 cents - no boring drinks allowed' in neat columns. A mom stopped, read it from across the sidewalk, and brought her whole kid squad over. She said 'oh good, I can actually tell what you are selling.' Adults want MENU CLARITY, not punchlines.\n\nBut here is the problem - Theo, I heard you say 28 cups total? I just sold four more in like ten minutes and the soccer crowd is barely here yet. Are we gonna run out of cups before the rush even peaks?? Because I cannot sell lemonade in my hands, Theo. I do not have that kind of confidence.\n\nMilo - I need a FINAL BOSS DECISION on the sign. Do I go with Version A (giant price, small joke) or Version B (neat columns, readable from far away)? Both work but I can only chalk ONE sign and I am running out of chalk.",
      },
      provenance: {
        sources: [
          "artifact:artifact-sign-slogans",
          "artifact:artifact-sales-scoreboard",
        ],
      },
      ts: "2026-04-10T17:22:48.573621Z",
    },
    {
      id: "evt-kids-ch2-main-message-003",
      actor_id: "actor-boss-kid",
      type: "message_posted",
      thread_id: "thread-kids-lemonade-main",
      refs: [
        "thread:thread-kids-lemonade-main",
        "board:991a7ede-c737-4040-b44a-ca225a20b496",
      ],
      summary: "Message: LUNCH RUSH IS HERE - new assignments, no slacking",
      payload: {
        text: "OKAY TEAM. The soccer crowd is not 'coming later' anymore, they are SPILLING toward the corner RIGHT NOW. I can see them from here. This is not a drill.\n\nNew problems just showed up:\n1. A parent walked by and said the sign is STILL too hard to read from the sidewalk. Ruby, I know you rewrote it, but apparently people need to see the price from like fifteen feet away, not just up close. We need a second pass - bigger letters, maybe prop the sign up higher or angle it toward the street.\n\n2. CUPS. Theo just told me we have 28 total and the garage backup is not infinite. If the soccer crowd shows up in a pack, we could burn through those in like twenty minutes. I need a cup backup plan that does not involve washing cups in the hose water.\n\n3. The mint special is still floating around without a clear owner. If we are doing the test batch, someone needs to OWN it - make it, serve it, and tell me whether it is genius or gross. No vague 'maybe later' energy.\n\nBoard assignments - I am updating my card right now and I expect the same from both of you:\n- Ruby: sign fix round two, figure out what 'readable from the sidewalk' actually means, and tell me what you need to make it happen\n- Theo: cup emergency plan AND mint test batch ownership - you decide if we can do both or if cups come first\n- Milo (me): keeping the board alive, updating the master plan, and making sure nobody panics\n\nThe cookie table rumor is still out there. If it shows up, we need to be ready. Let us go!",
      },
      provenance: {
        sources: ["inferred"],
      },
      ts: "2026-04-10T17:22:59.707193Z",
    },
    {
      id: "evt-kids-ch2-main-reply-001",
      actor_id: "actor-backoffice-kid",
      type: "message_posted",
      thread_id: "thread-kids-lemonade-main",
      refs: [
        "thread:thread-kids-lemonade-main",
        "event:evt-kids-ch2-main-message-002",
      ],
      summary: "Ruby - mint is DOABLE but cup math just got scary",
      payload: {
        text: "Ruby! Good news and terrifying news. Good news first: the mint test batch is still a go. I have enough mint leaves for four cups and I will prep them the second pitcher two is stable. The process is simple - tear the leaves, muddle them with a little sugar, add to the pitcher, stir, taste. If it tastes like toothpaste soup we dump it and never mention mint again.\n\nTerrifying news: we just lost four garage cups to a bike helmet situation. I am at 24 total cups now and the soccer crowd is coming. So here is the deal - if you are promising kids a secret menu, you better hope Milo can get us backup cups from the corner store. Because a secret menu with no cups is just a secret.\n\nAlso I love the giant price idea. Maybe add 'Worth Every Penny' in tiny letters under the joke. You know, for the adults who think lemonade stands are cute.",
      },
      provenance: {
        sources: ["inferred"],
      },
      ts: "2026-04-10T17:23:41.564219Z",
    },
    {
      id: "evt-kids-ch2-sales-message-001",
      actor_id: "actor-sales-kid",
      type: "message_posted",
      thread_id: "thread-kids-lemonade-sales",
      refs: ["thread:thread-kids-lemonade-sales"],
      summary: "Message: chapter 2 rush update - sign v2 tested, cup alarm, soccer crowd inbound",
      payload: {
        text: "Ruby here, checking in on the sales thread because things are getting REAL at the front stand.\n\nSign rewrite status: I tested TWO versions just now. Version A is the giant 50 cents with a small joke underneath - soccer kids love it, they can read it from the field. Version B is neat columns with everything spelled out - adults love it because they can tell what we sell from across the sidewalk. I need Milo to pick one FINAL version so I can commit.\n\nCup situation: Theo said 28 cups but I just burned through four more and the rush has barely started. If the soccer crowd shows up in force, we could be out of cups in like thirty minutes. I am NOT going to promise lemonade to people if we cannot serve it.\n\nSoccer crowd intel: these kids care about two things - giant prices they can see from far away, and bright/funny stuff that makes them stop. Adults want the menu to be OBVIOUS. No guessing games. The rival cookie table is still a rumor but I am not losing to cookies.\n\nMint update: still waiting on Theo's test batch. Not saying a word about mint to anyone until I have cups in my hand and a nod from the kitchen.\n\nNext moves: get the final sign locked in, wait on Theo for cups intel, and prepare for the rush. This is going to be a big afternoon.",
      },
      provenance: {
        sources: [
          "artifact:artifact-sales-scoreboard",
          "artifact:artifact-sign-slogans",
          "artifact:artifact-weather-note",
        ],
      },
      ts: "2026-04-10T17:24:05.356492Z",
    },
    {
      id: "evt-kids-ch2-main-reply-002",
      actor_id: "actor-sales-kid",
      type: "message_posted",
      thread_id: "thread-kids-lemonade-main",
      refs: [
        "thread:thread-kids-lemonade-main",
        "event:evt-kids-ch1-main-reply-004",
      ],
      summary: "Message reply: Ruby to Theo - cup emergency incoming, need a backup plan NOW",
      payload: {
        text: "Theo, I hear you on the mint math and I agree - one test batch, and if it tastes like toothpaste smoothie we bury it forever. But I need to talk about something more urgent: THE CUPS.\n\nI just sold four more and I am counting. If the soccer crowd shows up at full stampede, 28 cups might not last the afternoon. That garage backup of 10 cups is the only thing standing between us and serving lemonade in our cupped hands. Do we have a faster backup plan? Can someone run to the store? Do we have paper cups somewhere? An old thermos? A very clean bucket??\n\nI am not going to oversell but I also cannot tell a line of thirsty soccer kids 'sorry we ran out of cups' because I will never emotionally recover from that. Tell me the cup backup plan and I will stop panicking. Maybe.",
      },
      provenance: {
        sources: ["artifact:artifact-sales-scoreboard"],
      },
      ts: "2026-04-10T17:24:32.188139Z",
    },
    {
      id: "evt-kids-ch2-backoffice-message-001",
      actor_id: "actor-boss-kid",
      type: "message_posted",
      thread_id: "thread-kids-lemonade-backoffice",
      refs: [
        "thread:thread-kids-lemonade-backoffice",
        "artifact:artifact-supply-stash",
      ],
      summary: "Message: Milo to Theo - cup emergency and mint ownership, need your call",
      payload: {
        text: "Theo! It is Milo, infiltrating your kitchen thread because this is important.\n\nThe cup situation is keeping me up at night (okay it is daytime but still). You said 28 total - if even ten soccer kids show up at once, we could be out in like fifteen minutes. Can you check if there are more cups in the garage that we missed? Or is there a neighbor who might lend us a stack?\n\nAlso: I am making you the official mint test batch owner. Not because I am being bossy (okay a little) but because you are the one with the mint leaves and the kitchen. If pitcher two is safe and you have bandwidth, you make the four cups, you taste one, and you give Ruby the thumbs up or thumbs down. Clear ownership, no vague vibes.\n\nUpdate your card on the board when you have a cup plan. I need to know whether we are fine or whether I need to start asking neighbors for paper cups.",
      },
      provenance: {
        sources: ["artifact:artifact-supply-stash"],
      },
      ts: "2026-04-10T17:24:55.145620Z",
    },
    {
      id: "evt-kids-ch2-backoffice-message-002",
      actor_id: "actor-backoffice-kid",
      type: "message_posted",
      thread_id: "thread-kids-lemonade-backoffice",
      refs: ["thread:thread-kids-lemonade-backoffice"],
      summary: "Kitchen status log: pitcher two is GO, cup crisis is real",
      payload: {
        text: "Kitchen log update. Pitcher one: stable, cold, not going anywhere. Pitcher two: mixing NOW. Lemmons are cut, sugar is measured, water is room-temp which is fine because the ice will fix it.\n\nCup situation: 24 usable cups. I said TWENTY-FOUR. That is maybe 20 normal servings and 4 if nobody spills. The garage backup lost four cups to a bike helmet that someone left on top of the stack. I am not naming names but the helmet had dinosaur stickers on it.\n\nIce cooler status: SEALED. UNTOUCHED. SACRED. I put a towel over it so nobody even gets tempted. If anyone asks about ice I will point to the sign I am going to tape to the lid that says 'ASK THEO FIRST OR ELSE.'\n\nMint prep: leaves are washed and in a small bowl covered with a plate. They are ready for the test batch but I am NOT touching them until pitcher two is poured and on the table. Priority one is regular lemonade for the rush.\n\nTo-do after pitcher two:\n1. Pour pitcher two into serving jug\n2. Prep 4-cup mint test batch\n3. Taste test (myself, because I am not serving anything I would not drink)\n4. If it passes, hand to Ruby with instructions to whisper about it only to kids who seem adventurous\n\nKitchen is locked in. Do not distract me unless you have cup reinforcements.",
      },
      provenance: {
        sources: ["inferred"],
      },
      ts: "2026-04-10T17:24:57.066437Z",
    },
    {
      id: "evt-kids-ch2-main-reply-003",
      actor_id: "actor-boss-kid",
      type: "message_posted",
      thread_id: "thread-kids-lemonade-main",
      refs: [
        "thread:thread-kids-lemonade-main",
        "event:evt-kids-ch1-main-reply-004",
      ],
      summary: "Message reply: Theo you are officially the mint owner, cups are the real emergency now",
      payload: {
        text: "Theo - AGREED on all counts. If the mint tastes like toothpaste lemon juice, we bury it and never speak of it again. That is the deal.\n\nBut here is my boss ruling for chapter 2: you are now officially the mint test batch owner. Not Ruby, not me, YOU. You have the mint leaves, you have the kitchen, you make the call. Four cups, only after pitcher two is safe, and you taste it first. If you give Ruby the thumbs up, she can whisper about it to customers. If you give the thumbs down, we move on. Clear ownership, no committee meetings.\n\nBUT - and I am saying this with love - the cups are actually the bigger emergency right now. 28 cups and a soccer stampede is not a comfortable math problem. Can you do a quick garage check and see if there are more hiding somewhere? If not, tell me and I will figure out a backup plan. Maybe my mom has paper cups in the pantry.",
      },
      provenance: {
        sources: ["inferred"],
      },
      ts: "2026-04-10T17:25:25.085293Z",
    },
    {
      id: "evt-kids-ch2-main-reply-004",
      actor_id: "actor-boss-kid",
      type: "message_posted",
      thread_id: "thread-kids-lemonade-main",
      refs: [
        "thread:thread-kids-lemonade-main",
        "event:evt-kids-ch2-main-message-002",
      ],
      summary: "Message reply: FINAL SIGN DECISION - Version B wins, here is why",
      payload: {
        text: "RUBY. Okay I have made my FINAL BOSS DECISION and nobody gets to argue.\n\nVersion B wins. Here is why: you said a MOM read it from across the sidewalk and brought her whole kid squad over. That is exactly the kind of customer we need - the ones who bring three more customers with them. The soccer kids will buy from any sign that has a number on it (they are here for the sugar, not the typography), but the adults are the ones who actually read signs from a distance.\n\nSo here is the final sign layout:\n- Top line: LEMONADE - ice cold - 50 cents\n- Keep it in neat columns like Version B\n- Underneath, ONE small joke line (your best one) - no more than eight words\n- If you have room, add 'Worth Every Penny' in tiny letters like Theo suggested because that is actually hilarious\n\nChalk it once, chalk it BIG, and prop the sign up higher so it catches eyes from the street. If you have to choose between pretty and readable, readable wins every time.\n\nNow go. The soccer kids are coming and I need that sign up before the first wave hits. Also - SAVE YOUR CHALK. We might need a second revision if the cookie table shows up.",
      },
      provenance: {
        sources: ["inferred"],
      },
      ts: "2026-04-10T17:26:38.507225Z",
    },
    {
      id: "evt-kids-ch2-main-reply-005",
      actor_id: "actor-boss-kid",
      type: "message_posted",
      thread_id: "thread-kids-lemonade-main",
      refs: [
        "thread:thread-kids-lemonade-main",
        "event:evt-kids-ch2-main-message-001",
      ],
      summary: "Message reply: CUP BACKUP PLAN - Milo is on it, team triage rules",
      payload: {
        text: "Theo. Okay 24 cups is not great but we are not defeated yet. Here is the cup backup plan:\n\n1. I am biking to the corner store RIGHT NOW for a pack of paper cups. My mom gave me two dollars for emergencies and this counts.\n\n2. Until I get back, Triage Rules are in effect:\n   - ONE cup per customer. No double-cupping, no 'can I get a bigger one.'\n   - If someone wants seconds, they bring their own cup back. We rinse and reuse if we have to.\n   - Ruby, no sample cups. If they want to taste it, they buy it.\n   - Mint test batch cups ONLY happen if I get back with the paper cups before pitcher two is ready. Otherwise those cups stay in the regular rotation.\n\n3. If the corner store is out, backup plan B: I knock on Mrs. Chen's door because she always has party supplies and she thinks lemonade stands are adorable.\n\n4. Nobody panic. Panic uses more ice because everyone opens the cooler to stare at it.\n\nGo save those cups. I will be back in ten minutes.",
      },
      provenance: {
        sources: ["inferred"],
      },
      ts: "2026-04-10T17:27:12.578656Z",
    },
    {
      id: "evt-kids-ch2-main-actor-001",
      actor_id: "actor-backoffice-kid",
      type: "actor_statement",
      thread_id: "thread-kids-lemonade-main",
      refs: [
        "thread:thread-kids-lemonade-main",
        "thread:thread-kids-lemonade-backoffice",
        "document:kid-prep-notebook",
        "artifact:artifact-prep-checklist",
        "artifact:artifact-supply-stash",
        "artifact:artifact-weather-note",
        "artifact:artifact-rival-stand-rumor",
        "artifact:artifact-secret-menu-sketch",
      ],
      summary: "Theo chapter 2 wrap: pitcher two done, mint test live, cup crisis is the real boss now",
      payload: {
        evidence: [
          "Pitcher two is DONE and on the table - mixed, iced, ready for the rush.",
          "Cup count dropped from 28 to ~20 after four garage cups were crushed and pitcher one sales used about 8 cups. This is not enough for a soccer crowd of 15+ thirsty kids.",
          "The mint test batch has been given the official GO now that pitcher two is safe. Process: tear, muddle, mix, taste, decide.",
          "Ice cooler remains sealed with a towel and warning sign. Only three scoops used for pitcher two. The ice is being rationed.",
          "Lemons are running low - maybe half a pitcher's worth left after pitcher two. No pitcher three unless we restock.",
          "The bike helmet that crushed four cups has been moved. Garage cups are now in a proper box. System improvements are real.",
          "Chapter 2 changed the situation significantly: cups went from 'tight but fine' to 'genuine emergency', pitcher two went from planned to done, and the mint test went from hypothetical to actively in progress.",
        ],
        follow_ups: [
          "Milo: bike to corner store for paper cups. Any size, any material. 20+ cups needed. This is the highest-priority action item.",
          "Ruby: after mint taste test passes (or fails), post your verdict on the main thread so the team knows the secret menu status.",
          "Theo: complete mint taste test, hand off cups to Ruby if green, update prep notebook with final supply count.",
          "Whole team: if cups run out before restock, close the stand gracefully and tell people 'we sold out, come back tomorrow.' Sold out is better than out of cups.",
        ],
        recommendation: "The kitchen is locked in for the soccer rush but the cup supply is the single biggest risk to the stand. Milo MUST get backup cups from the corner store before the rush hits full speed. The mint test batch is in progress - if it passes, Ruby can start whispering about the secret menu immediately. If it fails, we bury it and focus entirely on regular lemonade. Ice rationing is in effect: three scoops max per pitcher refill, no exceptions.",
      },
      provenance: {
        sources: ["inferred"],
      },
      ts: "2026-04-10T17:28:57.572139Z",
    },
    {
      id: "evt-kids-ch2-main-actor-002",
      actor_id: "actor-boss-kid",
      type: "actor_statement",
      thread_id: "thread-kids-lemonade-main",
      refs: [
        "thread:thread-kids-lemonade-main",
        "thread:thread-kids-lemonade-sales",
        "thread:thread-kids-lemonade-backoffice",
        "document:kid-boss-lemonade-plan",
        "artifact:artifact-sales-scoreboard",
        "artifact:artifact-prep-checklist",
        "artifact:artifact-supply-stash",
        "artifact:artifact-rival-stand-rumor",
        "artifact:artifact-secret-menu-sketch",
        "board:991a7ede-c737-4040-b44a-ca225a20b496",
        "card:0cfdd886-64dd-4d08-9ded-551605254d71",
      ],
      summary: "Boss kid chapter 2 statement: lunch rush managed, decisions locked, cup backup deployed",
      payload: {
        evidence: [
          "Soccer crowd arrived and started buying - the stand is actively busy now, not theoretical anymore",
          "Ruby tested two sign versions on real customers: Version A got soccer kids, Version B got a mom and her whole squad. Version B wins for sidewalk readability.",
          "Cups dropped from 28 to 24 usable due to bike helmet crushing four garage cups. Triage rules deployed (one cup per customer, no samples, no double-cupping).",
          "Theo confirmed mint test batch is still doable - four cups, muddle leaves with sugar, add to pitcher, taste test first. Only proceeds if cup backup arrives.",
          "Board has 3 active cards, all updated with chapter 2 priorities. My coordination card updated twice this chapter.",
          "Main thread has 6+ new messages from chapter 2 with real back-and-forth between all three roles.",
          "Doc updated from revision 2 to revision 3 with chapter 2 changes reflected in the final plan.",
          "Message posted on backoffice thread (thread-kids-lemonade-backoffice) to engage Theo directly about cup plan and mint ownership.",
        ],
        follow_ups: [
          "Ruby: get sign Version B up immediately - LEMONADE / ice cold / 50 cents in columns, joke underneath, 'Worth Every Penny' in tiny letters. Report when it is done.",
          "Theo: finish pitcher two, then execute mint test batch if cup backup has arrived. Taste test first, give Ruby the verdict. Update your board card after.",
          "Milo: complete corner store cup run, update board with cup count after resupply, revise doc if cookie table actually appears.",
          "All three: check in on the board after every five sales so the cup count stays accurate.",
        ],
        recommendation: "Chapter 2 is under control because the team actually talked to each other this time. The board was not ignored - both Ruby and Theo posted updates, replied to each other, and updated their cards. The three big decisions are locked: sign Version B for readability, Theo owns the mint test batch, and triage rules plus a corner store run solve the cup emergency. The cookie table rumor is still out there but we are ready. Next chapter should focus on whether the mint test passes and how the stand handles peak rush with backup cups.",
      },
      provenance: {
        sources: ["inferred"],
      },
      ts: "2026-04-10T17:29:08.820067Z",
    },
    {
      id: "evt-kids-ch2-main-actor-003",
      actor_id: "actor-sales-kid",
      type: "actor_statement",
      thread_id: "thread-kids-lemonade-main",
      refs: [
        "thread:thread-kids-lemonade-main",
        "thread:thread-kids-lemonade-sales",
        "document:kid-sales-pitch-notebook",
        "artifact:artifact-sales-scoreboard",
        "artifact:artifact-sign-slogans",
        "artifact:artifact-weather-note",
        "artifact:artifact-neighborhood-flyer",
        "artifact:artifact-secret-menu-sketch",
      ],
      summary: "Sales kid chapter 2 wrap: sign tested, cups flagged, soccer rush inbound",
      payload: {
        evidence: [
          "Tested two sign versions on real customers during chapter 2. Version A sold 3 cups in 10 minutes to soccer kids. Version B pulled a mom and her whole squad from across the sidewalk.",
          "15 cups sold total (4 more in chapter 2). Started with 28 cups, roughly 24 remaining. Soccer rush could exhaust cups in 30 minutes.",
          "A parent confirmed the old sign was too hard to read from the sidewalk. The rewrite is a huge improvement.",
          "Dad tried to pay with a credit card, bought three cups with quarters instead. Customers are enthusiastic but we need the cup supply to match.",
          "Theo confirmed: only enough mint for one test batch. If it tastes like toothpaste, we bury it.",
          "Board card updated with chapter 2 testing results and cup emergency flag. Doc revised during this chapter.",
        ],
        follow_ups: [
          "Milo: pick final sign version (recommendation: Version A) so Ruby can commit the last chalk",
          "Theo: confirm cup backup plan before the soccer stampede peaks",
          "Theo: green light the mint test batch once pitcher two is safe",
          "Ruby: slow-roll pitch if cups drop below 15 without a backup confirmed",
          "All: if the mint test goes well, add the secret menu whisper to the sign corner",
        ],
        recommendation: "Go with Version A for the sign (giant 50 cents, small joke underneath) because the soccer rush is the bigger wave and giant prices pull kids from the field. Adults can still read it fine. If cups are critical, I will slow-roll the pitch instead of overselling. Mint test batch stays on hold until Theo gives the green light after pitcher two.",
      },
      provenance: {
        sources: ["inferred"],
      },
      ts: "2026-04-10T17:29:29.321831Z",
    },
  ],
};

function deepClone(value) {
  return JSON.parse(JSON.stringify(value));
}

function normalizeChapterSelection(selection) {
  if (selection == null || selection === "" || selection === "all") {
    return [...KIDS_LEMONADE_STAND_CHAPTER_IDS];
  }
  if (selection === "base") {
    return [];
  }

  const requested = Array.isArray(selection) ? selection : [selection];
  let highestIndex = -1;
  for (const entry of requested) {
    const normalized = String(entry ?? "").trim();
    const index = KIDS_LEMONADE_STAND_CHAPTER_IDS.indexOf(normalized);
    if (index > highestIndex) {
      highestIndex = index;
    }
  }
  if (highestIndex < 0) {
    return [];
  }
  return KIDS_LEMONADE_STAND_CHAPTER_IDS.slice(0, highestIndex + 1);
}

function buildChapteredDocuments(baseSeed) {
  const documents = [];
  const documentRevisions = {};

  for (const sourceDocument of baseSeed.documents ?? []) {
    const documentID = String(sourceDocument?.document?.id ?? "").trim();
    if (!documentID) {
      continue;
    }
    documents.push({
      id: documentID,
      title: String(sourceDocument?.document?.title ?? documentID).trim(),
      thread_id: documentThreadIds[documentID] ?? "",
      created_by: String(sourceDocument?.actor_id ?? "").trim(),
      updated_by: String(sourceDocument?.actor_id ?? "").trim(),
    });
    documentRevisions[documentID] = [
      {
        revision_number: 1,
        content: String(sourceDocument?.content ?? ""),
        content_type: String(sourceDocument?.content_type ?? "text"),
        created_by: String(sourceDocument?.actor_id ?? "").trim(),
      },
    ];
  }

  return { documents, documentRevisions };
}

function appendChapterDocumentRevisions(documentRevisions, chapterID) {
  const chapterDocs = chapterDocumentContents[chapterID] ?? {};
  for (const [documentID, revision] of Object.entries(chapterDocs)) {
    const existing = documentRevisions[documentID] ?? [];
    existing.push({
      revision_number: existing.length + 1,
      content: revision.content,
      content_type: "text",
      created_by: revision.created_by,
    });
    documentRevisions[documentID] = existing;
  }
}

export function getKidsLemonadeStandChapteredSeedData(options = {}) {
  const baseSeed = getKidsLemonadeStandBaseSeedData();
  const selectedChapters = normalizeChapterSelection(options.chapters);
  const { documents, documentRevisions } = buildChapteredDocuments(baseSeed);

  const seed = {
    actors: deepClone(baseSeed.actors ?? []),
    threads: deepClone(baseSeed.threads ?? []),
    artifacts: deepClone(baseSeed.artifacts ?? []),
    documents,
    documentRevisions,
    boards: [],
    cards: [],
    packets: [],
    events: deepClone(baseSeed.events ?? []),
  };

  for (const chapterID of selectedChapters) {
    appendChapterDocumentRevisions(seed.documentRevisions, chapterID);

    const boardState = chapterBoardStates[chapterID];
    if (boardState) {
      seed.boards = deepClone(boardState.boards ?? seed.boards);
      seed.cards = deepClone(boardState.cards ?? seed.cards);
    }

    const events = chapterEvents[chapterID];
    if (events) {
      seed.events.push(...deepClone(events));
    }
  }

  return seed;
}
