/**
 * Dev workspace fixtures: canonical seed payload and pure test helpers.
 *
 * Data lives in `./devWorkspaceFixtures.js`; import from here for seeds, e2e, and scripts.
 */
export {
  DEV_FIXTURE_PERSONAS,
  getDevSeedData,
  mockTopicRefFromThreadId,
  mockTopicRefSuffixFromThreadId,
  buildMockTopicWorkspaceFromThreadWorkspace,
} from "./devWorkspaceFixtures.js";
