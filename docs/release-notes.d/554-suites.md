- Suites is now a list of the project's named suites with their tests,
  environments and latest result (#554). A suite opens read-only on Tests,
  Data, Coverage and Versions, and Edit saves the whole suite — tests at exact
  saved versions, typed dataset rows, environments with named bindings,
  requirements and exclusions — as one new version with one Save; a cycle, an
  unsupported isolation or send order, a partial row or a wrong override type is
  named at its row, and nothing is dropped. A suite with no test yet saves but
  cannot run. A suite file already in the project opens as it is, and its first
  Save publishes version 1 beside it. Run hands one exact version and
  environment to the run review; Coverage assesses a retained run without
  running anything; Versions compares two versions by their exact check
  changes. Import suite, Export suite and Export run configuration read and
  write only where you choose. The Suites and releases panel and the Regression
  baseline panel are gone.
- A suite version's approvals keep their own scopes and actors (#554): Approve
  baseline releases its test versions under the local reviewer; Request review
  and Approve release go through the signed-in customer hub, only the reviewer
  asked can approve, and they bind the baseline's exact release bytes; Approve
  for environment records a local approval for one environment and target
  revision, never deploys or authorizes a send, and reads stale once a bound
  environment changes. Approvals are kept as history and never renewed.
