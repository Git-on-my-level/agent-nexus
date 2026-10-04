"""Workspace-scoped routing and revision-fenced linked evidence; never creates cards."""
from __future__ import annotations

import hashlib
import json
import re
from collections import defaultdict
from urllib.parse import quote, urlsplit

from anx_client import AnxError
from project import plan_reads

BEGIN = '<!-- fleet-sync:evidence:v1 -->'
END = '<!-- /fleet-sync:evidence -->'
MATCH_FIELDS = {'authority', 'connection_id', 'project', 'labels', 'repo', 'title_pattern'}


def canonical(value):
    return json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(',', ':'))


def digest(value):
    return hashlib.sha256(canonical(value).encode()).hexdigest()


def identity(item):
    return canonical([item['authority'], item['connection_id'], item['native_id']])


def validate_mapping(mapping, workspace):
    if not isinstance(mapping, dict) or mapping.get('version') != 1:
        raise ValueError('mapping must be a version 1 JSON object')
    if mapping.get('workspace') != workspace.rstrip('/'):
        raise ValueError('mapping workspace must equal this config base_url (no cross-workspace routing)')
    if set(mapping) - {'version', 'workspace', 'rules', 'pins', 'proposal_threshold'}:
        raise ValueError('unknown mapping field')
    threshold = mapping.get('proposal_threshold', 5)
    if type(threshold) is not int or threshold < 2:
        raise ValueError('proposal_threshold must be an integer >= 2')
    ids, pins = set(), set()
    for kind in ('rules', 'pins'):
        rows = mapping.get(kind, [])
        if not isinstance(rows, list):
            raise ValueError(f'{kind} must be a list')
        for row in rows:
            if not isinstance(row, dict):
                raise ValueError(f'{kind} entry must be an object')
            allowed = {'id', 'initiative', 'match'} if kind == 'rules' else {'authority', 'connection_id', 'native_id', 'initiative'}
            if set(row) != allowed:
                raise ValueError(f'{kind} entry requires exactly {sorted(allowed)}')
            if not isinstance(row['initiative'], str) or not re.fullmatch(r'card:[A-Za-z0-9._-]+', row['initiative']):
                raise ValueError('initiative must be a workspace-local card:<handle>')
            if kind == 'pins':
                if any(not isinstance(row[k], str) or not row[k] for k in ('authority', 'connection_id', 'native_id')):
                    raise ValueError('pin requires full source identity')
                key = identity(row)
                if key in pins:
                    raise ValueError('duplicate item pin')
                pins.add(key)
                continue
            if not isinstance(row['id'], str) or not row['id'] or row['id'] in ids:
                raise ValueError('rule ids must be nonempty and unique')
            ids.add(row['id'])
            match = row['match']
            if not isinstance(match, dict) or not match or set(match) - MATCH_FIELDS:
                raise ValueError('rule requires nonempty supported match fields')
            for key, value in match.items():
                if key == 'labels':
                    if not isinstance(value, list) or not value or any(not isinstance(v, str) or not v for v in value):
                        raise ValueError('labels must be a nonempty string list (all must match)')
                elif not isinstance(value, str) or not value:
                    raise ValueError(f'{key} must be a nonempty string')
            if 'title_pattern' in match:
                try:
                    re.compile(match['title_pattern'])
                except re.error as exc:
                    raise ValueError(f'invalid title_pattern: {exc}') from exc
    return mapping


def route(item, mapping):
    for pin in mapping.get('pins', []):
        if identity(pin) == identity(item):
            return pin['initiative'], 'pin'
    for rule in mapping.get('rules', []):
        matched = True
        for field, value in rule['match'].items():
            if field == 'title_pattern':
                matched &= re.search(value, item.get('title', '')) is not None
            elif field == 'labels':
                matched &= set(value).issubset(item.get('labels', []))
            else:
                matched &= item.get(field) == value
        if matched:
            return rule['initiative'], 'rule:' + rule['id']
    return None, 'unmatched'


