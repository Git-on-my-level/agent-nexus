"""Reviewable legacy-card migration. Default is a read-only manifest, never apply."""
from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

from anx_client import AnxClient, AnxError
from initiatives import (digest, identity, route, validate_mapping, plan_ingestion,
                         apply_ingestion, evidence_entries)

AUTHORITIES = ('multica', 'github', 'hermes-cron', 'agentctl', 'fleetctl', 'prometheus')


def legacy_item(work):
    source = work.get('source') or {}
    observation = work.get('latest_observation') or {}
    authority = source.get('authority')
    if authority not in AUTHORITIES or observation.get('reader_id') != f'fleet-sync/{authority}':
        return None
    if not source.get('connection_id') or not source.get('native_id'):
        return None
    facts = observation.get('facts') or {}
    return {'authority': authority, 'connection_id': source['connection_id'],
            'native_id': source['native_id'], 'title': work['title'],
            'url': source.get('url'), 'status': work.get('phase') or 'unknown',
            'project': facts.get('project'), 'labels': facts.get('labels') or [],
            'repo': facts.get('repo'), 'legacy_refs': [work['ref']]}


def observation_id(work):
    value = (work.get('latest_observation') or {}).get('id')
    if not isinstance(value, str) or not value:
        raise ValueError('source observation has no id; cannot archive safely')
    return value


def source_fence(work):
    # Observation changes can leave the canonical card updated_at unchanged.
    return digest({key: work.get(key) for key in ('source', 'latest_observation', 'head_revision_ref', 'updated_at', 'phase')})


def inventory(client, mapping, workspace):
    cards, skipped = [], []
    for authority in AUTHORITIES:
        for work in client.work_list(authority):
            item = legacy_item(work)
            if item is None:
                skipped.append({'ref': work.get('ref'), 'reason': 'not proven fleet-sync-owned'})
                continue
            target, reason = route(item, mapping)
            cards.append({'ref': work['ref'], 'source_fence': source_fence(work),
                          'initiative': target, 'mapping': reason, 'item': item})
    refs = {c['ref'] for c in cards}
    if len(refs) != len(cards):
        raise ValueError('duplicate legacy refs in inventory')
    if any(c['initiative'] in refs for c in cards):
        raise ValueError('legacy detail cards cannot be initiative targets')
    plan = plan_ingestion([c['item'] for c in cards], mapping, client)
    manifest = {'version': 1, 'workspace': workspace.rstrip('/'), 'mapping': mapping,
                'cards': sorted(cards, key=lambda c: c['ref']), 'skipped': skipped,
                'preview': plan, 'counts': {'legacy': len(cards), 'archive_after_fold': sum(bool(c['initiative']) for c in cards),
                                           'deferred_unmatched': sum(not c['initiative'] for c in cards), 'new_cards': 0}}
    manifest['digest'] = digest(manifest)
    return manifest


def validate_manifest(manifest, workspace, approval):
    content = {k: v for k, v in manifest.items() if k != 'digest'}
    if manifest.get('version') != 1 or manifest.get('workspace') != workspace.rstrip('/'):
        raise ValueError('manifest version/workspace mismatch')
    if digest(content) != manifest.get('digest') or approval != manifest.get('digest'):
        raise ValueError('--approve-digest must match the reviewed, unmodified manifest')
    validate_mapping(manifest['mapping'], workspace)
    for row in manifest['cards']:
        target, reason = route(row['item'], manifest['mapping'])
        if target != row['initiative'] or reason != row['mapping'] or row['item'].get('legacy_refs') != [row['ref']]:
            raise ValueError('manifest routing/provenance mismatch')


