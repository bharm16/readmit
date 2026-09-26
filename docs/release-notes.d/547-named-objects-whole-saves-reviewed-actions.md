- The desktop now lists each project's cases, tests, suites, runs,
  environments, observations, reports, checks, profiles, scenarios, analyses,
  variants, backups, runners and schedules as named objects read through their
  own readers, keeping a missing, unreadable or unsupported one visible with
  its reason. A project can be created from a name alone, with no interface
  revision invented (`readmit-project/v2`; v1 projects read unchanged and are
  converted only on request), and a moved or renamed project reopens as the
  same project (#547).
- Saving an environment, test or observation publishes the whole object as
  one revision or nothing: an interrupted save leaves the previous revision
  current, a stale edit is refused with the draft kept, and a double click
  saves once (#547).
- A send, export or version approval is reviewed once and bound by the
  application to exactly what the review showed; the final Send, Export or
  Approve checks every binding again, refuses a changed one without sending,
  expires after fifteen minutes and never survives a restart. Nobody has to
  copy an identity or hash to approve anything (#547).