def source_items(reads, config, now):
    # Reuse source normalization only, with no known cards: absence never closes
    # an initiative, and terminal evidence is retained without registering cards.
    items = []
    raw = {}
    for read in reads:
        for row in read.get('items', []):
            raw[(read['name'], row.get('native_id'))] = row
    projection_config = {**config, 'boards': {'agent_work': '', 'pull_requests': '', 'ops_hygiene': ''}}
    for plan in plan_reads(reads, {}, projection_config, now=now):
        row = raw.get((plan['authority'], plan['native_id']), {})
        items.append({
            'authority': plan['authority'], 'connection_id': plan['connection_id'],
            'native_id': plan['native_id'], 'title': row.get('title') or plan['title'],
            'url': plan.get('url'), 'status': plan['facts']['phase'],
            'project': row.get('project'), 'labels': row.get('labels', []),
            'repo': row.get('repo'),
        })
    return sorted(items, key=identity)


def evidence_entries(summary):
    if BEGIN not in summary and END not in summary:
        return []
    if summary.count(BEGIN) != 1 or summary.count(END) != 1:
        raise ValueError('malformed fleet-sync evidence block; refusing to overwrite')
    start = summary.index(BEGIN) + len(BEGIN)
    end = summary.index(END)
    if end < start:
        raise ValueError('malformed fleet-sync evidence block')
    block = summary[start:end]
    try:
        payload = block.split('<!-- fleet-sync:data\n', 1)[1].split('\n-->', 1)[0]
        entries = json.loads(payload)
        if not isinstance(entries, list) or len({identity(e) for e in entries}) != len(entries):
            raise ValueError('invalid evidence entries')
        return entries
    except (IndexError, KeyError, TypeError, json.JSONDecodeError) as exc:
        raise ValueError('malformed fleet-sync evidence data') from exc


def merge_evidence(card, additions):
    summary = card.get('summary') or ''
    previous = evidence_entries(summary)
    merged = {identity(item): item for item in previous}
    for item in additions:
        key = identity(item)
        old = merged.get(key, {})
        # Keep immutable legacy card links even after a newer source observation.
        merged[key] = {**item, 'legacy_refs': sorted(set(old.get('legacy_refs', [])) | set(item.get('legacy_refs', [])))}
    entries = sorted(merged.values(), key=identity)
    if entries == previous:
        return summary
    # Escape HTML delimiters so source titles cannot inject our block markers.
    payload = json.dumps(entries, indent=2, ensure_ascii=False).replace('<', '\\u003c').replace('>', '\\u003e')
    links = []
    for entry in entries:
        title = markdown_text(entry.get('title') or entry['native_id'])
        url = entry.get('url') or ''
        if urlsplit(url).scheme in {'http', 'https'} and urlsplit(url).netloc:
            title = f'[{title}]({quote(url, safe=":/?&=%#@+;,")})'
        status = markdown_text(entry.get('status') or 'unknown')
        links.append(f'- {title} — {status}')
    visible = '\n'.join(links)
    block = f'{BEGIN}\n<details><summary>Linked source evidence ({len(entries)} items)</summary>\n\n{visible}\n\n<!-- fleet-sync:data\n{payload}\n-->\n</details>\n{END}'
    if BEGIN in summary:
        start, end = summary.index(BEGIN), summary.index(END) + len(END)
        return summary[:start] + block + summary[end:]
    return summary + '\n\n' + block


def markdown_text(value):
    value = ' '.join(str(value).split()).replace('&', '&amp;').replace('<', '&lt;').replace('>', '&gt;')
    return re.sub(r'([\\`*_[\]{}()#!|])', r'\\\1', value)


def revision_body(card, summary):
    if not card.get('head_revision_ref'):
        raise ValueError('card read missing head_revision_ref')
    return {'if_base_revision': card['head_revision_ref'], 'revision': {
        'title': card['title'], 'summary': summary,
        'definition_of_done': card.get('definition_of_done') or [],
    }}


