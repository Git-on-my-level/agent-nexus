import copy
import json
import unittest
from unittest.mock import patch

from test_initiatives import Client, WS, item, mapping
from anx_client import AnxError
from initiatives import digest, merge_evidence
from retire_elsewhere import inventory, apply_retirement

DEST = 'https://anx.example.test/other'


class RetirementTests(unittest.TestCase):
    def setUp(self):
        self.source, self.dest = Client(), Client()
        self.ref = self.source.add_legacy(1)
        self.config = {'base_url': WS, 'mapping_doc': 'doc:mapping', 'agent': 'source-reader',
                       'destinations': {'other': {'base_url': DEST, 'mapping_doc': 'doc:mapping', 'agent': 'destination-reader'}}}
        self.source_mapping = mapping(rules=[{'id': 'other', 'elsewhere': 'other', 'match': {'project': 'p'}}])
        self.dest_mapping = mapping(workspace=DEST)
        self.source.docs_content = lambda ref: json.dumps(self.source_mapping)
        self.source.docs_ref = lambda ref: 'document:mapping'
        self.dest.docs_content = lambda ref: json.dumps(self.dest_mapping)
        self.dest.cards['card:initiative']['summary'] = merge_evidence(self.dest.cards['card:initiative'], [item()])
        self.candidates = [{'ref': self.ref, 'source': {k: item()[k] for k in ('authority', 'connection_id', 'native_id')}, 'destination': 'other'}]
        self.clients = {'other': self.dest}

    def preview(self):
        return inventory(self.source, self.config, self.candidates, self.clients)

    def apply(self, manifest):
        return apply_retirement(self.source, self.config, manifest, manifest['digest'], self.clients)

    def test_dry_run_read_only_and_apply_only_writes_source_with_url_tombstone(self):
        manifest = self.preview()
        self.assertEqual(manifest['counts']['archive_after_verification'], 1)
        self.assertEqual(self.source.writes + self.dest.writes, [])
        self.assertEqual(self.apply(manifest)['archived'], [self.ref])
        self.assertEqual(self.source.writes, [('tombstone', self.ref), ('archive', self.ref)])
        self.assertEqual(self.dest.writes, [])
        self.assertEqual(self.source.cards[self.ref]['summary'], 'Original evidence')
        self.assertEqual(self.source.works[self.ref]['relations'][0]['ref'], 'document:mapping')
        self.assertEqual(self.source.works[self.ref]['relations'][0]['destination_url'], DEST + '/tasks/card%3Ainitiative')
        self.assertEqual(self.apply(manifest)['already_archived'], [self.ref])
        self.assertEqual(len(self.source.writes), 2)

    def test_missing_credentials_or_evidence_defers_never_promotes_on_apply(self):
        with patch.object(self.dest, 'docs_content', side_effect=AnxError('host_not_enrolled', 'host is not enrolled')):
            manifest = self.preview()
        self.assertIn('not enrolled', manifest['cards'][0]['deferred_reason'])
        self.assertEqual(self.apply(manifest)['archived'], [])
        self.dest.cards['card:initiative']['summary'] = 'No evidence'
        self.assertEqual(self.preview()['counts']['deferred'], 1)
        self.assertEqual(self.source.writes + self.dest.writes, [])

    def test_full_identity_required(self):
        for field in ('authority', 'connection_id', 'native_id'):
            with self.subTest(field=field):
                self.dest.cards['card:initiative']['summary'] = merge_evidence({'summary': ''}, [item(**{field: 'different'})])
                self.assertEqual(self.preview()['counts']['deferred'], 1)

    def test_unproven_source_ownership_and_missing_source_defer(self):
        self.source.works[self.ref]['latest_observation']['reader_id'] = 'other-writer'
        self.assertEqual(self.preview()['counts']['deferred'], 1)
        del self.source.works[self.ref]
        self.assertEqual(self.preview()['counts']['deferred'], 1)

    def test_missing_or_wrong_destination_ownership_fails_before_mutation(self):
        for mutation in ('evidence', 'archived', 'source'):
            with self.subTest(mutation=mutation):
                self.setUp()
                manifest = self.preview()
                if mutation == 'evidence':
                    self.dest.cards['card:initiative']['summary'] = ''
                elif mutation == 'archived':
                    self.dest.cards['card:initiative']['archived_at'] = 'now'
                else:
                    self.dest.works['card:initiative']['source']['authority'] = 'github'
                with self.assertRaises(ValueError):
                    self.apply(manifest)
                self.assertEqual(self.source.writes + self.dest.writes, [])

    def test_changed_source_mapping_destination_or_digest_fails(self):
        for mutation in ('source', 'source_mapping', 'destination_mapping', 'endpoint', 'digest'):
            with self.subTest(mutation=mutation):
                self.setUp()
                manifest = self.preview()
                if mutation == 'source':
                    self.source.works[self.ref]['latest_observation']['facts']['status'] = 'changed'
                elif mutation == 'source_mapping':
                    self.source_mapping['rules'] = []
                elif mutation == 'destination_mapping':
                    self.dest_mapping['rules'] = []
                elif mutation == 'endpoint':
                    self.config['destinations']['other']['base_url'] = DEST + '-changed'
                else:
                    manifest['cards'][0]['initiative_url'] = 'https://wrong.test'
                with self.assertRaises(ValueError):
                    self.apply(manifest)
                self.assertEqual(self.source.writes + self.dest.writes, [])

    def test_url_tampering_rejected_even_with_recomputed_digest(self):
        manifest = self.preview()
        manifest['cards'][0]['initiative_url'] = 'https://wrong.test'
        manifest['digest'] = digest({k: v for k, v in manifest.items() if k != 'digest'})
        with self.assertRaises(ValueError):
            self.apply(manifest)

    def test_preflight_all_rows_before_first_write(self):
        ref2 = self.source.add_legacy(2)
        self.candidates.append({'ref': ref2, 'source': {k: item(2)[k] for k in ('authority', 'connection_id', 'native_id')}, 'destination': 'other'})
        self.dest.cards['card:initiative']['summary'] = merge_evidence(self.dest.cards['card:initiative'], [item(2)])
        manifest = self.preview()
        self.source.works[ref2]['updated_at'] = 'changed'
        with self.assertRaises(ValueError):
            self.apply(manifest)
        self.assertEqual(self.source.writes, [])

    def test_resume_after_archive_failure_and_board_fence_conflict(self):
        manifest = self.preview()
        original = self.source.card_archive

        def concurrent_change(ref, stamp, **fences):
            self.source.stamp += 1
            return original(ref, stamp, **fences)

        with patch.object(self.source, 'card_archive', side_effect=concurrent_change):
            with self.assertRaises(AnxError):
                self.apply(manifest)
        self.assertFalse(self.source.cards[self.ref].get('archived_at'))
        self.assertEqual(self.apply(manifest)['archived'], [self.ref])
        self.assertEqual(self.source.writes.count(('tombstone', self.ref)), 1)

    def test_evidence_removed_after_tombstone_prevents_archive(self):
        manifest = self.preview()
        original = self.source.work_patch

        def remove_evidence(ref, body):
            original(ref, body)
            self.dest.cards['card:initiative']['summary'] = ''

        with patch.object(self.source, 'work_patch', side_effect=remove_evidence):
            with self.assertRaises(ValueError):
                self.apply(manifest)
        self.assertEqual(self.source.writes, [('tombstone', self.ref)])
        self.assertFalse(self.source.cards[self.ref].get('archived_at'))

    def test_new_observation_after_final_readback_is_rejected_atomically(self):
        manifest = self.preview()
        original = self.source.card_archive

        def observe_before_archive(ref, stamp, **fences):
            self.source.works[ref]['latest_observation']['id'] = 'new-poll-same-phase'
            return original(ref, stamp, **fences)

        with patch.object(self.source, 'card_archive', side_effect=observe_before_archive):
            with self.assertRaises(AnxError):
                self.apply(manifest)
        self.assertEqual(self.source.writes, [('tombstone', self.ref)])
        self.assertFalse(self.source.cards[self.ref].get('archived_at'))

    def test_mapping_changes_after_tombstone_stop_archive(self):
        for which in ('source', 'destination'):
            with self.subTest(which=which):
                self.setUp()
                manifest = self.preview()
                original = self.source.work_patch

                def change_mapping(ref, body):
                    original(ref, body)
                    (self.source_mapping if which == 'source' else self.dest_mapping)['rules'] = []

                with patch.object(self.source, 'work_patch', side_effect=change_mapping):
                    with self.assertRaisesRegex(ValueError, 'published mapping differs'):
                        self.apply(manifest)
                self.assertEqual(self.source.writes, [('tombstone', self.ref)])
                self.assertEqual(self.dest.writes, [])
                self.assertFalse(self.source.cards[self.ref].get('archived_at'))

    def test_duplicate_candidates_rejected(self):
        self.candidates.append(copy.deepcopy(self.candidates[0]))
        with self.assertRaises(ValueError):
            self.preview()

    def test_reviewed_candidates_allow_missing_metadata_but_not_conflicting_route(self):
        self.source_mapping['rules'] = []
        self.assertEqual(self.preview()['counts']['archive_after_verification'], 1)
        self.source_mapping['rules'] = [{'id': 'local', 'initiative': 'card:initiative', 'match': {'project': 'p'}}]
        self.assertEqual(self.preview()['counts']['deferred'], 1)
        self.source_mapping['rules'] = [{'id': 'other', 'elsewhere': 'third', 'match': {'project': 'p'}}]
        self.assertEqual(self.preview()['counts']['deferred'], 1)


if __name__ == '__main__':
    unittest.main()
