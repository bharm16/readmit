"""One-off, idempotent issue-only execution-metadata repair.
No issue creation, closure, assignments, code edits, arbitrary commands from issue
text, or access outside bharm16/readmit. Revalidate every planning ID before writes.
"""
import json
import os
import re
import subprocess
import time

REPO = 'bharm16/readmit'
BASE = f'repos/{REPO}'
BEGIN = '<!-- readmit-execution:start -->'
END = '<!-- readmit-execution:end -->'
AREA_BEGIN = '<!-- readmit-area-context:start -->'
AREA_END = '<!-- readmit-area-context:end -->'
# Engineering-reviewed, transitively reduced DAG; not a numerical issue chain.
DEP_ROWS = """
24:
26:24
27:85
28:77,89
29:24
30:29
31:39
32:31
33:26,30
34:33
35:34
36:34,85
37:39
38:37
39:33
40:38
41:34,45
42:38,41
43:42
44:42,70
45:
46:26,30,45
47:46
48:47
49:41
50:41,70
51:37,49,50
52:49,50,77
53:38,41
54:53
55:54
56:53,47,65
57:38,45
58:57
59:58,87
60:58,53
61:26,30,45
62:61
63:62
64:63,47,108
65:26,66
66:30
67:65
68:67
69:
70:69
71:70,67
72:70,85
73:72,41,76
74:34,76
75:76
76:85
77:57,68,73,74,75,78
78:45,76
79:77,89
80:79,88
81:80,47
82:81,55
83:81
84:82,86
85:67
86:81
87:86
88:78
89:78,38,73
90:89,55,56
91:90,80
92:91
93:58,91,87
94:66
95:94,92,93
96:32,94,88
97:96
98:97,82
99:98
100:97,87
101:83,92
102:100,99,83
103:101,82,100
104:88
105:104,117,100
106:104,32,117
107:105,106
108:
109:28,40,52,59,60,64,84,95,99,103,107,119
110:40,100
111:95,99,107
112:109
113:49,50,89,77
114:95
115:99,100,106,114
116:28,117
117:30
118:117
119:118,105
"""
GATES = {
 '35': 'Confirm the supported Mirth/OIE export versions and provide or approve a legally usable, representative export corpus. The plan names an adapter but does not establish those format contracts. Record the approved inputs in this issue before applying ready-for-agent.',
 '45': 'Confirm authoritative, redistributable dictionary/profile sources and the support level for each of the seven versions and four message families in the area requirements. The broad support matrix is a target, not a verified licensing or conformance contract.',
 '75': 'Confirm the supported PostgreSQL, SQL Server, and Oracle version/driver/deployment matrix and a reproducible authorized integration-test environment/corpus. No credentials belong in the issue. Driver validation is an explicit unresolved requirement in the source plan.',
 '93': 'R18.1 (visual case-wide transformation review) was not imported and has no issue number. The external-equivalence feature needs its approved derived-case/review contract as well as the native blockers below. Do not substitute the fixture proof or treat this unrepresented prerequisite as complete.',
 '95': 'R18.1 (visual case-wide transformation review) remains unimported. The sharing workflow needs the approved review/approval surface before it can meet full acceptance. Native blockers below cover the imported dependencies; the missing task must also be resolved.',
 '104': 'Confirm the desktop OS/distribution support matrix and provision approved Apple Developer ID/notarization and Windows signing identities through repository secrets or the approved signer. Do not post signing material in issues. Signed-release acceptance cannot be completed using unsigned previews.',
 '116': 'Specify evaluation duration, offline clock policy, grace period, and feature/seat/runner entitlements for the trial. Keep these explicit product policies, not guesses embedded by an agent; sample walkthrough remains ungated.',
 '118': 'Select the billing/merchant provider and approve the product-to-entitlement mapping, renewal/cancellation behavior, invoicing ownership, and test account. Source plan deliberately leaves price unvalidated; do not invent a $2,000 product or use live charges for acceptance.',
 '119': 'Approve the license/legal notices, support-entitlement rules, organization/seat/device policy, and offline revocation language. This issue contains commercial/legal decisions that are not supplied by the source plan.'
}
NOTES = {
 '24': 'Keep Go/Cobra and the existing engine. Add Wails 2 with React/TypeScript and a typed Go facade. The initial sample workspace can open existing artifacts; complete project management belongs to #29, not a circular prerequisite for this shell.',
 '27': 'Use canonical mutable drafts from #30 and durable execution state from #85. Recover view state without attempting to resume or resend uncertain network work.',
 '28': 'This is integration acceptance for the completed visual journey, not a shell-only tutorial. It runs after visual authoring and result inspection exist, even though its planning ID is in R01.',
 '45': 'Existing CLI dictionary support is not deleted or reclassified. This issue expands coverage only for independently validated and legally redistributable combinations.',
 '47': 'Use a versioned profile-reference index and test-document contract to report impact. Do not require the entire suite editor (#81) before implementing profile versioning; #81 will consume the same reference contract.',
 '55': 'Local approval records are explicit local user decisions, not verified organization identity. Team-authenticated approvals are added by #97/#98. Do not make baseline storage depend on the future team hub.',
 '59': 'Reduction must execute through durable runs, an explicit reset plan, a trusted observation-completion contract and a stable chosen assertion-failure signature. A timeout is not an equivalent reduced failure. Its wave-B position does not remove these later-numbered prerequisites.',
 '76': 'Define source-neutral observation identity, watermark, completion, missingness and pre-existing-state contracts here. Source-specific collectors #73/#74/#75 implement them afterwards; do not create a collector-to-contract dependency cycle.',
 '78': 'Implement finite typed operators and shared selectors against the observation contract, with independent fixtures. GUI authoring #77 consumes these operators; the evaluator must not depend on its UI.',
 '85': 'Provide the durable run lifecycle and observer interface using the existing fixture/ACK path for its own tests. General collectors consume this lifecycle. Do not make the base job journal depend on every future collector or suite.',
 '88': 'Verify the shared evaluator through desktop/CLI/headless invocation adapters. Enrolled remote-runner deployment belongs to #100 and consumes this contract; do not make this issue depend on #100.',
 '108': 'Independent endpoint/corpus and mutation infrastructure may start against the implemented CLI now. Other tickets must extend that corpus for their own supported contracts; do not block independent validation infrastructure on the complete desktop product.',
 '109': 'This is finished-product integration acceptance, not permission to postpone per-feature tests until the end. Its blockers are completion gates; unit, contract, privacy and packaged tests belong in each delivering issue.',
 '117': 'Implement a versioned signed-entitlement format and local verifier with test keys and configurable policy fields. Do not select commercial prices or trial duration. #116/#118/#119 supply and approve those policies later.'
}
PLAN = {}
for row in DEP_ROWS.strip().splitlines():
    n, deps = row.split(':')
    number = int(n)
    index = 0 if number == 24 else (number - 25 if number <= 92 else number - 24)
    pid = f'R{index // 4 + 1:02d}.{index % 4 + 1}'
    PLAN[n] = {'planning_id': pid, 'depends_on': [int(x) for x in deps.split(',') if x], 'needs_info': GATES.get(n), 'notes': NOTES.get(n)}


