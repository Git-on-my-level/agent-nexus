import copy
import io
import json
import sys
import tempfile
import unittest
from contextlib import redirect_stdout, redirect_stderr
from datetime import datetime, timezone
from pathlib import Path
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from anx_client import AnxError, AnxClient
from initiatives import (validate_mapping, route, plan_ingestion, apply_ingestion,
                         evidence_entries, merge_evidence, add_unsorted_panel, source_items)
from migrate import inventory, apply_migration, digest
from fleet_sync import main, validate_report, node_binary, validator_script
from report import build_report

WS = 'https://anx.example.test/workspace'
NOW = datetime(2026, 10, 5, tzinfo=timezone.utc)
READS = [{'name': 'multica', 'ok': True, 'complete': True, 'observed_at': '2026-10-05T00:00:00Z', 'items': [], 'meta': {}}]


def mapping(**extra):
    return {'version': 1, 'workspace': WS, 'rules': [
        {'id': 'project', 'initiative': 'card:initiative', 'match': {'project': 'p'}}], 'pins': [], **extra}


def item(n=1, **extra):
    return {'authority': 'multica', 'connection_id': 'local', 'native_id': str(n),
            'title': f'Issue {n}', 'status': 'in_progress', 'project': 'p', 'labels': [],
            'repo': None, 'url': f'https://source.test/{n}', **extra}


class Client:
    def __init__(self):
        self.cards = {'card:initiative': {'ref': 'card:initiative', 'title': 'Outcome', 'summary': 'Human narrative',
                      'head_revision_ref': 'revision:1', 'definition_of_done': ['accepted'],
                      'related_refs': ['doc:context'], 'provenance': {'sources': ['reported']}, 'board_ref': 'board:main'}}
        self.works = {'card:initiative': {'source': {'authority': 'nexus'}}}
        self.writes = []
        self.stamp = 1
        self.fail_revise = False

    def work_get(self, ref):
        return copy.deepcopy(self.works[ref])

    def card_get(self, ref):
        return copy.deepcopy(self.cards[ref])

    def card_revise(self, ref, body):
        if self.fail_revise or body['if_base_revision'] != self.cards[ref]['head_revision_ref']:
            raise AnxError('conflict', 'card changed')
        self.writes.append(('revise', ref))
        self.cards[ref].update(copy.deepcopy(body['revision']))
        self.stamp += 1
        self.cards[ref]['head_revision_ref'] = f'revision:{self.stamp}'
        return {'card': self.card_get(ref)}

    def work_list(self, source):
        return [copy.deepcopy(w) for w in self.works.values() if w['source']['authority'] == source and not self.cards.get(w.get('ref'), {}).get('archived_at')]

    def work_patch(self, ref, body):
        if body['if_version'] != self.works[ref]['version']:
            raise AnxError('conflict', 'annotations changed')
        self.works[ref].update(copy.deepcopy(body['patch']))
        self.works[ref]['version'] += 1
        self.writes.append(('tombstone', ref))

    def board_get(self, ref):
        return {'updated_at': str(self.stamp)}

    def card_archive(self, ref, stamp, *, observation_id, work_version):
        if (observation_id != self.works[ref]['latest_observation']['id']
                or work_version != self.works[ref]['version']):
            raise AnxError('conflict', 'source changed')
        if stamp != str(self.stamp):
            raise AnxError('conflict', 'board changed')
        self.writes.append(('archive', ref))
        self.cards[ref]['archived_at'] = 'now'
        self.stamp += 1

    def add_legacy(self, n, **extra):
        ref = f'card:legacy-{n}'
        it = item(n, **extra)
        work = {'ref': ref, 'title': it['title'], 'phase': it['status'], 'updated_at': 'before',
                'source': {k: it[k] for k in ('authority', 'connection_id', 'native_id', 'url')},
                'head_revision_ref': 'revision:old', 'version': 1, 'relations': [], 'latest_observation': {
                    'id': f'observation:{n}', 'reader_id': 'fleet-sync/multica', 'facts': {'project': it['project'], 'labels': it['labels']}}}
        self.works[ref] = work
        self.cards[ref] = {'ref': ref, 'title': it['title'], 'summary': 'Original evidence',
                           'head_revision_ref': 'revision:old', 'board_ref': 'board:main'}
        return ref


