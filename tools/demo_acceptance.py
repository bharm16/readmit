"""The authored demo acceptance obligations shared by DOM and native drivers.

The document is repository material, never a customer script. Drivers keep their
own UI mechanics; these expectations are not derived from production results.
"""
import json
from pathlib import Path

SCENARIO = Path(__file__).resolve().parents[1] / "testdata/acceptance/demo-scenario.json"
IDS = {"open-messages", "create-test", "run-defective", "view-failed-check", "run-fixed", "compare"}


def load_scenario():
    raw = SCENARIO.read_bytes()
    if len(raw) > 65536:
        raise ValueError("demo acceptance scenario exceeds its bound")
    scenario = json.loads(raw)
    if set(scenario) != {"schema", "entry", "region", "test_name", "steps", "expected", "retained"} or scenario["schema"] != "readmit-demo-acceptance/v1":
        raise ValueError("unsupported demo acceptance scenario")
    steps = scenario["steps"]
    if len(steps) != len(IDS) or {step["id"] for step in steps} != IDS:
        raise ValueError("demo scenario must retain every named obligation once")
    if any(set(step) != {"id", "title"} or not isinstance(step["title"], str) or not 1 <= len(step["title"]) <= 120 for step in steps):
        raise ValueError("invalid demo acceptance step")
    expected = scenario["expected"]
    if set(expected) != {"check", "required_count", "defective_count", "fixed_count", "defective_status", "fixed_status", "defective_label", "fixed_label", "boundary", "occurrences", "paired", "unchanged", "field_changes"}:
        raise ValueError("incomplete demo acceptance oracle")
    return scenario


def verify_comparison(report, scenario):
    expected = scenario["expected"]
    for side, status in (("left", "defective_status"), ("right", "fixed_status")):
        result = report[side]
        if result["result_status"] != expected[status] or result["result_boundary"] != expected["boundary"] or result["occurrences"] != expected["occurrences"]:
            raise ValueError("retained run disagrees with the authored demo expectation")
    if any(report["summary"][key] != expected[key] for key in ("paired", "unchanged", "field_changes")):
        raise ValueError("retained comparison disagrees with the authored demo expectation")
