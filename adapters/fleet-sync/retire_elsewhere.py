"""Retire reviewed legacy copies only after read-only destination verification."""
from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path
from urllib.parse import quote, urlsplit

from anx_client import AnxClient, AnxError
from initiatives import digest, evidence_entries, identity, route, validate_mapping
from migrate import legacy_item, source_fence


def endpoint(config):
    url = config['base_url'].rstrip('/')
    parsed = urlsplit(url)
    if parsed.scheme not in ('https', 'http') or not parsed.netloc or parsed.username or parsed.password or parsed.query or parsed.fragment:
        raise ValueError('workspace must be an absolute HTTP(S) URL without credentials, query or fragment')
    return {'base_url': url, 'mapping_doc': config['mapping_doc']}


def client_for(config):
    return AnxClient(config.get('anx_binary') or 'anx', endpoint(config)['base_url'], config['agent'])


def published(client, config):
    return validate_mapping(json.loads(client.docs_content(config['mapping_doc'])), endpoint(config)['base_url'])


def source_allows(item, mapping, destination):
    # Reviewed explicit candidates can supply ownership missing from old
    # observations, but must never override a conflicting published route.
    return route(item, mapping) in ((None, 'unmatched'), (None, 'elsewhere:' + destination))


def verify_evidence(client, row):
    """Destination clients are used exclusively for reads, with their own auth."""
    work = client.work_get(row['initiative'])
    card = client.card_get(row['initiative'])
    if (work.get('source') or {}).get('authority') != 'nexus' or card.get('archived_at') or card.get('trashed_at'):
        raise ValueError('destination must be an active Nexus-owned initiative')
    if not card.get('head_revision_ref'):
        raise ValueError('destination has no revision fence')
    entry = next((e for e in evidence_entries(card.get('summary') or '') if identity(e) == identity(row['item'])), None)
    if entry is None:
        raise ValueError('destination initiative lacks this full source identity as evidence')
    return {'head_revision_ref': card['head_revision_ref'], 'entry_digest': digest(entry)}


def inventory(client, config, candidates, destinations):
    source_mapping = published(client, config)
    source_mapping_ref = client.docs_ref(config['mapping_doc'])
    destination_records = {}
    for label, dest_config in config['destinations'].items():
        record = endpoint(dest_config)
        if record['base_url'] == endpoint(config)['base_url']:
            raise ValueError('destination must differ from source workspace')
        try:
            record['mapping'] = published(destinations[label], dest_config)
        except (AnxError, ValueError) as exc:
            record['error'] = str(exc)
        destination_records[label] = record
    refs, identities = set(), set()
    for candidate in candidates:
        if candidate['ref'] in refs or identity(candidate['source']) in identities:
            raise ValueError('duplicate candidate ref/source identity')
        if candidate['destination'] not in destination_records:
            raise ValueError('candidate destination missing from config')
        refs.add(candidate['ref'])
        identities.add(identity(candidate['source']))
    # List current active cards, never silently treat an absent historical copy
    # as verified. Source inventory failure aborts instead of using stale data.
    works = {}
    for authority in sorted({c['source']['authority'] for c in candidates}):
        works.update({w['ref']: w for w in client.work_list(authority)})
    rows = []
    for candidate in candidates:
        row = {'ref': candidate['ref'], 'source': candidate['source'],
               'destination': candidate['destination'], 'eligible': False}
        try:
            work = works.get(row['ref'])
            item = legacy_item(work) if work else None
            if item is None or identity(item) != identity(row['source']):
                raise ValueError('active source card missing or fleet-sync ownership/identity unproven')
            row.update(item=item, source_fence=source_fence(work))
            if not source_allows(item, source_mapping, row['destination']):
                raise ValueError('source published mapping conflicts with reviewed destination')
            dest = destination_records[row['destination']]
            if 'error' in dest:
                raise ValueError('destination unavailable: ' + dest['error'])
            target, reason = route(item, dest['mapping'])
            if not target:
                raise ValueError('destination mapping has no local initiative for source item')
            row.update(initiative=target, mapping=reason,
                       initiative_url=dest['base_url'] + '/tasks/' + quote(target, safe=''))
            row['evidence'] = verify_evidence(destinations[row['destination']], row)
            row['eligible'] = True
        except (AnxError, ValueError) as exc:
            row['deferred_reason'] = str(exc)
        rows.append(row)
    manifest = {'version': 1, 'mode': 'retire-elsewhere', 'source': endpoint(config),
                'mapping': source_mapping, 'mapping_ref': source_mapping_ref, 'destinations': destination_records,
                'cards': sorted(rows, key=lambda r: r['ref']),
                'counts': {'candidates': len(rows), 'archive_after_verification': sum(r['eligible'] for r in rows),
                           'deferred': sum(not r['eligible'] for r in rows), 'new_cards': 0}}
    manifest['digest'] = digest(manifest)
    return manifest