class RoutingTests(unittest.TestCase):
    def test_pin_precedence_and_rules_are_ordered_and_conjunctive(self):
        m = mapping(pins=[{k: item()[k] for k in ('authority', 'connection_id', 'native_id')} | {'initiative': 'card:pinned'}])
        validate_mapping(m, WS)
        self.assertEqual(route(item(), m), ('card:pinned', 'pin'))
        self.assertEqual(route(item(2), m), ('card:initiative', 'rule:project'))
        m['rules'][0]['match'] = {'repo': 'org/repo', 'labels': ['ship'], 'title_pattern': '^Issue', 'authority': 'multica', 'connection_id': 'local'}
        self.assertIsNone(route(item(2, repo='org/repo'), m)[0])
        self.assertEqual(route(item(2, repo='org/repo', labels=['ship', 'other']), m)[0], 'card:initiative')

    def test_invalid_scope_typo_and_regex_fail_closed(self):
        for m in [mapping(workspace='other'), mapping(proposal_threshold=1), mapping(extra=True),
                  mapping(rules=[{'id': 'x', 'initiative': 'card:x', 'match': {'titel': 'x'}}]),
                  mapping(rules=[{'id': 'x', 'initiative': 'card:x', 'match': {'title_pattern': '['}}])]:
            with self.subTest(m=m), self.assertRaises(ValueError):
                validate_mapping(m, WS)

    def test_connection_is_part_of_pin_identity(self):
        m = mapping(rules=[], pins=[{k: item()[k] for k in ('authority', 'connection_id', 'native_id')} | {'initiative': 'card:pinned'}])
        self.assertIsNone(route(item(connection_id='elsewhere'), m)[0])

    def test_bounded_batch_and_rerun_without_any_local_state(self):
        for n in (1, 5, 340):
            with self.subTest(n=n):
                client = Client()
                items = [item(i) for i in range(n)]
                plan = plan_ingestion(items, mapping(), client)
                self.assertEqual(plan['counts']['new_cards'], 0)
                self.assertEqual(apply_ingestion(client, plan)['revised'], 1)
                self.assertEqual(client.writes, [('revise', 'card:initiative')])
                again = plan_ingestion(list(reversed(items)), mapping(), client)
                self.assertEqual(apply_ingestion(client, again)['skipped'], 1)
                self.assertEqual(len(client.writes), 1)
                card = client.card_get('card:initiative')
                self.assertTrue(card['summary'].startswith('Human narrative'))
                self.assertEqual(card['definition_of_done'], ['accepted'])
                self.assertEqual(len(evidence_entries(card['summary'])), n)

    def test_status_change_and_partial_read_preserve_other_evidence(self):
        client = Client()
        apply_ingestion(client, plan_ingestion([item(1), item(2)], mapping(), client))
        apply_ingestion(client, plan_ingestion([item(1, status='done')], mapping(), client))
        entries = evidence_entries(client.cards['card:initiative']['summary'])
        self.assertEqual({e['native_id']: e['status'] for e in entries}, {'1': 'done', '2': 'in_progress'})

    def test_invalid_target_and_conflict_never_create(self):
        client = Client()
        plan = plan_ingestion([item()], mapping(), client)
        client.fail_revise = True
        self.assertTrue(apply_ingestion(client, plan)['errors'])
        self.assertEqual(client.writes, [])
        client.works['card:initiative']['source']['authority'] = 'multica'
        with self.assertRaises(ValueError):
            plan_ingestion([item()], mapping(), client)

    def test_unmatched_cluster_proposes_once_never_creates(self):
        client = Client()
        plan = plan_ingestion([item(i) for i in range(340)], mapping(rules=[]), client)
        self.assertEqual(len(plan['unsorted']), 340)
        self.assertEqual(len(plan['proposals']), 1)
        self.assertEqual(plan['proposals'][0]['count'], 340)
        apply_ingestion(client, plan)
        self.assertEqual(client.writes, [])
        self.assertEqual(plan_ingestion([item(i) for i in range(4)], mapping(rules=[]), client)['proposals'], [])

    def test_shared_label_is_found_with_distinct_earlier_labels(self):
        for with_project_repo in (False, True):
            with self.subTest(with_project_repo=with_project_repo):
                items = [item(i, project=f'p-{i}' if with_project_repo else None,
                              repo=f'org/repo-{i}' if with_project_repo else None,
                              labels=[f'a-{i}', 'shared-initiative']) for i in range(5)]
                plan = plan_ingestion(items, mapping(rules=[]), Client())
                self.assertEqual(len(plan['proposals']), 1)
                self.assertEqual(plan['proposals'][0]['cluster'][2], 'label:shared-initiative')
                self.assertEqual(plan['proposals'][0]['count'], 5)

    def test_all_dimensions_and_overlaps_form_one_stable_suggestion(self):
        items = [item(i, repo='org/repo', labels=['release', 'shared', 'shared']) for i in range(5)]
        proposals = plan_ingestion(items, mapping(rules=[]), Client())['proposals']
        self.assertEqual(len(proposals), 1)
        self.assertEqual(proposals[0]['count'], 5)
        self.assertEqual({c[2] for c in proposals[0]['clusters']},
                         {'project:p', 'repo:org/repo', 'label:release', 'label:shared'})
        for row in items:
            row['labels'].reverse()
        self.assertEqual(plan_ingestion(items[::-1], mapping(rules=[]), Client())['proposals'], proposals)

    def test_partial_and_transitive_overlap_deduplicates_members(self):
        items = [item(i, project=None, labels=[label for label, members in
                 [('a', range(5)), ('b', range(4, 9)), ('c', range(8, 13))] if i in members])
                 for i in range(13)]
        proposals = plan_ingestion(items, mapping(rules=[]), Client())['proposals']
        self.assertEqual(len(proposals), 1)
        self.assertEqual(proposals[0]['count'], 13)
        self.assertEqual(len(proposals[0]['clusters']), 3)

    def test_disjoint_scopes_stay_separate_and_threshold_precedes_union(self):
        items = [item(i, connection_id=connection, labels=['shared'])
                 for connection in ('local', 'other') for i in range(5)]
        proposals = plan_ingestion(items, mapping(rules=[]), Client())['proposals']
        self.assertEqual([p['count'] for p in proposals], [5, 5])
        items = [item(i, project=None, labels=['a'] if i < 5 else ['b']) for i in range(10)]
        proposals = plan_ingestion(items, mapping(rules=[]), Client())['proposals']
        self.assertEqual([p['count'] for p in proposals], [5, 5])
        items = [item(i, project=None, labels=['a'] if i < 3 else ['b']) for i in range(6)]
        self.assertEqual(plan_ingestion(items, mapping(rules=[]), Client())['proposals'], [])


    def test_source_reader_metadata_reaches_routing(self):
        reads = [{'name': 'multica', 'ok': True, 'complete': True,
            'observed_at': '2026-10-05T00:00:00Z', 'items': [
            {'native_id': 'source-1', 'identifier': 'SCA-1', 'title': 'A milestone',
             'status': 'done', 'project': 'p', 'labels': ['release'], 'url': 'https://source.test/1',
             'updated_at': '2026-10-04T12:00:00Z'}]}]
        rows = source_items(reads, {'multica': {'connection_id': 'local'}}, NOW)
        self.assertEqual(rows[0]['project'], 'p')
        self.assertEqual(rows[0]['labels'], ['release'])
        self.assertEqual(rows[0]['status'], 'done')
        self.assertEqual(rows[0]['updated_at'], '2026-10-04T12:00:00Z')
        self.assertEqual(rows[0]['_observed_at'], reads[0]['observed_at'])
        self.assertEqual(route(rows[0], mapping())[0], 'card:initiative')

    def test_volatile_duplicate_uses_newest_timestamp_and_reports_warning(self):
        older = item(status='in_progress', title='Issue 1', labels=['old'], url='https://source.test/old',
                     updated_at='2026-10-04T11:00:00Z', _observed_at='2026-10-04T12:00:00Z', _reader='github')
        newer = item(status='done', title='Issue 1 updated', labels=['new'], url='https://source.test/new',
                     updated_at='2026-10-05T00:00:00Z', _observed_at='2026-10-04T10:00:00Z', _reader='multica')
        client = Client()
        plan = plan_ingestion([older, newer], mapping(), client)
        planned = plan['initiatives'][0]['items'][0]
        self.assertEqual(planned['status'], 'done')
        self.assertEqual(planned['title'], 'Issue 1 updated')
        self.assertNotIn('_reader', planned)
        warning = plan['warnings'][0]
        self.assertEqual(warning['identity'], ['multica', 'local', '1'])
        self.assertEqual(warning['selected_reader'], 'multica')
        self.assertEqual(warning['selected_timestamp'], '2026-10-05T00:00:00Z')
        self.assertEqual(warning['volatile_fields'], ['labels', 'status', 'title', 'updated_at', 'url'])
        self.assertIn('updated_at', warning['differing_fields'])
        self.assertEqual(apply_ingestion(client, plan)['warnings'], [warning])
        self.assertEqual(plan_ingestion([newer, older], mapping(), Client())['warnings'], [warning])

    def test_reader_priority_is_a_stable_timestamp_tiebreak(self):
        common = {'updated_at': '2026-10-05T00:00:00Z', '_observed_at': '2026-10-05T00:01:00Z'}
        github = item(status='in_progress', _reader='github', **common)
        multica = item(status='done', _reader='multica', **common)
        for candidates in ([github, multica], [multica, github]):
            plan = plan_ingestion(candidates, mapping(), Client())
            self.assertEqual(plan['initiatives'][0]['items'][0]['status'], 'done')
            self.assertEqual(plan['warnings'][0]['selected_reader'], 'multica')

    def test_observation_time_breaks_ties_when_updated_at_is_missing(self):
        earlier = item(status='in_progress', _reader='github', _observed_at='2026-10-04T23:59:00Z')
        later = item(status='done', _reader='github', _observed_at='2026-10-05T00:00:00Z')
        plan = plan_ingestion([earlier, later], mapping(), Client())
        self.assertEqual(plan['initiatives'][0]['items'][0]['status'], 'done')
        self.assertEqual(plan['warnings'][0]['selected_timestamp'], '2026-10-05T00:00:00Z')

    def test_routing_conflict_names_item_identity_and_differing_fields(self):
        client = Client()
        with self.assertRaisesRegex(ValueError, r"Issue 1.*identity=\[\"multica\",\"local\",\"1\"\].*project"):
            plan_ingestion([item(), item(project='other')], mapping(), client)
        self.assertEqual(client.writes, [])

    def test_duplicate_rows_keep_their_own_timestamps_and_metadata(self):
        read = {'name': 'multica', 'ok': True, 'complete': True,
                'observed_at': '2026-10-05T00:00:00Z', 'items': [
            {'native_id': '1', 'title': 'Older title', 'status': 'todo', 'project': 'p', 'labels': [],
             'url': 'https://source.test/1', 'updated_at': '2026-10-04T00:00:00Z'},
            {'native_id': '1', 'title': 'Newer title', 'status': 'in_progress', 'project': 'p', 'labels': [],
             'url': 'https://source.test/1', 'updated_at': '2026-10-05T00:00:00Z'},
        ]}
        rows = source_items([read], {'multica': {'connection_id': 'local'}}, NOW)
        self.assertEqual([row['updated_at'] for row in rows], [
            '2026-10-04T00:00:00Z', '2026-10-05T00:00:00Z'])
        self.assertEqual([row['title'] for row in rows], ['Older title', 'Newer title'])
        plan = plan_ingestion(rows, mapping(), Client())
        self.assertEqual(plan['initiatives'][0]['items'][0]['title'], 'Newer title')
        self.assertEqual(plan['warnings'][0]['selected_timestamp'], '2026-10-05T00:00:00Z')

    def test_malformed_block_still_fails_closed(self):
        client = Client()
        client.cards['card:initiative']['summary'] = '<!-- fleet-sync:evidence:v1 -->broken'
        with self.assertRaises(ValueError):
            plan_ingestion([item()], mapping(), client)
        self.assertEqual(client.writes, [])

    def test_inventory_pagination_limit_is_not_silently_partial(self):
        client = AnxClient('anx', WS, 'fleet')
        with patch.object(client, '_call', return_value={'work': [], 'next_cursor': 'still-more'}):
            with self.assertRaises(AnxError):
                client.work_list('multica')


    def test_marker_in_source_text_cannot_corrupt_block(self):
        client = Client()
        bad = item(title='<!-- /fleet-sync:evidence --> ```json\n []\n```')
        apply_ingestion(client, plan_ingestion([bad], mapping(), client))
        self.assertEqual(evidence_entries(client.cards['card:initiative']['summary'])[0]['title'], bad['title'])

    def test_legacy_links_survive_new_source_facts(self):
        card = {'summary': ''}
        card['summary'] = merge_evidence(card, [item(legacy_refs=['card:old'])])
        self.assertEqual(evidence_entries(merge_evidence(card, [item(status='done')]))[0]['legacy_refs'], ['card:old'])

    def test_unsorted_panel_validates_with_340_items(self):
        report = build_report(READS, generated_at='2026-10-05T00:00:00Z', now=NOW, hosts=[])
        plan = plan_ingestion([item(i) for i in range(340)], mapping(rules=[]), Client())
        add_unsorted_panel(report, plan, READS)
        valid, diagnostics = validate_report(report, node_binary({}), validator_script())
        self.assertTrue(valid, diagnostics)