def api(method, path, payload=None):
    if not path.startswith(BASE + '/'):
        raise RuntimeError('Repository boundary violated')
    if method != 'GET':
        allowed = re.fullmatch(re.escape(BASE) + r'/issues/(\d+)(?:/dependencies/blocked_by|/sub_issues|/labels)?', path)
        label_create = path == BASE + '/labels'
        if not (allowed or label_create):
            raise RuntimeError('Write endpoint not allowed')
        if allowed and int(allowed.group(1)) not in ({25} | {int(x) for x in PLAN}):
            raise RuntimeError('Write issue not allowed')
        if method not in ('PATCH', 'POST'):
            raise RuntimeError('Write method not allowed')
        time.sleep(1.15)
    args = ['gh', 'api', '--hostname', 'github.com', '--method', method,
            '-H', 'Accept: application/vnd.github+json',
            '-H', 'X-GitHub-Api-Version: 2026-03-10', path]
    if payload is not None:
        args += ['--input', '-']
    result = subprocess.run(args, input=None if payload is None else json.dumps(payload),
                            capture_output=True, text=True, timeout=90, check=False)
    if result.returncode:
        # Do not retry an uncertain mutation. Reruns reconcile existing state.
        raise RuntimeError(f'{method} {path}: {result.stderr[-1000:]}')
    return json.loads(result.stdout) if result.stdout.strip() else None


