import copy
import unittest
from pathlib import Path
from unittest.mock import Mock, patch

from test_initiatives import Client, NOW, READS, WS, item, mapping
from test_report import _reads, _pull
from fleet_sync import publish, validate_report, validator_script
from initiatives import validate_mapping, plan_ingestion, apply_ingestion, add_unsorted_panel
from report import build_report, fit_report, report_text, REPORT_BUDGET


class RoutingTests(unittest.TestCase):
    def test_elsewhere_is_excluded_from_local_work_and_keeps_precedence(self):
        items = [item(i, repo='org/omi') for i in range(156)]
        items += [item(200, repo='org/anx'), item(201, project='unmapped')]
        pin = {k: items[0][k] for k in ('authority', 'connection_id', 'native_id')}
        m = mapping(pins=[{**pin, 'initiative': 'card:initiative'}], rules=[
            {'id': 'omi', 'elsewhere': 'omi', 'match': {'repo': 'org/omi', 'authority': 'multica'}},
            {'id': 'anx', 'elsewhere': 'anx', 'match': {'repo': 'org/anx'}},
            *mapping()['rules'],
        ])
        validate_mapping(m, WS)
        client = Client()
        plan = plan_ingestion(items, m, client)
        self.assertEqual(plan['counts'], {'items': 158, 'mapped': 1, 'unsorted': 1, 'elsewhere': 156, 'new_cards': 0})
        self.assertEqual(plan['proposals'], [])
        self.assertEqual(len(plan['elsewhere']['omi']), 155)
        apply_ingestion(client, plan)
        self.assertEqual(client.writes, [('revise', 'card:initiative')])
        # An elsewhere pin overrides a local rule without even reading a target.
        m = mapping(pins=[{**pin, 'elsewhere': 'omi'}])
        validate_mapping(m, WS)
        untouched = Mock()
        pinned = plan_ingestion([items[0]], m, untouched)
        apply_ingestion(untouched, pinned)
        self.assertEqual(untouched.mock_calls, [])
        report = build_report(READS, generated_at='2026-10-05T00:00:00Z', now=NOW, hosts=[])
        add_unsorted_panel(report, plan, [{**READS[0], 'complete': False}])
        panel = next(p for p in report['panels'] if p['id'] == 'fleet-elsewhere')
        self.assertEqual([r['cells'][0] for r in panel['data']['rows']],
                         ['1+ items belong to anx', '155+ items belong to omi'])
        self.assertEqual(panel['freshness'], 'stale')
        self.assertTrue(validate_report(report)[0])

    def test_invalid_elsewhere_destinations_fail_closed(self):
        for destination in ('', ' ', '../omi', 'https://omi.test', 'card:omi', 'Omi', 'a' * 64, None, 7, [], {}):
            for kind in ('rules', 'pins'):
                row = {'elsewhere': destination}
                row.update({'id': 'x', 'match': {'repo': 'org/omi'}} if kind == 'rules' else
                           {k: item()[k] for k in ('authority', 'connection_id', 'native_id')})
                with self.subTest(destination=destination, kind=kind), self.assertRaises(ValueError):
                    validate_mapping(mapping(**{kind: [row]}), WS)
        for row in ({'id': 'x', 'match': {'repo': 'org/omi'}},
                    {'id': 'x', 'match': {'repo': 'org/omi'}, 'initiative': 'card:x', 'elsewhere': 'omi'}):
            with self.assertRaises(ValueError):
                validate_mapping(mapping(rules=[row]), WS)