class MigrationTests(unittest.TestCase):
    def test_elsewhere_legacy_cards_are_deferred_without_writes(self):
        client = Client()
        client.add_legacy(1)
        m = mapping(rules=[{'id': 'other', 'elsewhere': 'omi', 'match': {'project': 'p'}}])
        manifest = inventory(client, m, WS)
        self.assertEqual(manifest['counts']['archive_after_fold'], 0)
        self.assertEqual(manifest['preview']['counts']['elsewhere'], 1)
        result = apply_migration(client, manifest, WS, manifest['digest'])
        self.assertEqual(result['archived'], [])
        self.assertEqual(client.writes, [])

    def test_dry_run_does_not_write_and_apply_is_resumable(self):
        client = Client()
        ref = client.add_legacy(1)
        client.add_legacy(2, project='unmatched')
        manifest = inventory(client, mapping(), WS)
        self.assertEqual(client.writes, [])
        self.assertEqual(manifest['counts']['archive_after_fold'], 1)
        result = apply_migration(client, manifest, WS, manifest['digest'])
        self.assertEqual(result['archived'], [ref])
        self.assertEqual(client.writes, [('revise', 'card:initiative'), ('tombstone', ref), ('archive', ref)])
        self.assertTrue(client.cards[ref]['summary'].startswith('Original evidence'))
        again = apply_migration(client, manifest, WS, manifest['digest'])
        self.assertEqual(again['already_archived'], [ref])
        self.assertEqual(len(client.writes), 3)

    def test_changed_source_or_unapproved_manifest_fails_before_writes(self):
        client = Client()
        ref = client.add_legacy(1)
        manifest = inventory(client, mapping(), WS)
        with self.assertRaises(ValueError):
            apply_migration(client, manifest, WS, 'wrong')
        client.works[ref]['latest_observation']['facts']['phase'] = 'done'
        with self.assertRaises(ValueError):
            apply_migration(client, manifest, WS, manifest['digest'])
        self.assertEqual(client.writes, [])

    def test_fold_failure_does_not_archive(self):
        client = Client()
        client.add_legacy(1)
        manifest = inventory(client, mapping(), WS)
        client.fail_revise = True
        with self.assertRaises(ValueError):
            apply_migration(client, manifest, WS, manifest['digest'])
        self.assertEqual(client.writes, [])

    def test_non_adapter_source_card_is_not_migrated(self):
        client = Client()
        ref = client.add_legacy(1)
        client.works[ref]['latest_observation']['reader_id'] = 'another-adapter'
        manifest = inventory(client, mapping(), WS)
        self.assertEqual(manifest['counts']['legacy'], 0)
        self.assertEqual(len(manifest['skipped']), 1)

    def test_new_observation_after_final_readback_prevents_local_migration_archive(self):
        client = Client()
        ref = client.add_legacy(1)
        manifest = inventory(client, mapping(), WS)
        original = client.card_archive

        def observe_before_archive(ref, stamp, **fences):
            client.works[ref]['latest_observation']['id'] = 'new-poll-same-phase'
            return original(ref, stamp, **fences)

        with patch.object(client, 'card_archive', side_effect=observe_before_archive):
            with self.assertRaises(AnxError):
                apply_migration(client, manifest, WS, manifest['digest'])
        self.assertFalse(client.cards[ref].get('archived_at'))

    def test_archive_failure_can_resume_after_tombstone(self):
        client = Client()
        ref = client.add_legacy(1)
        manifest = inventory(client, mapping(), WS)
        with patch.object(client, 'card_archive', side_effect=AnxError('unavailable', 'offline')):
            with self.assertRaises(AnxError):
                apply_migration(client, manifest, WS, manifest['digest'])
        self.assertFalse(client.cards[ref].get('archived_at'))
        self.assertEqual(apply_migration(client, manifest, WS, manifest['digest'])['archived'], [ref])


