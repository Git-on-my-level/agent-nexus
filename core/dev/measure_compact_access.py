"""Read-only synthetic fixture input; compact-ledger experiment in a NEW database.
Not a migration. Does not modify a workspace or prove online cutover/rollback.
"""
import argparse
import json
import pathlib
import sqlite3
import time

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("source", type=pathlib.Path, help="disposable synthetic workspace database")
parser.add_argument("destination", type=pathlib.Path, help="new experiment database")
args = parser.parse_args()
source, dest = args.source, args.destination
# Exclusive creation prevents replacing another experiment.
with dest.open('xb'):
    pass
conn = sqlite3.connect(dest)
conn.execute("ATTACH DATABASE ? AS original", (source.resolve().as_uri() + "?mode=ro",))
conn.create_function("digest_bytes", 1, bytes.fromhex, deterministic=True)
started = time.monotonic()
conn.execute("CREATE TABLE compact_exact(edge_id INTEGER PRIMARY KEY,target_key BLOB NOT NULL)")
conn.execute(
    "INSERT INTO compact_exact SELECT edge_id,digest_bytes(target_key) "
    "FROM original.resource_access_exact_edges"
)
conn.execute("CREATE INDEX compact_exact_target ON compact_exact(target_key)")
conn.commit()
new = conn.execute("SELECT sum(pgsize) FROM dbstat").fetchone()[0]
old = conn.execute(
    "SELECT sum(pgsize) FROM dbstat('original') WHERE name IN "
    "('resource_access_exact_edges','idx_access_exact_edges_target')"
).fetchone()[0]
row_count = conn.execute("SELECT count(*) FROM compact_exact").fetchone()[0]
# Reconstruct the original SQL relation using the existing integer edge key,
# then compare every canonical column in both directions.
legacy = (
    "SELECT edge_id,source_kind,source_id,target_key "
    "FROM original.resource_access_exact_edges"
)
compact = (
    "SELECT c.edge_id,e.source_kind,e.source_id,lower(hex(c.target_key)) "
    "FROM compact_exact c JOIN original.resource_access_edges e ON e.rowid=c.edge_id"
)
for left, right in [(legacy, compact), (compact, legacy)]:
    difference = conn.execute(
        "SELECT count(*) FROM (" + left + " EXCEPT " + right + ")"
    ).fetchone()[0]
    if difference:
        raise SystemExit(f"divergence: {difference}")
key = conn.execute("SELECT target_key FROM compact_exact LIMIT 1").fetchone()[0]
plan = conn.execute(
    "EXPLAIN QUERY PLAN SELECT e.source_kind,e.source_id "
    "FROM compact_exact c JOIN original.resource_access_edges e ON e.rowid=c.edge_id "
    "WHERE c.target_key=?",
    (key,),
).fetchall()
print(json.dumps({
    "rows": row_count,
    "old_exact_bytes": old,
    "compact_exact_bytes": new,
    "saved_bytes": old - new,
    "percent_saved": 100 * (old - new) / old,
    "elapsed_seconds": time.monotonic() - started,
    "full_relation_differences": 0,
    "plan": plan,
}, indent=2))
conn.close()
