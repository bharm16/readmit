- Environments is now a list of the project's named environments, opening to
  one environment's Connection, Observation and Reset values with an Edit sheet
  for each and one Save (#556). Add environment asks for a name, address,
  classification (Not classified by default) and transport, which you choose;
  TLS settings appear only for TLS. Saving never approves a transport: an
  environment whose transport is not yet approved offers Approve transport, a
  reviewed step of its own that shows the address, transport and
  classification it covers, in place of Test connection. Test connection is
  explicit, says it sends no messages and shows a dated result, and the
  environment then says when it was last checked rather than showing a lasting
  Connected state. Credentials lists references by name, purpose, store and
  rotation, never a secret value or argument; arguments are replaced only when
  you ask. Allowed destinations are named ranges, and Check destination starts
  from the environment's saved classification and simulates a send without
  sending anything. Duplicate, Details and Remove are in the environment's
  menu.
- Reset actions are added and edited in their own sheet and the reset is saved
  once (#556). A Check empty observation action names one of the project's
  observations and confirms its latest completed collection found no records
  and is still within its freshness bound; a reset that only checks says
  Readmit deletes nothing.
- Observations are named objects of the project with one editor for all four
  source types — file export, HTTPS API, downstream capture and database view —
  showing only the chosen type's fields (#556). HTTPS and database sources pick
  a named project credential, a file export's record key is picked from the
  chosen file's own fields, and database filters are rows you add. Completion
  asks for a baseline only for a recorded baseline, chosen from the
  observation's completed collections. Collect shows exactly what will be read
  before you confirm, and while it runs shows its progress beside Stop. An
  observation shows its Latest result and its collections, where an incomplete
  collection says why instead of zero records, and Inspect completion reads a
  collection again without collecting. Add observation is also reached from
  Settings › Security › Add connection › Source and from a finalized capture's
  Set up observation. The old file-path observation setup is gone.