def all_pages(path):
    out = []
    for page in range(1, 21):
        sep = '&' if '?' in path else '?'
        rows = api('GET', f'{path}{sep}per_page=100&page={page}')
        if not isinstance(rows, list):
            raise RuntimeError('Expected paginated list')
        out.extend(rows)
        if len(rows) < 100:
            return out
    raise RuntimeError('Pagination limit exceeded')


def strip_block(body, begin, end):
    return re.sub(re.escape(begin) + r'.*?' + re.escape(end) + r'\s*', '', body, flags=re.S).strip()


def validate_graph(graph):
    done = set()
    active = set()
    def visit(n):
        if n in active:
            raise RuntimeError(f'Dependency cycle at #{n}')
        if n in done:
            return
        active.add(n)
        for b in graph.get(n, []):
            visit(b)
        active.remove(n)
        done.add(n)
    for n in graph:
        visit(n)


def verify_task(issue, n):
    pid = PLAN[str(n)]['planning_id']
    if 'pull_request' in issue or f'<!-- readmit-product-backlog:{pid} -->' not in (issue.get('body') or ''):
        raise RuntimeError(f'Issue #{n} no longer matches {pid}; stopping rather than editing another item')


def main():
    if os.environ.get('GITHUB_REPOSITORY') != REPO:
        raise RuntimeError('Unexpected repository')
    raw = all_pages(BASE + '/issues?state=all')
    issues = {x['number']: x for x in raw if 'pull_request' not in x}
    for sn in PLAN:
        n = int(sn)
        if n not in issues:
            raise RuntimeError(f'Missing issue #{n}')
        verify_task(issues[n], n)
    roadmap = issues[25]
    if '<!-- readmit-product-backlog:roadmap -->' not in roadmap['body']:
        raise RuntimeError('Roadmap marker missing')
    areas = {}
    for match in re.finditer(r'(?ms)^### (R\d{2})[^\n]*\n(.*?)(?=^### R\d{2}|^## |\Z)', roadmap['body']):
        d = re.search(r'<summary>Product-area requirements</summary>\s*(.*?)\s*</details>', match.group(2), re.S)
        if d:
            areas[match.group(1)] = d.group(1).strip()
    if len(areas) != 24:
        raise RuntimeError(f'Expected 24 full product contexts, found {len(areas)}')
    for sn, p in PLAN.items():
        for b in p['depends_on']:
            if str(b) not in PLAN or b == int(sn):
                raise RuntimeError('Invalid plan edge')
    planned = {int(n): p['depends_on'] for n, p in PLAN.items()}
    existing = {}
    for sn in PLAN:
        n = int(sn)
        count = issues[n].get('issue_dependencies_summary', {}).get('total_blocked_by')
        existing[n] = [] if count == 0 else all_pages(BASE + f'/issues/{n}/dependencies/blocked_by')
    union = {n: sorted(set(planned[n]) | {x['number'] for x in existing[n]}) for n in planned}
    known = set(union)
    queue = [b for bs in union.values() for b in bs if b not in known]
    while queue:
        b = queue.pop()
        if b in known:
            continue
        known.add(b)
        deps = all_pages(BASE + f'/issues/{b}/dependencies/blocked_by')
        union[b] = [x['number'] for x in deps]
        queue.extend(x for x in union[b] if x not in known)
    validate_graph(union)
    print(f'Preflight: {len(PLAN)} tasks; {sum(len(x) for x in planned.values())} planned edges; acyclic.', flush=True)

    # Install every edge before advertising any new task as agent-ready.
    added = 0
    for sn, p in PLAN.items():
        n = int(sn)
        have = {x['id'] for x in existing[n]}
        for b in p['depends_on']:
            if issues[b]['id'] not in have:
                api('POST', BASE + f'/issues/{n}/dependencies/blocked_by', {'issue_id': issues[b]['id']})
                added += 1
        print(f'Edges installed: #{n} {p["planning_id"]} <- {p["depends_on"]}', flush=True)

    labels = {x['name'] for x in all_pages(BASE + '/labels')}
    for name, color, desc in [('needs-info', 'D876E3', 'Waiting on reporter for more information'),
                              ('ready-for-agent', '0E8A16', 'Fully specified, ready for an AFK agent'),
                              ('wayfinder:map', '5319E7', 'Product roadmap and child-ticket index')]:
        if name not in labels:
            api('POST', BASE + '/labels', {'name': name, 'color': color, 'description': desc})
    updated = 0
    for sn, p in PLAN.items():
        n = int(sn)
        issue = api('GET', BASE + f'/issues/{n}')
        verify_task(issue, n)
        body = strip_block(strip_block(issue['body'], BEGIN, END), AREA_BEGIN, AREA_END)
        delivery = re.search(r'## Required delivery\s*(.*?)(?=\n## |\Z)', body, re.S)
        if not delivery:
            raise RuntimeError(f'Missing task scope on #{n}')
        checklist = [s.strip() for s in re.split(r'(?<=[.!?])\s+(?=[A-Z])', delivery.group(1).strip()) if s.strip()]
        blockers = union[n]
        lines = [BEGIN, '## Execution readiness', '',
                 '**Triage:** `' + ('needs-info' if p['needs_info'] else 'ready-for-agent') + '`.',
                 '`ready-for-agent` means the implementation is specified, not that its prerequisites are complete. Start only when every native blocker is closed, this issue has no unresolved information gate, and it is unassigned.',
                 '', '**Scope:** This issue owns its Required delivery only. The epic completion test and area context are shared integration requirements; do not implement all sibling tickets inside this one.',
                 '', '## Blocked by', '']
        if blockers:
            for b in blockers:
                title = issues[b]['title'] if b in issues else f'Existing prerequisite #{b}'
                lines.append(f'- #{b} — {title}')
        else:
            lines.append('- None — no unfinished implementation prerequisite was identified in this dependency review.')
        lines += ['', 'These are engineering prerequisites derived from the delivery contracts, not a serial chain inferred from planning IDs. Native GitHub relationships are authoritative. Existing relationships are preserved.']
        if p['needs_info']:
            lines += ['', '### Information required before agent execution', '', p['needs_info']]
        if p['notes']:
            lines += ['', '### Contract boundary', '', p['notes']]
        lines += ['', '### Issue-level completion checklist', '']
        lines += ['- [ ] ' + s for s in checklist]
        lines += ['- [ ] Demonstrate the required delivery through its public interface and add targeted positive and relevant negative/error/cancel/recovery tests.',
                  '- [ ] Preserve existing evidence/artifact compatibility and privacy safeguards; record any unsupported behavior explicitly.', END]
        context = '\n'.join([AREA_BEGIN, '<details>', '<summary>Full source product-area requirements</summary>', '', areas[p['planning_id'].split('.')[0]], '', '</details>', AREA_END])
        new_body = '\n'.join(lines) + '\n\n' + body + '\n\n' + context + '\n'
        if len(new_body) > 60000:
            raise RuntimeError('Issue body too large')
        old_labels = {x['name'] for x in issue.get('labels', [])}
        triage = {'needs-triage', 'needs-info', 'ready-for-agent', 'ready-for-human', 'wontfix'}
        chosen = 'needs-info' if p['needs_info'] else 'ready-for-agent'
        if old_labels & {'ready-for-human', 'wontfix'}:
            raise RuntimeError(f'Human triage changed on #{n}; stopping to preserve it')
        new_labels = sorted((old_labels - triage) | {chosen})
        if new_body != issue['body'] or set(new_labels) != old_labels:
            api('PATCH', BASE + f'/issues/{n}', {'body': new_body, 'labels': new_labels})
            updated += 1
        print(f'Triaged: #{n} {chosen}', flush=True)

    # Attach existing tasks to the existing roadmap, without reparenting.
    children = all_pages(BASE + '/issues/25/sub_issues')
    child_ids = {x['id'] for x in children}
    for sn in PLAN:
        n = int(sn)
        if issues[n]['id'] not in child_ids:
            api('POST', BASE + '/issues/25/sub_issues', {'sub_issue_id': issues[n]['id']})
    api('POST', BASE + '/issues/25/labels', {'labels': ['wayfinder:map']})

    # Read back native relationships, labels, checklists and parent links.
    fresh = {x['number']: x for x in all_pages(BASE + '/issues?state=all') if 'pull_request' not in x}
    actual = {}
    for sn, p in PLAN.items():
        n = int(sn)
        got = all_pages(BASE + f'/issues/{n}/dependencies/blocked_by')
        actual[n] = [x['number'] for x in got]
        if not set(union[n]) <= set(actual[n]):
            raise RuntimeError(f'Edge verification failed #{n}')
        names = {x['name'] for x in fresh[n]['labels']}
        wanted = 'needs-info' if p['needs_info'] else 'ready-for-agent'
        if wanted not in names or BEGIN not in fresh[n]['body']:
            raise RuntimeError(f'Triage verification failed #{n}')
    validate_graph({**union, **actual})
    children = all_pages(BASE + '/issues/25/sub_issues')
    if not {issues[int(n)]['id'] for n in PLAN} <= {x['id'] for x in children}:
        raise RuntimeError('Parent-child verification failed')
    roots = [int(n) for n, p in PLAN.items() if not p['needs_info'] and fresh[int(n)]['state'] == 'open'
             and not fresh[int(n)].get('assignees')
             and all(fresh.get(b, {}).get('state') == 'closed' for b in actual[int(n)])]
    gate = [int(n) for n, p in PLAN.items() if p['needs_info']]
    counts = {'tasks': len(PLAN), 'ready_for_agent': len(PLAN) - len(gate), 'needs_info': len(gate),
              'native_edges': sum(len(v) for v in actual.values()), 'edges_added': added, 'issues_updated': updated,
              'native_children': len(children), 'claimable_now': roots}
    print('VERIFIED ' + json.dumps(counts, sort_keys=True), flush=True)
    summary = [BEGIN, '## Execution metadata — verified September 18, 2026', '',
               f'**{counts["tasks"]} delivery tickets triaged; {counts["native_edges"]} native blocking edges; {counts["native_children"]} native sub-issues.**',
               f'`ready-for-agent`: **{counts["ready_for_agent"]}**. `needs-info`: **{counts["needs_info"]}**. The roadmap itself is not an executable task.',
               '', '**Claimable at verification:** ' + ', '.join(f'#{n}' for n in roots) + '.',
               'Readiness is specification readiness. An agent must additionally check open native blockers, assignees and information gates before claiming work.',
               '', 'The original source JSON had no task-level dependency graph. The native graph now reflects an engineering review of each delivery contract; it was cycle-checked and transitively reduced without losing prerequisite reachability. The waves remain organization guidance, not blanket barriers.',
               '', '### Information gates', '']
    for n in gate:
        summary.append(f'- #{n} — {PLAN[str(n)]["needs_info"]}')
    summary += ['', 'R18.1 remains unimported. This repair did not retry or bypass its earlier blocked creation. #93 and #95 explicitly retain that missing prerequisite as an information gate.',
                '', 'Every issue retains its original delivery text and now includes its area context, an issue-level completion checklist, and a Blocked by section matching native relationships. No original completed issue, assignment, application code, or main-branch file was changed.',
                '', '### Dependency index', '', '| Ticket | Native blockers | Triage |', '|---|---|---|']
    for sn, p in PLAN.items():
        n = int(sn)
        summary.append(f'| #{n} ({p["planning_id"]}) | ' + (', '.join(f'#{b}' for b in actual[n]) or 'None') + ' | ' + ('needs-info' if p['needs_info'] else 'ready-for-agent') + ' |')
    summary += [END]
    road = api('GET', BASE + '/issues/25')
    text = strip_block(road['body'], BEGIN, END)
    text = text.replace('No labels, assignments, or invented ticket-level blocking dependencies were added. The source defines implementation waves, not a complete task-level dependency graph.',
                        'The original import did not include execution metadata. The verified dependency/triage section above supersedes that import-only state. No assignments were changed.')
    api('PATCH', BASE + '/issues/25', {'body': '\n'.join(summary) + '\n\n' + text})
    with open(os.environ['GITHUB_STEP_SUMMARY'], 'a') as f:
        f.write('\n'.join(summary) + '\n')
    with open('verification.json', 'w') as f:
        json.dump({'counts': counts, 'actual_dependencies': actual, 'plan': PLAN}, f, indent=2)


if __name__ == '__main__':
    main()
