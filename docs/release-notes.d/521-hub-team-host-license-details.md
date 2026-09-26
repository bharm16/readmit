- The hub panel keeps results to the configuration, session and project that
  produced them: choosing another project, configuration or signing out clears
  the previous project's artifacts and team work, and an answer for the
  previous project never appears under the new one (#521). Team work is split
  into Reviews, Notifications, Revisions, Support approvals and
  Administration; each review decision and lifecycle action shows and sends
  only its own fields, removing a user and retiring an artifact are confirmed
  by name, and each new command gets its own command ID (shown under Details)
  that a retry keeps. Recorded lifecycle events are shown as the hub returned
  them, a revision history read that reports no event head says so rather than
  showing 0, a failed history read after a recorded action no longer hides it,
  and an audit export shows its review and lifecycle events with an explicit
  Save audit file… action. An offline revision draft stays available after
  sign-out for local retention only.
- Host administration groups local copies apart from paths on the hub host and
  offers Cancel review while a preview runs and Clear preview once one is
  shown (#521). The license review names the licensed person, licensed device
  and runner pool, with what the runner pool choice allocates stated below it.
