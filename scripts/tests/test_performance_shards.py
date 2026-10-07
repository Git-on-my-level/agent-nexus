import copy
import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location("coverage", Path(__file__).parents[1] / "check-performance-shards.py")
coverage = importlib.util.module_from_spec(spec)
spec.loader.exec_module(coverage)


class ExecutedCoverageTests(unittest.TestCase):
    def setUp(self):
        self.hash = "a" * 64
        self.routes = [dict(method="GET", path=f"/case/{i}", authorized_status=200,
            unauthorized_status=200, latency_ms=500, max_queries=100, max_rows=1024,
            max_vm_steps=50000) for i in range(109)]
        self.weights = [dict(key=coverage.case_key(r), milliseconds=i+1) for i,r in enumerate(self.routes)]
        shards = coverage.assignments(self.routes,self.weights)
        self.reports = [dict(shard=i,diagnostic=False,shard_count=4,core_source_sha256=self.hash,samples=[],completed=[]) for i in range(1,5)]
        for route in self.routes:
            report = self.reports[shards[coverage.case_key(route)]-1]
            for principal in ("authorized","unauthorized"):
                report["completed"].append(coverage.case_key(route)+" "+principal)
                for phase,index in [("first_read",0),*(("warm",i) for i in range(1,6)),("post_invalidation",6)]:
                    report["samples"].append(dict(method=route["method"],path=route["path"],
                        principal=principal,cache_phase=phase,sample=index,measured=True,
                        sampling_policy="one-first-read/five-warm/one-post-invalidation",
                        fixture_policy="fresh-pool-and-handler-per-case-principal/shared-4096-distinct-thread-corpus",
                        invalidation_epoch_before=10 if phase=="post_invalidation" else 0,
                        invalidation_epoch_after=11 if phase=="post_invalidation" else 0,
                        elapsed_ms=1,queries=1,rows=1,vm_steps=1,fullscan_steps=0,
                        sorts=0,autoindex_rows=0,status=200,deadline_exceeded=False,stream_polls=0))

    def validate(self,reports=None,allowances=()):
        return coverage.validate(self.routes,self.weights,allowances,
            self.reports if reports is None else reports,self.hash)

    def test_exact_actual_union(self):
        self.assertEqual(sum(s["cases"] for s in self.validate()),109)

    def test_missing_duplicate_and_wrong_coverage_fail(self):
        def omit_sample(reports): reports[0]["samples"].pop()
        def duplicate(reports): reports[0]["samples"].append(reports[0]["samples"][0])
        def omit_principal(reports): reports[0]["samples"] = [s for s in reports[0]["samples"] if s["principal"]=="authorized"]
        def wrong_phase(reports): reports[0]["samples"][0]["cache_phase"]="warm"
        def unknown_case(reports): reports[0]["samples"][0]["path"]="/unknown"
        def duplicate_shard(reports): reports[0]["shard"]=reports[1]["shard"]
        def stale_hash(reports): reports[0]["core_source_sha256"]="b"*64
        def diagnostic(reports): reports[0]["diagnostic"]=True
        def declared_only(reports): reports[0]["samples"]=[]
        def failed_case(reports): reports[0]["completed"].pop()
        def no_invalidation(reports):
            next(s for s in reports[0]["samples"] if s["cache_phase"]=="post_invalidation")["invalidation_epoch_after"]=10
        for mutation in (omit_sample,duplicate,omit_principal,wrong_phase,unknown_case,duplicate_shard,stale_hash,diagnostic,declared_only,failed_case,no_invalidation):
            with self.subTest(mutation=mutation.__name__):
                reports = copy.deepcopy(self.reports); mutation(reports)
                with self.assertRaises((AssertionError,IndexError)): self.validate(reports)

    def test_cold_allowance_does_not_widen_warm_work(self):
        sample = self.reports[0]["samples"][0]
        allowance = dict(method=sample["method"],path=sample["path"],principal=sample["principal"],
            cache_phase="first_read",core_source_sha256=self.hash,latency_ms=500,
            max_queries=100,max_rows=1024,max_vm_steps=100000)
        sample["vm_steps"]=90000
        self.validate(allowances=[allowance])
        self.reports[0]["samples"][1]["vm_steps"]=90000
        with self.assertRaises(AssertionError): self.validate(allowances=[allowance])


if __name__ == "__main__": unittest.main()