def validate_manifest(manifest, config, approval):
    if manifest.get('version') != 1 or manifest.get('mode') != 'retire-elsewhere' or manifest.get('source') != endpoint(config):
        raise ValueError('manifest mode/version/source mismatch')
    if digest({k: v for k, v in manifest.items() if k != 'digest'}) != manifest.get('digest') or approval != manifest.get('digest'):
        raise ValueError('--approve-digest must match the reviewed, unmodified manifest')
    validate_mapping(manifest['mapping'], endpoint(config)['base_url'])
    refs, identities = set(), set()
    for row in manifest['cards']:
        if row['ref'] in refs or identity(row['source']) in identities:
            raise ValueError('duplicate candidate ref/source identity')
        refs.add(row['ref'])
        identities.add(identity(row['source']))
        if not row['eligible']:
            continue
        dest = manifest['destinations'][row['destination']]
        if endpoint(config['destinations'][row['destination']]) != endpoint(dest) or dest['base_url'] == endpoint(config)['base_url']:
            raise ValueError('destination workspace/mapping document changed')
        validate_mapping(dest['mapping'], dest['base_url'])
        if (identity(row['item']) != identity(row['source']) or row['item'].get('legacy_refs') != [row['ref']]
                or not source_allows(row['item'], manifest['mapping'], row['destination'])
                or route(row['item'], dest['mapping']) != (row['initiative'], row['mapping'])
                or row['initiative_url'] != dest['base_url'] + '/tasks/' + quote(row['initiative'], safe='')):
            raise ValueError('manifest routing/provenance mismatch')


def tombstone(row, manifest_digest, mapping_ref):
    # Relations resolve locally. Anchor the audit to the source mapping doc;
    # preserve the remote link as metadata rather than an invalid local ref.
    return {'kind': 'related', 'ref': mapping_ref,
            'destination_url': row['initiative_url'],
            'fleet_sync_migration': manifest_digest,
            'note': f"Source evidence verified in [owning initiative]({row['initiative_url']}); archived legacy copy retained."}


def apply_retirement(client, config, manifest, approval, destinations):
    validate_manifest(manifest, config, approval)
    if published(client, config) != manifest['mapping']:
        raise ValueError('source published mapping differs from reviewed manifest')
    if client.docs_ref(config['mapping_doc']) != manifest['mapping_ref']:
        raise ValueError('source mapping document ref changed')
    candidates = [r for r in manifest['cards'] if r['eligible']]
    for label in sorted({r['destination'] for r in candidates}):
        if published(destinations[label], config['destinations'][label]) != manifest['destinations'][label]['mapping']:
            raise ValueError('destination published mapping differs from reviewed manifest')
    active, done = [], []
    # Preflight the entire eligible batch before the first source mutation.
    for row in candidates:
        verify_evidence(destinations[row['destination']], row)
        card, work = client.card_get(row['ref']), client.work_get(row['ref'])
        relation = tombstone(row, manifest['digest'], manifest['mapping_ref'])
        if card.get('trashed_at'):
            raise ValueError('source card trashed since preview')
        if card.get('archived_at'):
            if relation not in (work.get('relations') or []):
                raise ValueError('source card archived outside this migration')
            done.append(row['ref'])
        elif source_fence(work) != row['source_fence']:
            raise ValueError('source changed since preview; regenerate manifest')
        else:
            active.append(row)
    archived = []
    for row in active:
        card = client.card_get(row['ref'])
        board = client.board_get(card['board_ref'])
        # Verify again immediately before writing. Cross-workspace operations
        # cannot be atomic; unrelated new destination evidence is harmless.
        verify_evidence(destinations[row['destination']], row)
        work = client.work_get(row['ref'])
        if source_fence(work) != row['source_fence']:
            raise ValueError('source changed during retirement; stopping')
        relation = tombstone(row, manifest['digest'], manifest['mapping_ref'])
        if relation not in (work.get('relations') or []):
            client.work_patch(row['ref'], {'if_version': work['version'],
                                         'patch': {'relations': [*(work.get('relations') or []), relation]}})
        readback = client.work_get(row['ref'])
        if relation not in (readback.get('relations') or []) or source_fence(readback) != row['source_fence']:
            raise ValueError('tombstone readback/source changed; stopping')
        verify_evidence(destinations[row['destination']], row)
        client.card_archive(row['ref'], board['updated_at'])
        archived.append(row['ref'])
    return {'archived': archived, 'already_archived': done, 'deferred': sum(not r['eligible'] for r in manifest['cards']), 'created': 0}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--config', required=True)
    parser.add_argument('--candidates', help='reviewed JSON array of ref, source identity and destination label')
    parser.add_argument('--output')
    parser.add_argument('--apply')
    parser.add_argument('--approve-digest')
    args = parser.parse_args(argv)
    try:
        config = json.loads(Path(args.config).expanduser().read_text(encoding='utf-8'))
        client = client_for(config)
        destinations = {label: client_for(dest) for label, dest in config['destinations'].items()}
        if args.apply:
            if args.candidates or args.output:
                raise ValueError('--apply cannot use --candidates or --output')
            manifest = json.loads(Path(args.apply).read_text(encoding='utf-8'))
            result = apply_retirement(client, config, manifest, args.approve_digest, destinations)
        else:
            if args.approve_digest or not args.candidates:
                raise ValueError('dry run requires --candidates; --approve-digest requires --apply')
            candidates = json.loads(Path(args.candidates).read_text(encoding='utf-8'))
            result = inventory(client, config, candidates, destinations)
        output = json.dumps(result, indent=2, ensure_ascii=False) + '\n'
        if args.output:
            Path(args.output).write_text(output, encoding='utf-8')
            print(json.dumps({'output': args.output, 'digest': result['digest'], 'counts': result['counts']}))
        else:
            print(output, end='')
        return 0
    except (AnxError, ValueError, OSError, KeyError, TypeError) as exc:
        print(str(exc), file=sys.stderr)
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
