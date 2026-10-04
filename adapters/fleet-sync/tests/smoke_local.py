"""Isolated real CLI/core smoke; --core and --cli require built binaries."""
import argparse
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import tempfile
import time
import urllib.request
import urllib.error
from datetime import datetime, timedelta, timezone

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from anx_client import AnxClient
from readers.run import Runner
from initiatives import plan_ingestion, apply_ingestion, evidence_entries
from migrate import inventory, apply_migration


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--core', required=True)
    parser.add_argument('--cli', required=True)
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[3]
    with tempfile.TemporaryDirectory(prefix='fleet-sync-smoke-') as directory:
        scratch = Path(directory)
        workspace = scratch / 'workspace'
        workspace.mkdir()
        (workspace / '.anx-dev-insecure-auth').touch()
        with socket.socket() as sock:
            sock.bind(('127.0.0.1', 0))
            port = sock.getsockname()[1]
        base = f'http://127.0.0.1:{port}'
        env = {**os.environ, 'ANX_BOOTSTRAP_TOKEN': 'local-smoke-bootstrap',
               'ANX_ALLOW_PASSKEY_DEV_BYPASS': '1', 'ANX_HOSTED_DEV_MODE': '1',
               'ANX_ENABLE_DEV_ACTOR_MODE': '1'}
        with (scratch / 'core.log').open('w+') as log:
            core = subprocess.Popen([str(Path(args.core).resolve()), '--listen-addr', f'127.0.0.1:{port}',
                                     '--workspace-root', str(workspace), '--schema-path', str(root/'contracts/anx-schema.yaml')],
                                    env=env, stdout=log, stderr=log)
            try:
                deadline = time.monotonic() + 30
                while True:
                    try:
                        urllib.request.urlopen(base+'/readyz', timeout=1).close()
                        break
                    except OSError:
                        if time.monotonic() > deadline or core.poll() is not None:
                            log.seek(0)
                            raise RuntimeError(log.read())
                        time.sleep(.1)

                def post(path, body, token=None):
                    headers = {'Content-Type': 'application/json'}
                    if token:
                        headers['Authorization'] = 'Bearer '+token
                    req = urllib.request.Request(base+path, data=json.dumps(body).encode(), headers=headers)
                    try:
                        with urllib.request.urlopen(req, timeout=10) as response:
                            return json.load(response)
                    except urllib.error.HTTPError as exc:
                        raise RuntimeError(exc.read().decode()) from exc

                human = post('/auth/passkey/dev/register', {'display_name':'Smoke operator', 'bootstrap_token':'local-smoke-bootstrap'})
                enrolled = post('/auth/hosts/enrollment-tokens', {'label':'Smoke host', 'expires_at':(datetime.now(timezone.utc)+timedelta(minutes=20)).isoformat()}, human['tokens']['access_token'])
                runner = Runner()

                def isolated(argv, **kwargs):
                    return runner([argv[0], '--config-dir', str(scratch/'config'), *argv[1:]], **kwargs)

                client = AnxClient(str(Path(args.cli).resolve()), base, 'fleet-smoke', runner=isolated)
                client._call(['host','enroll','--name','smoke-host','--token',enrolled['token']], timeout=30)
                topic = client._call(['topics','create','--title','Fleet test','--summary','Smoke context'], timeout=30)['topic']['ref']
                board = client._call(['boards','create','--topic',topic,'--title','Initiatives','--summary','Smoke outcomes'], timeout=30)['board']['ref']
                initiative = client._call(['cards','create','--board',board,'--title','Ship outcome','--body','Human context'], timeout=30)['card']['ref']
                mapping = {'version':1,'workspace':base,'rules':[{'id':'test','initiative':initiative,'match':{'authority':'multica'}}],'pins':[]}
                items = [{'authority':'multica','connection_id':'test','native_id':str(i),'title':f'Issue {i}','status':'in_progress','url':f'https://example.test/{i}'} for i in range(340)]
                first = apply_ingestion(client, plan_ingestion(items, mapping, client))
                assert first['revised'] == 1 and not first['errors'], first
                assert len(evidence_entries(client.card_get(initiative)['summary'])) == 340
                second = apply_ingestion(client, plan_ingestion(items, mapping, client))
                assert second['skipped'] == 1 and not second['errors'], second
                legacy = client._call_body(['work','create'], {'board_ref':board,'title':'Legacy issue','summary':'Original source content','phase':'ready','source':{'authority':'multica','connection_id':'test','native_id':'old'}}, timeout=30)['work']['ref']
                client._call_body(['work','observations','submit',legacy], {'observation':{
                    'idempotency_key':'fixture','reader_id':'fleet-sync/multica','reader_revision':'0.1.0',
                    'observed_at':datetime.now(timezone.utc).isoformat(),'status':'reported',
                    'facts':{'title':'Legacy issue','phase':'ready'},'evidence':[]}}, timeout=30)
                manifest = inventory(client, mapping, base)
                assert manifest['counts']['archive_after_fold'] == 1, manifest['counts']
                result = apply_migration(client, manifest, base, manifest['digest'])
                assert result['archived'] == [legacy], result
                card = client.card_get(legacy)
                assert card['summary'] == 'Original source content' and card.get('archived_at'), card
                replay = apply_migration(client, manifest, base, manifest['digest'])
                assert replay['already_archived'] == [legacy], replay
                print('PASS: real CLI/core; 340-item zero-create ingestion, cache-free rerun, source-backed tombstone/archive, replay; isolated workspace removed')
            finally:
                core.terminate()
                core.wait(timeout=10)


if __name__ == '__main__':
    main()
