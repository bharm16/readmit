- Settings › Team is now one named team: **Connect team** saves a name and the
  organization's configuration without connecting, and **Sign in** is one flow
  that checks the setup, connects and signs in through your browser, then reads
  the project's **Activity**, **Files** and **Reviews**. Reviews show a suite
  version's changes or a support summary and record **Approve**, **Request
  changes** (a new `readmit-hub-review-command/v3`) and **Comment** once each.
  Files list who added each file and when, **Download** names a new file first
  and **Upload** is reviewed before it is sent. **Administrator setup** holds
  Members (add member and change role as host setups over a copy of the access
  policy), Retention (applied to current files, never shortened), Audit log,
  Revisions (three-way conflict resolution), Operator hub and Host tasks. The
  generic lifecycle command form is gone.
- The customer hub records who linked each file to a project and when
  (metadata migration 7), lists a project's files, members and reviewers on
  three new v2 routes, and backs the new link metadata up as
  `readmit-hub-backup/v6`; older backups still restore, with no origin
  recorded for their links. Run `migrate` after upgrading.

Transfer reviews bind the actual connection and signed-in session. Conflict
resolutions bind the displayed competing revisions and require a fresh choice
when they change. Review requests retain their recorded suite publication and
submission instead of inferring them from reusable release bytes. Uncertain
command responses retain the original command for explicit retry; support
approval requires a verified displayed summary. Cancelling an audit destination
keeps the recorded export available for another destination choice.

A lost or malformed transfer receipt remains unconfirmed, with any confirmed
upload and original revision command retained separately. Check status reads
metadata without repeating a write, and an unconfirmed revision keeps its
local draft. Successful receipts must identify the exact command and actor.