def plan_ingestion(items, mapping, client):
    unique = {}
    for item in items:
        key = identity(item)
        if key in unique and unique[key] != item:
            raise ValueError('conflicting duplicate source identity')
        unique[key] = item
    items = sorted(unique.values(), key=identity)
    groups, unsorted = defaultdict(list), []
    for item in items:
        target, reason = route(item, mapping)
        entry = {**item, 'mapping': reason}
        if target:
            groups[target].append(entry)
        else:
            unsorted.append(entry)
    changes = []
    for target, entries in sorted(groups.items()):
        work = client.work_get(target)
        if (work.get('source') or {}).get('authority') != 'nexus':
            raise ValueError(f'{target}: initiative must be Nexus-owned')
        card = client.card_get(target)
        if card.get('archived_at') or card.get('trashed_at'):
            raise ValueError(f'{target}: initiative is not active')
        summary = merge_evidence(card, entries)
        changes.append({'initiative': target, 'action': 'revise' if summary != card.get('summary', '') else 'skip',
                        'items': entries, 'body': revision_body(card, summary)})
    clusters = defaultdict(list)
    for item in unsorted:
        # Source/host alone is not a coherent project cluster.
        key = ('project:' + item['project']) if item.get('project') else ('repo:' + item['repo']) if item.get('repo') else ('label:' + sorted(item['labels'])[0]) if item.get('labels') else None
        if key:
            clusters[(item['authority'], item['connection_id'], key)].append(identity(item))
    proposals = [{'cluster': list(key), 'count': len(set(members)), 'items': sorted(set(members)),
                  'action': 'suggest initiative; requires deliberate promotion'}
                 for key, members in sorted(clusters.items()) if len(set(members)) >= mapping.get('proposal_threshold', 5)]
    return {'initiatives': changes, 'unsorted': unsorted, 'proposals': proposals,
            'counts': {'items': len(items), 'mapped': sum(len(v) for v in groups.values()),
                       'unsorted': len(unsorted), 'new_cards': 0}}


def apply_ingestion(client, plan):
    summary = {'created': 0, 'revised': 0, 'skipped': 0, 'errors': []}
    for change in plan['initiatives']:
        if change['action'] == 'skip':
            summary['skipped'] += 1
            continue
        try:
            client.card_revise(change['initiative'], change['body'])
            summary['revised'] += 1
        except AnxError as exc:
            summary['errors'].append(f"{change['initiative']}: {exc.message}")
    return summary


def add_unsorted_panel(report, plan, reads):
    from report import _table, _ref
    items = plan['unsorted']
    rows = [{'cells': [i['title'], i['authority'], i['status'], i.get('url') or i['native_id']], 'source_ids': []} for i in items]
    proposals = plan['proposals']
    rows = [{'cells': ['Suggestion: ' + p['cluster'][2], str(p['count']) + ' items', 'review mapping', 'Create or select an initiative deliberately'], 'source_ids': []} for p in proposals] + rows
    incomplete = any(not r.get('ok') or not r.get('complete') for r in reads)
    if len(rows) > 200:
        omitted = len(rows) - 199
        rows = rows[:199] + [{'cells': [f'{omitted} further items / suggestions', '', 'not shown', 'Review the complete ingestion preview to map remaining groups'], 'source_ids': []}]
    panel = _table('fleet-unsorted', f"Unsorted ({len(items)}{'+' if incomplete else ''})", ['Item / suggestion', 'Source / count', 'Status', 'Source link'], rows, [], 'stale' if incomplete else 'current', report['generated_at'])
    report['panels'].append(panel)
    report['layout']['items'][0]['children'].append(_ref(panel['id']))
    # Schema caps tables at 200 rows. Always show the full count and explicitly
    # label omitted rows. The complete list and proposals remain in --plan.