def apply_migration(client, manifest, workspace, approval):
    validate_manifest(manifest, workspace, approval)
    candidates = [row for row in manifest['cards'] if row['initiative']]
    # Preflight the whole batch before the first mutation. Retry can continue
    # from our exact tombstone; unrelated source/card edits demand a new plan.
    active, done = [], []
    for row in candidates:
        card = client.card_get(row['ref'])
        tombstone = tombstone_relation(row, manifest['digest'])
        work = client.work_get(row['ref'])
        if card.get('trashed_at'):
            raise ValueError(f"{row['ref']}: trashed since preview")
        migrated = tombstone in (work.get('relations') or [])
        if card.get('archived_at'):
            if not migrated:
                raise ValueError(f"{row['ref']}: archived outside this migration")
            done.append(row['ref'])
            continue
        if source_fence(work) != row['source_fence']:
            raise ValueError(f"{row['ref']}: changed since preview; regenerate manifest")
        active.append((row, card, migrated))
    plan = plan_ingestion([row['item'] for row, _, _ in active], manifest['mapping'], client)
    summary = apply_ingestion(client, plan)
    if summary['errors']:
        raise ValueError('fold failed; no cards archived: ' + '; '.join(summary['errors']))
    # Read back durable evidence before touching any source card.
    for change in plan['initiatives']:
        entries = evidence_entries(client.card_get(change['initiative']).get('summary') or '')
        stored = {identity(e): e for e in entries}
        for item in change['items']:
            if not set(item['legacy_refs']).issubset(stored.get(identity(item), {}).get('legacy_refs', [])):
                raise ValueError('fold readback missing legacy link; no cards archived')
    archived = []
    for row, _, migrated in active:
        # Fetch the board fence BEFORE checking the card/source. A concurrent
        # canonical mutation after this read invalidates archive's board fence.
        card = client.card_get(row['ref'])
        board = client.board_get(card['board_ref'])
        card = client.card_get(row['ref'])
        work = client.work_get(row['ref'])
        if source_fence(work) != row['source_fence']:
            raise ValueError(f"{row['ref']}: changed during migration; stopping")
        relation = tombstone_relation(row, manifest['digest'])
        if relation not in (work.get('relations') or []):
            client.work_patch(row['ref'], {'if_version': work['version'],
                                          'patch': {'relations': [*(work.get('relations') or []), relation]}})
        readback = client.work_get(row['ref'])
        if relation not in (readback.get('relations') or []) or source_fence(readback) != row['source_fence']:
            raise ValueError(f"{row['ref']}: tombstone readback/source changed; stopping")
        client.card_archive(row['ref'], board['updated_at'],
                            observation_id=observation_id(readback), work_version=readback['version'])
        archived.append(row['ref'])
    return {'archived': archived, 'already_archived': done,
            'deferred_unmatched': manifest['counts']['deferred_unmatched'], 'created': 0}


def tombstone_relation(row, manifest_digest):
    # Work annotations remain writable for source-backed cards; their body and
    # phase do not. Unknown relation metadata is preserved by the contract.
    return {'kind': 'related', 'ref': row['initiative'],
            'fleet_sync_migration': manifest_digest,
            'note': 'Folded into initiative; archived detail retained, source remains authoritative.'}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--config', required=True)
    parser.add_argument('--mapping-file', help='review draft mapping instead of mapping_doc; dry-run only')
    parser.add_argument('--output', help='write dry-run manifest to this file')
    parser.add_argument('--apply', metavar='MANIFEST')
    parser.add_argument('--approve-digest')
    args = parser.parse_args(argv)
    try:
        config = json.loads(Path(args.config).expanduser().read_text(encoding='utf-8'))
        client = AnxClient(config.get('anx_binary') or 'anx', config['base_url'], config['agent'])
        if args.apply:
            if args.mapping_file or args.output:
                raise ValueError('--apply uses only the reviewed manifest; no --mapping-file or --output')
            manifest = json.loads(Path(args.apply).read_text(encoding='utf-8'))
            # Rules must now be published in this workspace, matching the review.
            published = validate_mapping(json.loads(client.docs_content(config['mapping_doc'])), config['base_url'])
            if published != manifest['mapping']:
                raise ValueError('published mapping differs from reviewed manifest')
            result = apply_migration(client, manifest, config['base_url'], args.approve_digest)
        else:
            if args.approve_digest:
                raise ValueError('--approve-digest requires --apply')
            text = Path(args.mapping_file).read_text(encoding='utf-8') if args.mapping_file else client.docs_content(config['mapping_doc'])
            mapping = validate_mapping(json.loads(text), config['base_url'])
            result = inventory(client, mapping, config['base_url'])
        output = json.dumps(result, indent=2, ensure_ascii=False) + '\n'
        if args.output:
            Path(args.output).write_text(output, encoding='utf-8')
            print(json.dumps({'output': args.output, 'digest': result['digest'], 'counts': result['counts']}))
        else:
            print(output, end='')
        return 0
    except (AnxError, ValueError, OSError, KeyError) as exc:
        print(str(exc), file=sys.stderr)
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