class MainTests(unittest.TestCase):
    def test_plan_reads_destinations_and_writes_nothing(self):
        config = {'base_url': WS, 'agent': 'fleet', 'topic': 'fleet', 'boards': {'agent_work':'b', 'pull_requests':'b', 'ops_hygiene':'b'}}
        client = Client()
        with tempfile.TemporaryDirectory() as directory:
            cfg, rules, state = [Path(directory)/s for s in ('config.json', 'mapping.json', 'state.json')]
            cfg.write_text(json.dumps(config)); rules.write_text(json.dumps(mapping()))
            with patch('fleet_sync.AnxClient', return_value=client), patch('fleet_sync.collect', return_value=READS), patch('fleet_sync.source_items', return_value=[item()]), redirect_stdout(io.StringIO()) as output:
                code = main(['--config', str(cfg), '--mapping-file', str(rules), '--state', str(state), '--plan'])
            self.assertEqual(code, 0)
            self.assertEqual(json.loads(output.getvalue())['planned_writes']['initiatives'][0]['action'], 'revise')
            self.assertFalse(state.exists())
            self.assertEqual(client.writes, [])
            with patch('fleet_sync.AnxClient', return_value=client), redirect_stderr(io.StringIO()):
                self.assertEqual(main(['--config', str(cfg)]), 1)


if __name__ == '__main__':
    unittest.main()