class ReportBoundsTests(unittest.TestCase):
    def test_oversized_chart_becomes_explicit_omission_and_keeps_counts(self):
        report = build_report(READS, generated_at='2026-10-05T00:00:00Z', now=NOW, hosts=[])
        chart = next(p for p in report['panels'] if p['type'] == 'chart')
        chart['data']['option']['xAxis']['data'] = ['large category ' * 5000] * 3
        counts = copy.deepcopy(next(p for p in report['panels'] if p['type'] == 'metric-strip'))
        fit_report(report)
        self.assertLessEqual(len(report_text(report).encode()), REPORT_BUDGET)
        self.assertEqual(chart['type'], 'callout')
        self.assertEqual(chart['data']['label'], 'Detail omitted')
        self.assertEqual(next(p for p in report['panels'] if p['type'] == 'metric-strip'), counts)
        self.assertTrue(validate_report(report)[0])

    def test_oversized_summary_fails_closed(self):
        report = build_report(READS, generated_at='2026-10-05T00:00:00Z', now=NOW, hosts=[])
        report['summary'] = 'x' * (128 * 1024)
        with self.assertRaisesRegex(ValueError, 'report budget'):
            fit_report(report)

    def test_ten_times_production_volume_fits_exact_published_bytes(self):
        reads = _reads()
        reads[0]['items'] = [
            {'native_id': str(i), 'identifier': f'SCA-{i}', 'title': '界🧭' * 200,
             'status': 'in_review', 'host': 'host-a', 'updated_at': '2026-10-01T00:00:00Z',
             'url': 'https://source.test/' + 'x' * 1000 + str(i)} for i in range(2400)]
        reads[1]['items'] = [
            {**_pull(f'org/repo-{i % 20}#{i}', 'red', '2026-10-03T00:00:00Z'),
             'title': '界🧭' * 200} for i in range(2470)]
        items = [item(i, project=f'p-{i % 20}', title='界🧭' * 500,
                      url='https://source.test/' + 'x' * 2000) for i in range(4870)]
        plan = plan_ingestion(items, mapping(rules=[]), Client())
        original = copy.deepcopy(plan)
        report = build_report(reads, generated_at='2026-10-05T00:00:00Z', now=NOW, hosts=[])
        self.assertGreater(len(report_text(report).encode()), 128 * 1024)
        add_unsorted_panel(report, plan, reads)
        content = report_text(report)
        self.assertLessEqual(len(content.encode()), REPORT_BUDGET)
        self.assertEqual(plan, original)  # Preview keeps every full item and proposal.
        panel = next(p for p in report['panels'] if p['id'] == 'fleet-unsorted')
        self.assertEqual(panel['title'], 'Unsorted (4870+)')
        self.assertEqual(len(panel['data']['rows']), 16)
        self.assertIn('4870 Unsorted items; 20 suggestions', panel['data']['rows'][-1]['cells'])
        self.assertTrue(any('size limit' in p['title'] for p in report['panels']))
        valid, errors = validate_report(report)
        self.assertTrue(valid, errors)

        class Publisher:
            saved = None

            def docs_list(self):
                return []

            def docs_create(self, topic, title, path):
                self.saved = Path(path).read_bytes()
                return {'document': {'ref': 'doc:fleet-dashboard'}}

            def docs_content(self, ref):
                return self.saved.decode('utf-8')

        client = Publisher()
        result = publish(client, {'topic': 'fleet'}, {}, report, 'node', validator_script())
        self.assertTrue(result['readback_valid'])
        self.assertEqual(client.saved, content.encode('utf-8'))
        with patch('fleet_sync.validate_text', return_value=(True, [])) as validator:
            validate_report(report)
            self.assertEqual(validator.call_args.args[0].encode(), client.saved)

    def test_unsorted_clusters_are_ranked_and_full_values_stay_in_plan(self):
        items = [item(i, project='small' if i < 5 else 'large') for i in range(30)]
        plan = plan_ingestion(items, mapping(rules=[]), Client())
        report = build_report(READS, generated_at='2026-10-05T00:00:00Z', now=NOW, hosts=[])
        add_unsorted_panel(report, plan, READS)
        panel = next(p for p in report['panels'] if p['id'] == 'fleet-unsorted')
        self.assertEqual(panel['data']['rows'][0]['cells'][:2], ['Suggestion: project:large', '25 items'])
        self.assertEqual(len(plan['unsorted']), 30)
