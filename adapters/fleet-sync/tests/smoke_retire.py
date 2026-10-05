"""Real two-workspace retirement smoke; creates and removes isolated local cores."""
import argparse
from contextlib import contextmanager
from datetime import datetime, timedelta, timezone
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import tempfile
import time
import urllib.request

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from anx_client import AnxClient
from initiatives import apply_ingestion, plan_ingestion
from migrate import legacy_item
from readers.run import Runner
from retire_elsewhere import inventory, apply_retirement


@contextmanager
def workspace(core_binary, cli_binary, scratch):
    root = Path(__file__).resolve().parents[3]
    (scratch / 'workspace').mkdir(parents=True)
    (scratch / 'workspace' / '.anx-dev-insecure-auth').touch()
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        port = sock.getsockname()[1]
    base = f'http://127.0.0.1:{port}'
    env = {**os.environ, 'ANX_BOOTSTRAP_TOKEN': 'local-smoke-bootstrap',
           'ANX_ALLOW_PASSKEY_DEV_BYPASS': '1', 'ANX_HOSTED_DEV_MODE': '1', 'ANX_ENABLE_DEV_ACTOR_MODE': '1'}
    with (scratch / 'core.log').open('w+') as log:
        process = subprocess.Popen([core_binary, '--listen-addr', f'127.0.0.1:{port}',
                                    '--workspace-root', str(scratch / 'workspace'),
                                    '--schema-path', str(root / 'contracts/anx-schema.yaml')],
                                   env=env, stdout=log, stderr=log)
        try:
            deadline = time.monotonic() + 30
            while True:
                try:
                    urllib.request.urlopen(base + '/readyz', timeout=1).close()
                    break
                except OSError:
                    if time.monotonic() > deadline or process.poll() is not None:
                        log.seek(0)
                        raise RuntimeError(log.read())
                    time.sleep(.1)

            def post(path, body, token=None):
                headers = {'Content-Type': 'application/json'}
                if token:
                    headers['Authorization'] = 'Bearer ' + token
                request = urllib.request.Request(base + path, data=json.dumps(body).encode(), headers=headers)
                with urllib.request.urlopen(request, timeout=10) as response:
                    return json.load(response)

            human = post('/auth/passkey/dev/register', {'display_name': 'Smoke operator', 'bootstrap_token': 'local-smoke-bootstrap'})
            grant = post('/auth/hosts/enrollment-tokens', {'label': 'Smoke host', 'expires_at': (datetime.now(timezone.utc) + timedelta(minutes=20)).isoformat()}, human['tokens']['access_token'])
            runner = Runner()

            def isolated(argv, **kwargs):
                return runner([argv[0], '--config-dir', str(scratch / 'config'), *argv[1:]], **kwargs)

            client = AnxClient(cli_binary, base, 'fleet-smoke', runner=isolated)
            client._call(['host', 'enroll', '--name', 'smoke-host', '--token', grant['token']], timeout=30)
            topic = client._call(['topics', 'create', '--title', 'Fleet test', '--summary', 'Smoke context'], timeout=30)['topic']['ref']
            board = client._call(['boards', 'create', '--topic', topic, '--title', 'Initiatives', '--summary', 'Smoke outcomes'], timeout=30)['board']['ref']
            yield client, base, topic, board
        finally:
            process.terminate()
            process.wait(timeout=10)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--core', required=True)
    parser.add_argument('--cli', required=True)
    args = parser.parse_args()
    core, cli = str(Path(args.core).resolve()), str(Path(args.cli).resolve())
    with tempfile.TemporaryDirectory(prefix='fleet-retire-smoke-') as directory:
        root = Path(directory)
        with workspace(core, cli, root / 'source') as (source, src_url, src_topic, src_board), workspace(core, cli, root / 'destination') as (dest, dst_url, dst_topic, dst_board):
            initiative = dest._call(['cards', 'create', '--board', dst_board, '--title', 'Owning outcome', '--body', 'Human context'], timeout=30)['card']['ref']
            legacy = source._call_body(['work', 'create'], {'board_ref': src_board, 'title': 'Old source copy', 'summary': 'Original source body', 'source': {'authority': 'multica', 'connection_id': 'test', 'native_id': 'old'}}, timeout=30)['work']['ref']
            source._call_body(['work', 'observations', 'submit', legacy], {'observation': {'idempotency_key': 'fixture', 'reader_id': 'fleet-sync/multica', 'reader_revision': '1', 'observed_at': datetime.now(timezone.utc).isoformat(), 'status': 'reported', 'facts': {'phase': 'ready'}, 'evidence': []}}, timeout=30)
            src_mapping = {'version': 1, 'workspace': src_url, 'pins': [], 'rules': [{'id': 'owner', 'elsewhere': 'other', 'match': {'authority': 'multica'}}]}
            dst_mapping = {'version': 1, 'workspace': dst_url, 'pins': [], 'rules': [{'id': 'owner', 'initiative': initiative, 'match': {'authority': 'multica'}}]}
            for client, topic, mapping, name in ((source, src_topic, src_mapping, 'source'), (dest, dst_topic, dst_mapping, 'destination')):
                path = root / (name + '.json')
                path.write_text(json.dumps(mapping))
                client.docs_create(topic, 'Fleet sync mapping', str(path))
            config = {'base_url': src_url, 'mapping_doc': 'doc:fleet-sync-mapping', 'agent': 'fleet-smoke', 'destinations': {'other': {'base_url': dst_url, 'mapping_doc': 'doc:fleet-sync-mapping', 'agent': 'fleet-smoke'}}}
            item = legacy_item(source.work_get(legacy))
            candidates = [{'ref': legacy, 'source': {k: item[k] for k in ('authority', 'connection_id', 'native_id')}, 'destination': 'other'}]
            clients = {'other': dest}
            missing = inventory(source, config, candidates, clients)
            assert missing['counts']['deferred'] == 1, missing
            # Evidence is deliberately ingested using only the destination's
            # enrolled client; retirement itself never performs this step.
            assert not apply_ingestion(dest, plan_ingestion([item], dst_mapping, dest))['errors']
            before = dest.card_get(initiative)
            manifest = inventory(source, config, candidates, clients)
            assert manifest['counts']['archive_after_verification'] == 1, manifest
            result = apply_retirement(source, config, manifest, manifest['digest'], clients)
            assert result['archived'] == [legacy], result
            assert source.card_get(legacy)['summary'] == 'Original source body'
            relation = source.work_get(legacy)['relations'][0]
            assert relation['ref'] == 'document:fleet-sync-mapping', relation
            assert relation['destination_url'] == dst_url + '/tasks/' + initiative.replace(':', '%3A'), relation
            assert apply_retirement(source, config, manifest, manifest['digest'], clients)['already_archived'] == [legacy]
            assert dest.card_get(initiative) == before, 'retirement modified destination'
            print('PASS: two isolated cores and separate credentials; missing evidence deferred; URL tombstone roundtrip, source-only archive, body preservation, resumable replay, destination unchanged')


if __name__ == '__main__':
    main()
