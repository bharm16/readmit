Unsigned preview of local HL7 incident reproduction and regression workflows.

- The privacy panel's support bundle steps and the protection panel's Retire
  are driven end to end through the facade (#309), which turned up faults in
  both panels. The protection panel kept showing the document as it was first
  read, so a retired control still read as active, still offered Retire and
  was still offered to write a package, and a rotation still showed its old
  generation; every registration, rotation and retirement now shows the
  document it wrote. A rotation or a retirement was reported as "Registered.";
  each now says what it did. A protection document could only be selected,
  never named, so a folder without one could never register its first control
  from the window; a new document is now named, reads as empty, and is written
  by its first registration. Retiring cannot be undone, so it now asks first,
  and Keep it active or `Escape` changes nothing. In the privacy panel, a
  sharing policy the decoder refused showed nothing, a policy saved there
  showed the reading of the policy selected before it, and choosing another
  policy left the preview of the previous one on screen with its approval
  still typed; the refusal is now named, the saved policy is shown as written,
  and another policy withdraws the preview and the approval. A dismissed or
  unavailable save dialog now says so, a bundle can be verified again after it
  changed on disk, and choosing a destination or verifying no longer labels
  other controls as authoring or publishing. The journeys retire a control,
  keep opening the package it wrote, publish into a natively named folder
  after a dismissed dialog and an existing folder are refused, and refuse a
  bundle changed after publication, each as `readmit protect retire`, `readmit
  share` and `readmit share verify` decide; Go parity tests hold the window to
  those commands, including their refusals. No `readmit-*` document, command,
  bound method or machine output changes.

- The reproducer panel's build, undo, register and compare controls are
  driven end to end (#300); no test pressed them through the facade before,
  and pressing them showed several faults. A registration was reported as done
  the moment it was asked for, so a refused one read as registered while the
  refusal went to the project overview; the panel now says a build is
  registered only once the project recorded it, and shows a refusal beside
  the build. That registration was also shown
  beside every later build. Compare build with this revision compared a
  build with its registered copy, which holds no manifest, so it was always
  refused; Compare this build with another revision now hands the build
  to the revision comparison as the later revision. A registration the
  project refused left the copy it had placed, so the same name was refused
  on the next attempt; the copy is now removed and the workspace is left as
  it was. An edit can no longer name an occurrence that is not retained, a
  new Discard this plan control abandons a plan and its unstored draft without
  writing anything, and the revision comparison now shows the one run it read
  when the other revision has none. Two interaction journeys cover an edit and
  its undo, a discarded plan, refused, occupied and stale builds, a revision
  registered in the window that `readmit project show` reads as the window
  reports it, refused registrations, and three revisions compared by lineage,
  retention, edits and what the command line's runs of each decided. Go
  parity tests hold registration to `readmit project revise`, byte for byte
  and in the same words, an undone step to the plan without it, and the proof
  to what `readmit diff` reads from each run. No `readmit-*` document,
  command, bound method or machine output changes.

- The packet panels generate, verify and prepare synthetic demonstration
  packets (#308). `readmit report`, `report verify` and `report prepare` had
  no screen, and the listing called a synthetic packet unsupported. A section
  of the investigation-packet panels now generates the committed
  `siu-reschedule-v1` scenario into a new folder named in the save dialog,
  against fresh built-in receivers the window starts on loopback, and reads
  the packet back under the identity `readmit report verify` then prints;
  verifies a packet chosen in the folder dialog offline and read-only; and
  prepares runnable copies into a new folder outside it, byte for byte what
  `report prepare` writes. A changed, incomplete or unsupported packet, a folder
  inside the packet and an address that is not numeric loopback are refused
  in the commands' own words. Like the commands, none of it needs an
  activation. A synthetic packet is labelled synthetic in every view and
  listed as a `synthetic-packet`, never as the person's own evidence, and the
  retained-packet panels refuse it. The facade gains
  `ChooseSyntheticPacketPath`, `GenerateSyntheticPacket`,
  `OpenSyntheticPacket` and `PrepareSyntheticRerun`; no `readmit-*` contract,
  command, flag, exit status or machine output changes.

- Observation setup validates a saved source and a saved window each on its
  own, and a source keeps one identity (#294). Validate source document and
  Validate window document read the named document with the reader the
  command line uses and show its identity, or the refusal in the command
  line's words. A source that named its export relative to its own folder was
  pinned at save with the identity of what it declares, but validated and
  reopened with the identity of the path resolved on this machine, and the
  editor then held that absolute path, so saving again rewrote the document
  with it. Saving, validating and reopening now answer what the document
  declares and its one identity, the digest of the canonical bytes the window
  writes; a saved document keeps its bytes, and only the identity reported
  when validating or reopening a source that declares a relative path
  changes. A save refused inside a retained case used to create the folder
  it would have been written into, after which the case no longer verified;
  it now creates nothing. A refused source no longer lets the window be saved
  on its own under a "Not saved" notice. A document the reader refuses says so
  when it is opened and is no longer overwritten by what the editor held
  before, naming one document no longer reads the other again over what was
  typed, and the editor stays closed while a document is read. No
  `readmit-*` document, command, flag, exit status or machine output changes.

- The window explains what a retained run's evidence decided, assertion by
  assertion, as `readmit explain` does (#303). Until now an assertion set
  written in the assertion-set panel could only be re-decided from a terminal.
  A new **Explain a retained run** panel beside the durable-run panels takes a
  durable run, a result, a run bundle or a suite job and an assertion set,
  chosen through the host's dialogs or typed as entries of the workspace, and,
  for a set about observed records, a completion record and the source it
  read. It re-decides the set through the operation the command renders and
  shows the verdict or the execution error, the counts, the identities, each
  message's payloads and each assertion's outcome, reading, expectation and
  evidence in the command's own words: undecided, skipped and unevaluated
  assertions are never shown as passes, and values stay hidden until
  revealed. What the command refuses, the panel refuses in the same sentence,
  including a set or run of a later contract version, a stale observation and
  a file export whose records cannot be derived again. It needs no admission,
  sends nothing and writes nothing, and its cancel names its own operation.
  The command's per-assertion wording moved into `internal/runexplain` so both
  entry points share it; its output is unchanged byte for byte. An interaction
  journey explains the window's own runs of an independent downstream system
  against the command line; Go parity tests hold every line the window shows
  to what `readmit explain` prints over the runs the native window retained in
  September. No `readmit-*` document, command, exit status or machine output
  changes; two bound methods are added.

- The comparison panel's Compare, its normalization preview and its policy
  editor's Open are driven end to end through the facade (#296); until now a
  test only called the panel's own callbacks. The label of the editor's
  Retained policy document picker pointed at the preview's policy picker,
  because both used one element ID, so the editor's picker had no label of its
  own; each now has its own. Preview under this policy read whatever
  collection and keys the comparison form held, which could be another pair
  than the comparison on screen; it now reads the comparison shown, and a
  comparison of another pair, or a refused one, withdraws the reading below
  it. Opening a retained policy only replaced the exact document, so the next
  rule added in the controls replaced the opened policy with the controls'
  earlier rules; its rules now become the editor's rules, and the window names
  the entry and the SHA-256 of the bytes it read. Opening a policy over rules
  changed since they were last opened or saved asks first, and Keep these
  rules or `Escape` reads nothing. The editor's own open and save now say they
  are running and hold its controls until they answer, where a second press
  could be refused as busy over the first. Two interaction journeys compare,
  page and normalize two captured feeds, and open, extend and save a policy,
  and the command line reads each result the same way; Go parity tests hold the
  panel to `readmit diff` and `readmit normalize`, including their refusals
  and a case the native window retained in September. No `readmit-*`
  document, command, bound method or machine output changes.

- This computer's license is handled the way any software purchase is
  (#315). The `readmit license import`, `show`, `renew`, `export` and
  `release` commands had no screen, and the window's license pane installed a
  different form than `license import`, so neither entry point could read what
  the other activated. The license pane now activates one license per computer
  from the file received at purchase or its pasted contents, checks it
  locally and describes the licensee, plan, seats and term in plain words;
  renews it in place when the renewed file is activated; saves a copy byte for
  byte; and deactivates this computer after asking, so the seat can be
  reissued. It is the license the command line uses: without
  `--operation-policy` new work is admitted through it, `license show`,
  `renew`, `export` and `release` without a store act on it, and `license
  import` without `--output` installs it. Given a store, every command keeps
  its flags, output and exit statuses. The license lives in the account's
  configuration folder as an unchanged entitlement store beside the trust
  document it was verified against and, for a v2 license, the operation
  policy, clock and runner record new work is admitted through; no
  `readmit-*` contract changes, and a released license is set aside, never
  deleted. The window warns thirty days before a term ends and offers the
  operator-configured account address only as a link. The window's facade
  gains `LicenseStatus`, `ReviewLicense`, `ActivateLicense`,
  `ExportInstalledLicense` and `DeactivateLicense`.

- A new folder the window asks for is now named in the host's save dialog, so
  it can be written through the installed application (#377). A backup, a
  restored project, a recovery or rollback archive, a portable review and a
  support export each asked for their new folder through the host's folder
  dialog, which returns only a folder that already exists, while each writer
  creates its destination itself and refuses one that exists; through the
  installed window every one of those writes was refused. The save dialog
  takes a new name in a folder the person chooses and creates nothing; the
  writer creates the folder and still refuses a name that exists, with its
  reason, writing nothing into it. Dismissing the dialog names nothing and
  changes nothing. Opening a workspace, importing, choosing a hub
  configuration, a license folder, a project's parent folder, a backup to
  verify or restore and a staged package keep the folder dialog. The
  interaction journeys' scripted dialogs now answer only as a host dialog
  can: a folder dialog with a folder that exists, a save dialog with a name in
  a folder that exists, so the journeys would have caught this. The installed
  window now takes a backup, a rollback archive and a portable review through
  each platform's own save dialog in the native journeys. No `readmit-*`
  document, facade result or `readmit` command changes.

- The capture screen completes a SIU fixture listen and reopens responder
  policies and source registrations (#293). A running collector or fixture now
  says where it listens, `Listening on 127.0.0.1:PORT`, once its listener is
  ready, so a listen at port 0 can be pointed at; before, the port was known
  only after the listen had ended. A completed or cancelled fixture listen
  shows the case it sealed and its appointment ledger counted as `readmit
  listen` prints them, and a stopped capture answers cancelled rather than
  completed. Cancel holds the focus while a capture runs. The fixture tab
  listens on loopback only; before, an approval of a nonloopback bind ticked
  on the collector tab reached the fixture unseen. Open registration… and Open
  policy… read a declared document with the command line's reader, show every
  member it declares for review and fill the form for editing, keeping what
  the form has no control for; a reopened policy nothing has changed since is
  previewed without being rewritten. A fault policy's approved test endpoints
  are checked at preview, before anything binds, as `readmit collect` checks
  them, and `readmit collect` now reads its policy through the window's shared
  reader with the same output and refusals. Journeys over the real facade and
  parity tests hold the window to `readmit listen`, `readmit collect`,
  `readmit collect status` and `readmit source diagnose`. No `readmit-*`
  document, command, flag, exit status or machine output changes; the window's
  facade gains `CaptureProgress`, and its capture result gains the ledger
  counts.

- The runner panel verifies a staged runner update, and journeys drive its
  configuration, grant, job and schedule controls (#313). Verify staged
  update checks a manifest and candidate against the deployment key and
  approved build the named configuration pins, as `readmit runner
  verify-update` does. It reads the candidate without running it, refuses an
  unsigned candidate or another authority's in the command's own words, and
  withdraws its answer when another file is named. A preflight now refuses a
  job id the runner root on this machine already holds, because the runner
  never runs it again, and a saved grant revision is shown as written.
  Interaction journeys drive the configuration from a refused preview to a
  keyboard save, the verification, job documents and their preflight, a
  schedule policy reopened with its entries and identity, and, on a real hub,
  a grant revision replacing a stale grant, a job cancelled while its delivery
  waits and its occupied job id refused by the window and the command line.
  The command line's runner reads every document the window wrote. No
  `readmit-*` document or command changes; the window's update result gains
  the approved build.

- The customer-hub panel searches the team's review history and your
  notifications, and its uploads and support-export downloads are checked end
  to end against a real hub (#311). The history and notification searches were
  bound but no control called them, and a notification list the hub refused
  showed nothing at all. A search form now asks the hub's v2 routes with the
  text, evidence digest and sequence the person typed, only when they search,
  and says how many events matched; a query the hub's query contract cannot
  carry is refused before anything is sent. Loading notifications says when
  nothing is addressed to you and why a read was refused. Journeys through
  the window against a real hub now show publishing refused before anything
  is sent with no activated license and under an expired session, a role that
  may not write, a stored copy that no longer matches its digest and a support
  summary digest no approval names refused by the hub, the approved summary
  downloaded byte for byte, and a stopped hub reported rather than retried.
  The configuration is chosen through the host's dialog only:
  `SelectHubConfig` stays bound for a caller that already holds the path, and
  the capability ledger records it as superseded, as it does the v1 search
  and notification addresses, which refuse once a project carries a support
  command. No `readmit-*` document, `readmit` command, bound method or hub
  route changes.

- The test authoring panel's review of proposed expectations and the
  assertion-set panel's Import and Export are driven end to end (#302); no test
  pressed them before. A review is now recorded against the request that
  proposed what is on screen, not whatever the entry field names by then.
  Asking again withdraws the proposals on screen, so a refused request, such
  as one naming a run whose own expectations failed, no longer leaves another
  run's proposals standing beside its refusal, and a new Cancel this review
  control drops the proposals and every decision taken on them and records
  nothing. Saving or exporting an assertion set to a name that is already an
  entry of the workspace is refused as one, where it was reported as a failure
  to create the file. One interaction journey proposes expectations from runs
  the command line made, approves, edits and rejects them, and has the command
  line run what was approved; another imports a colleague's set, refuses an
  undecodable one in the command's words, and exports reviewed bytes that
  `readmit explain` reads unchanged. Go parity tests hold both to
  `readmit diff`, `readmit test` and `readmit explain` over the runs the native
  window retained in September. No `readmit-*` document, command, bound method
  or machine output changes.

- The synthetic scenario panel's design, library and SIU fixture controls are
  now driven end to end (#299), and what the window writes is what `readmit
  scenario preview`, `readmit scenario check-library` and `readmit synth`
  read and write. A refusal is now shown with its reason instead of being
  replaced by the name of the action that was refused. The design tab no
  longer offers a pack entry the local profile binding never read. The SIU
  fixture tab declares every input the command requires instead of sending a
  fixed seed and base time; the seed crosses the facade as text, read as the
  command reads it, so every seed the command accepts can be declared, and the
  base time is read through the command's declaration. Each case bundle is
  listed with the identity the command prints. The library tab reads every
  library with the command's reader, so it no longer writes a library the
  command refuses or opens one the command would refuse. It reads library and
  expectations documents up to the command's 4 MiB rather than 256 KiB,
  creates a new library rather than failing when the named entry was never
  opened, refuses an export of a library the reader refuses before writing it,
  and imports only a library named by its absolute path. A fixture check can be
  cancelled from its control or with `Escape`, removes its private
  regeneration and passes nothing. Journeys over the real facade cover an
  unsupported profile, overwrite refusals, a failing fixture check and
  cancellation, and parity tests hold the window to the command line. No
  `readmit-*` document or `readmit` command changes; the window's SIU request
  takes its seed as text and its result gains each case's identity, and
  `scenariolibrary.Decode` exposes the library reader the check already used.

- The window can forget a recent folder, import the frozen receiver fixtures
  as `readmit sample capture` does, and show a project's editable document as
  recorded (#291). Each recent folder now has a Forget action that asks first;
  Forget it removes that entry and nothing else, the folder stays where it is,
  and Keep it or Escape leaves the list alone. A folder another window already
  forgot is refused and the list as it now stands is shown. The guided sample
  panel imports the two frozen fixtures from a folder chosen in the host's
  dialog as one imported case, without activation, through the operation the
  command now shares with it: the command line writes the same case from the
  same folder, byte for byte apart from the instant each records as its import
  time, and refuses any other bytes in the same words. The project overview
  shows the editable document on request, every note with its text and every
  revision with the identity its parent was registered under, as `project
  show` prints it. The settings form gains Cancel, and Escape discards an edit;
  a refused settings edit keeps what was typed, the further interface version
  included. The saved-filter form gains Discard the unsaved filter, and Escape
  does the same. Interaction journeys drive the settings form, the recent list,
  the saved filters, the editable document and the import from the keyboard,
  through refusals, cancellation and documents a later release wrote, over the
  real facade. No `readmit-*` document changes and no command's behaviour or
  output changes; the window's facade gains `ForgetWorkspace` and
  `CaptureSample`.

- The profile panel imports profile packages and opens existing local
  profiles (#295). Its Import Package control was never driven and said only
  that an import completed, and nothing in the window called `OpenProfile`, so
  the panel could edit a profile but not open one. An import now runs the
  import `readmit profile import` performs, into a new directory named in the
  workspace, and shows what it verified: the profile, the pinned pack, the
  version seal and its SHA-256, the package's SHA-256, the local origin with
  its notice on request and the pack's provenance and rights review, and that
  nothing was activated. A tampered package, an unsupported version and an
  occupied destination are refused in the command's own words, and a running
  import can be cancelled; a cancellation after the directory was created
  says it holds an incomplete import. An Open Profile control reads a profile
  from the workspace or one of its folders, such as an imported directory,
  resolves it against the pack it pins, says whether that pack answered, and
  shows the profile's seal; it never replaces edits that were not stored.
  Interaction journeys drive both over the real facade, and the command line
  imports the same packages into byte-identical documents and refuses the
  same ones. No `readmit-*` document or `readmit` command changes; the
  window's package results gain the seal and the pack's provenance.

- Building the macOS packages no longer leaves the disk image it created
  attached, and a failed `pkgbuild` now says why (#353). `hdiutil create` can
  return, having succeeded or not, with the image it wrote still attached;
  after every create the build detaches only the devices that create left of
  the path it wrote, checked as verification checks an attach that failed, and
  never an image it did not create. A cleanup that cannot prove or finish a
  detach says so on stderr, naming the image, and leaves the create's result
  as it was. A `pkgbuild` failure is refused with what pkgbuild wrote and the
  temporary staging folder named `<payload>`, where it was an exit status and
  a command line. The packages, their names and their manifest are unchanged.

- The privacy status now shows when an operator-declared program is running
  (#356). Testing, rotating or scanning a credential reference runs the
  locator program the reference declares, and a hub configuration's key
  command, a runner configuration's key and token commands, a protection
  control's key program, a client certificate's or capture listener's key
  locator, a source's transfer program and an observation source's credential
  locator are programs the operator declared too. Any of them may contact a
  secret store or vault, but the status listed none of them, and testing,
  rotating or scanning a credential reference held the operation slot unnamed,
  so the status answered busy while its locator ran. A new row, Operator-declared
  programs, reads active while such a program runs and idle otherwise. It says
  which operation's program is running and that it may contact whatever it is
  configured to reach; Readmit cannot see or vouch for those destinations and
  adds no network access of its own. Each program reports itself from its
  start to its end through the locator read or the transfer program, the only
  two places Readmit starts one, so the row is never active merely because an
  operation that could run one is under way. The credential operations now run
  under names (`secret-test`, `secret-rotation`, `secret-scan`), and a runner
  execution's closing release now reports its key and token commands like its
  enrollment's. No `readmit-*` document, `readmit` command, bound method or
  authority changes; the status still contacts nothing.

- A customer hub configuration the window cannot remember is no longer shown
  as selected (#364). Choosing one ignored a failure to write
  `readmit-desktop-hub-selection/v1`, so the panel showed the configuration
  selected while the next window would restore the previous one or none. The
  choice is now refused and changes nothing, as the operation and commercial
  selections are: the panel keeps the configuration it had, or the reason a
  remembered one could not be restored, and says why, and choosing again once
  the selection can be written recovers. The panel now keeps what was selected
  beside every refused choice, where it used to show no configuration while
  one was still selected. Cancelling a new choice while a remembered
  configuration could not be restored no longer hides the reason until the
  status is refreshed. ADR-0005 now counts the seven local documents
  the shell keeps, the three selections among them. No contract, facade method
  or `readmit` command changes.

- The window can now edit a registered credential reference, and each
  environment document it saves names itself (#292). The credential reference
  panel always registered and never updated, so `readmit secret update` had no
  path through the window. Each reference now has an Edit action for its store,
  address, locator program, locator arguments and maximum rotation age, through
  the shared update the command uses. The window sends only the members the
  person changed, as the command changes only what its flags name, so a change
  the command line made while the edit was open is kept; an edit is refused
  for what the command refuses, one that changes nothing included. Escape
  cancels an edit and writes nothing. Locator arguments are typed one per line,
  so one may hold a space, and are counted rather than shown, as
  `readmit secret show` counts them; registering can now declare a maximum
  rotation age. A registered or edited reference store, a send policy and a
  reset plan each show `Written to FILE · identity SHA256` once saved, the
  SHA-256 of the exact bytes written, which for a plan is the `plan_sha256` a
  reset of it retains; a refused save claims none. A secrets document that
  cannot be read says why instead of showing the one read before it, and focus
  returns to the control that started an action once it answers. Interaction
  journeys drive registration, a duplicate and refused reference, a purpose
  mismatch, an invalid destination prefix, cancellation and the keyboard over
  the real facade, and the command line reads every document the window wrote
  unchanged. No `readmit-*` document or `readmit` command changes; the window's
  secrets, send-policy and reset-plan save results gain the identity, and its
  secret save request gains the change an edit names.

- Every installed desktop package is now driven through its platform's
  accessibility API (#109): the Accessibility API on macOS, UI Automation on
  Windows and AT-SPI on Linux, on all five targets in the desktop workflow's
  daily and dispatched runs and on linux/amd64 in every pull request's. The
  guided sample is created in a folder chosen through the host's own dialog,
  authored into a saved test, fails on the fixture's defect, passes once it is
  corrected and reads both verdicts back after the window is closed and
  reopened; a licensed project checks the staged upgrade the same run built,
  which the window and `readmit upgrade check` refuse as the build already
  installed and a development preview, and `readmit upgrade prepare` takes a
  rollback archive that verifies. The accessibility tree the window gave a
  screen reader at each step is kept with each run. Driving the packages found
  that the Windows packages folder carried WiX's debug database beside the
  installer, so its real candidate could not be checked as a staged upgrade;
  the build keeps it out and packaging now refuses a folder holding anything
  its manifest does not record. It also found that no destination the window
  asks for as a new folder can be chosen through a host folder dialog, which
  only returns folders that exist, and that on Windows the application can
  end as a host folder dialog opens, when WebView2 refuses the focus the
  window hands it; neither is fixed here.

- The window inspects raw HL7 files and generates and scans performance
  corpora (#290). `readmit inspect`, `readmit corpus generate` and `readmit
  corpus scan` had no screen: the window could look at HL7 only once it was
  imported into a case, and nothing in it generated a declared corpus or
  streamed a file larger than a case may hold. Two screens in the inspector
  region, opened from the palette or their headings, now do both. Raw
  inspection chooses one file through the host's dialog, declares its framing
  and terminator as the command does, and pages every message, segment, field
  and repetition the command prints, 200 rows at a time, with values only on
  request and escaped in Go, a field past 4,096 bytes shown in part and saying
  so, and a page of a file that changed since the earlier pages refused; a
  byte-identical copy goes to a new file of a chosen folder, and the source is
  never changed or imported. The corpus
  screen declares every generator input and plan member through structured
  controls, writes the corpus and its manifest to two new files of a chosen
  folder, scans one stream in bounded batches with a window and an optional
  benchmark, shows the progress counts while either runs, and cancels either;
  a cancelled scan reports the counts it reached, the case bounds not
  evaluated and no benchmark. The command line and the window now reach one
  shared operation for each: the command's inspection, reading, round-trip and
  benchmark writing moved into `internal/operation` without changing a byte
  of its output, exit status or refusal. No `readmit-*` document changes and
  no contract is added; the facade gains `ChooseInspectionPath`,
  `InspectRawFile`, `WriteRoundTrip`, `ChooseCorpusPath`, `GenerateCorpus`,
  `ScanCorpus` and `CorpusProgress`.
- Run bundles, test results, durable runs and the outputs `synth`,
  `reproducer`, `redact`, `report prepare`, `report assemble` and `report
  export` write are now reported written only once the directory entries they
  are found through are synced as well as their files (#361). Each synced
  every file but not `payloads/`, its own directory or its entry in the folder
  holding it, so after a power loss an output already reported written could be
  missing a name, which its reader refuses, or be missing altogether. Each now
  syncs every directory it made, itself and that folder after its completion
  record and before it answers; a folder it cannot open is refused before
  anything is written into it, and before a run, test or durable run connects.
  A redaction review also syncs the private state its export reads. A directory
  sync that fails after the completion record no longer says an incomplete
  output was retained: every file is written by then and the output opens, so
  the error says it was written in full but a power loss could still lose it.
  A durable run's result now syncs its own entries, so the job no longer
  repeats those syncs; a result it cannot write or sync is now reported beside
  the summary, as a failed sent prefix is, rather than only recorded as an
  execution error. Every directory sync goes through one shared seam.
  Windows keeps flushing every file and no directory, since Go does not expose
  a directory flush there. Bytes, identities and every contract are unchanged.
  The added syncs are a fixed cost per output, not per message or payload: 3
  for a run, 2 more for a result, 2 net for a durable run, 2 for a family or a
  reproducer, 15 for a redaction review and 27 for its export (including the
  proof results they keep), and 7 to 13 for a report output. On a loaded macOS
  development host, where a sync is a full device flush, a directory sync cost
  up to what a file sync does (median 0.01-3 ms, p90 about 5 ms, beside about
  4 ms per file). Median write time rose by 8.5 ms (+24%) for a one-message
  run, 12.6 ms (+23%) for a test result, 13 ms (+10%) for a durable run, 2-13
  ms (+2-13%) for a family, a reproducer or a prepared workspace, 8-18 ms
  (+3-6%) for an assembled packet or its review, and 15-34 ms (+4-5%) for a
  redaction review or export, against -2.7 ms (-4%) for an unchanged case
  write measured alongside. Windows adds no cost.

- The window inspects retained baselines and released test versions as the
  command line does, and pins a suite to a release and promotes it without the
  terminal (#306). **Inspect retained test version** showed a retained
  release's revision but never its full release identity, the identity a
  `readmit-suite-releases/v1` reference pins, so writing one needed
  `readmit expectation show`. The Regression baseline panel's inspection now
  shows a baseline's approved review identity, or a release's stable test
  identity and full release identity, in full and selectable, beside the local
  approver and rationale; the facade's inspection result gains the release's
  test identity. Each release reference in the Suites and releases panel has
  **Read identity**, which reads the named release with the same reader and
  fills the identity it declares. Pasted suite JSON the suite reader refuses
  is now refused with its reason rather than ignored, and the editor keeps what
  it held. New journeys over the real facade show a baseline and a release as
  `readmit baseline show` and `readmit expectation show` print them, refuse a
  missing and an unsupported one with the command's reason, and review and
  approve a promotion the command line reproduces byte for byte. No `readmit-*`
  document, command or machine output changes.

- The window's remaining write-side confinement gaps found after #348 are
  closed (#368). Pasted content followed a symbolic link planted at the
  workspace's `staged-sources` folder and was staged wherever it pointed; that
  folder must now be one real folder of the workspace, created when absent. A
  new project's name was joined to the chosen folder as given, so a `..`
  escape or a nested name created the project beside that folder or inside one
  of its folders; it must now be one name, refused before the dialog opens. A
  collection, a capture, a finalized collection and an import wrote into the
  workspace or project the request named without resolving it, so a symbolic
  link there sent their evidence outside; each now refuses it, as every other
  operation does, before anything is read or written. Every entry the window's
  facade itself refuses as a link, such as the case every panel reads, now has
  a test that requires the refusal's own sentence, and the tests that prove a
  link is never read through by its target's access time fail rather than skip
  where the filesystem records no read. No contract, facade method or
  `readmit` command changes.

- `readmit report --scenario` no longer flushes the workspace it throws away
  (#346). Its trials ran in a temporary execution workspace written through the
  same durable writers other commands keep, so 94 of the command's 147 file
  flushes went to files it deleted before it answered. On a Windows OS
  disk, where one flush takes several milliseconds and can stall for over half
  a second, that made the command take seconds. The generated family, each
  trial's inputs, its fixture's case and ledger and its sender's result, run
  and send-policy decision are now written as scratch, an explicit choice those
  writers take for this workspace only, and are not flushed; every other caller
  keeps them durable. The packet stays fully durable: each of its 53 files is
  synced as before, and its directories and the entry naming it in its parent
  are now synced too before the command reports success, so a parent the
  command cannot open is refused before anything runs. On three Windows
  runners' OS disks the median run fell from 1.11-1.48 s to 0.56-0.73 s, and
  on a macOS workstation from 0.78 s to 0.31 s. No contract changes, and a
  packet's bytes and identities are the same as before.

- Each page of the message grid now verifies the case once instead of twice
  (#337). A Next or Previous click, and opening an index in the grid, asked the
  facade to describe the index and then to open the window, and each call read
  and hashed every payload of the case again. `OpenGrid` now also returns the
  index details it checked, exactly as `DescribeIndex` reports them and from the
  same read, refused windows included, and the window shows those, so a page is
  one call. Every check still runs on every page: a case or an index changed
  between two page reads is refused on the second as before, and the details
  beside a window can no longer come from a different reading of the evidence
  than its rows. `DescribeIndex` and `OpenGrid` now read an index through one
  shared path, so an entry that declares the index contract but cannot be read
  as one is refused by the grid as altered since it was written, as its
  description already said, rather than as not an index. On a loaded macOS
  development host the facade work for a page fell from about 700 ms to about
  350 ms at the median over the largest case a bundle admits, and the window's
  next page from a p95 of 1,090–1,474 ms to 504–631 ms; a page is still above
  the proposed 200 ms target. No `readmit-*` document changes; only the
  window's grid result gains the index details.

- The window's remaining path-confinement gaps found after #339 are closed
  (#348). Opening a profile library accepted an absolute path, `..` and a folder
  reached through a symbolic link; it now opens only the workspace itself or one
  real folder of it, and the panel's hint says so. A generated scenario family
  and its case, a synthetic family, a source collection's staging folder and
  receipt, a capture's case and observation record, and the case and receipt a
  staged collection is finalized into were joined to the workspace as given, so
  `..`, an absolute path, a nested name or a name through a linked folder placed
  them beside the workspace, inside one of its folders or outside it. Each must
  now be one new entry name, refused before anything is read or written. Finding
  a case's index when none is named read the first bytes of whatever a symbolic
  link in the workspace pointed at before passing over it; it now passes over
  the link unread. Every entry the reader that opens it already refused as a
  link now has a test that hands it a link to a real entry of its kind. The
  environment, credential, observation and capture screens keep accepting a
  document by absolute or nested path, as decided, and the desktop guide now
  lists which inputs are workspace entries and which accept outside paths. No
  contract, facade method or `readmit` command changes.

- `readmit report` and the guided sample's practice runs no longer fail because
  the local disk is slow (#350). Both send to the built-in fixture in their own
  process, and as in redaction's proofs before #331, the fixture flushed its
  ledger twice before each ACK inside the target's five-second message timeout,
  and the sender's synced recording had to fit inside the fixture's five-second
  wait for the next message. On a loaded disk either window could expire, which
  failed the report or left a practice run without its verdict. Both fixtures
  now answer once the ledger is installed rather than flushed, since it is read
  back in the same process, and wait for their own sender for the whole
  operation: a report trial's 20 seconds, a practice run's 30. A packet still
  keeps every copy of the ledger in a synced write, and a practice run flushes
  the `observation.json` it keeps once its session is over. Target, result,
  packet and case bytes are unchanged, and `listen` still syncs before every
  ACK.

- The interaction journeys now reach a real customer hub (#109): the checkout's
  readmit-hub over mutual TLS on loopback, its store in a disposable PostgreSQL
  cluster each journey creates and removes, and a customer identity provider.
  Through the window a person connects, cancels a sign-in and signs in again,
  publishes evidence, and meets a colleague's concurrent review of it as a
  conflict their renewed decision then resolves; a released test version is
  requested for team review, refused as the requester's own approval and as an
  unrequested reviewer's, and approved by the requested reviewer from their own
  window. An enrolled runner configured and granted from the window's forms is
  refused while the restarted hub holds leases, then runs the saved test once
  against the downstream system to the verdict the window's own run and
  `readmit runner execute` reach, never runs the same job twice, and passes once
  the system is fixed; a recurring schedule for it refuses a mismatched pin,
  marks the spring-forward night, and is initialized and served by the hub's
  operator. The journeys found and fixed three defects: a published artifact's
  digest was never shown, so a person could not name it in a review; after a
  recorded decision the history showed only that one event; and a refused review
  or lifecycle decision on a stale head stated its remedy twice. The hub
  journeys run in CI's hub-journeys job; they are not installed-package
  acceptance.

- A reopened window now restores the customer hub configuration chosen in an
  earlier session (#335). The window never retained the selection, so it
  neither wrote `readmit-desktop-hub-selection/v1` nor read it back, and every
  reopen started with no hub configured. The selection is now kept beside the
  operation selection and restored by reading it and the configuration it
  names, and nothing else: the restored configuration is offline, and no hub,
  client key, sign-in or session is touched until the person acts. A
  remembered selection that can no longer be restored is shown rather than
  dropped: the hub panel names the configuration and says it is gone or why it
  no longer validates, or says the selection cannot be read, and offers neither
  diagnosis nor connection until a configuration is chosen again. The
  selection's bytes and meaning are unchanged, the privacy status now names it
  among what the shell keeps, and no contract, facade method or `readmit`
  command changes.


- A case is now reported written only once the directory entries naming its
  files are synced as well as the files (#336). The writer synced each file and
  the case directory but never `payloads/` or the folder holding the case, so
  after a power loss a case already reported written could be missing payload
  entries, which the reader refuses, or be missing altogether. Every case
  writer now syncs `payloads/`, the case directory and the folder that holds
  it before it answers; a saved correlation review, which shares the writer,
  now also syncs the folder that holds it. A folder the writer can create in
  but not open is refused before anything is written, where before the case
  was written into it. Windows keeps flushing every file and no directory,
  since Go does not expose a directory flush there. Case bytes, identities and
  every `readmit-case/*` contract are unchanged. The two added syncs are a
  fixed cost per case, not per occurrence: on a loaded macOS development host
  each took a median under 0.1 ms and at most 6.5 ms, beside about 4.4 ms for
  each file the case already synced.

- The interaction journeys now follow an investigation past its first executed
  test (#109). The saved test becomes a suite, built through the structured
  editor, prepared without sending and sent once against the independent
  downstream system: it fails on the defect and passes once the system is fixed,
  as `readmit suite run` does over the same downstream. Handed to CI as the
  POSIX workflow the window writes, run by an automation agent as installed, its
  gate follows the same defect and fix with the exit codes `readmit suite ci`
  gives, the window refuses to report a gate for a directory that holds none,
  and nothing the gate records carries an evidence value. The runs become a
  sealed packet verified read-only, a portable review that reopens read-only and
  reads the same on the command line, and a value-free support bundle published
  only under the exact identity its preview showed. The shipped planted
  example's privacy review is blocked while its policy leaves findings and is
  exported only under the exact identity once handled, with the same located
  findings `readmit redact` derives. A project is backed up, held to a quota,
  reindexed, archived, refused a delete because it changed after the delete was
  previewed, deleted after confirmation and restored from the recovery archive.
  A delivered license is verified, installed and activated without hand-written
  configuration, refuses the same issue as a renewal, exports byte for byte, and
  once released admits no new work in the window or on the command line while
  reading and backing up continue; an expired term refuses new work until its
  renewal is installed, a term in grace still admits it, and the commercial
  portal's destination is stated as missing, refused when invalid and shown as a
  link the window never requests. The test kit's license fixture signs several
  terms under one fresh key for this, as test-only code. The journeys found and
  fixed five defects: a suite or other entry saved in the suites panel was
  offered by no picker until the folder was reopened; leaving an import did not
  re-read the folder, so the imported case was not offered to the packet,
  privacy and suite panels; two imported sources' members collided in the
  extraction preview; a license with no grace period read "grace  days"; and a
  refused project write took the project and its controls off the screen. These
  journeys are not installed-package acceptance.

- The privacy status now shows every network-reaching operation as active
  while it runs (#341). A connectivity check, a fixture reset, a send-policy
  evaluation, a source access check, a runner enrollment, the start of a hub
  sign-in and every hub request held the window's one operation slot without a
  name, so while one of them reached its destination the status answered busy
  and the environment row read active only during a reduction; a practice run,
  a disclosure review or derived export (whose proofs send to loopback
  fixtures) and the hub sign-in ran under names the status did not map. Each now runs under a name the status maps to its row: the run,
  environment, capture and runner rows say which of their operations is
  running, and the hub reads active during a hub request or a sign-in rather
  than only connected. A name is recorded for work that cannot be interrupted
  as well, and the facade's tests enumerate every operation that can reach a
  destination from its own source and fail if one is unnamed. No network
  access, authority, contract or `readmit` command changes; the status still
  contacts nothing, and local work that holds the slot still answers busy.

- A save that may overwrite its output no longer writes through a symbolic
  link at the output name (#347). Upgrading a saved test's profile pin, and
  saving a scenario library entry under a name that differs from the library's
  only by surrounding spaces, opened the output with truncation, so a link
  planted there, as in a shared or synced workspace, emptied and rewrote the
  file outside the workspace it pointed at. Both now write the new document
  beside the entry and rename it onto the name, syncing the file and the
  folder, so a link, a hard link or a FIFO at the name is itself replaced and
  what it led to keeps its bytes, and a save that cannot write its replacement
  leaves the previous document in place instead of removing it. A folder at
  the name, or anything at the name the replacement is first written to, is
  refused. A regular file at the name is replaced by the same bytes as before,
  now as a new owner-only file like every other document the window writes
  over: the folder, not the file, must be writable, and another hard link to
  the previous file keeps the previous bytes. No contract, facade method or
  `readmit` command changes.

- A macOS disk image that `hdiutil create` does not write is now refused with
  what hdiutil said, with the temporary staging folder named `<payload>` rather
  than its path on the build machine (#334). The failure hdiutil documents for a
  volume it cannot unmount, `create failed - Resource busy`, is tried again at
  most three attempts five seconds apart; every other failure is refused the
  first time. The disk image's name, format and contents are unchanged.

- Redaction's fixture proofs no longer fail because the local disk is slow
  (#331). On a loaded Windows runner the fixture's synced ledger writes could
  outlast the fixture target's three-second message timeout, and the sender's
  synced recording the fixture's five-second wait for the next message, so a
  correct case was blocked with `original-fixture-proof-failed`. In both the
  original and the export's derived proof the fixture now answers once its
  ledger is installed rather than flushed, since the proof reads it back in the
  same process and every retained copy is synced, and it waits for its own
  sender for the proof's 30-second budget. Target, review, state and packet
  bytes are unchanged, as are the findings for a proof that really fails; the
  command, and the export refusal on the command line and in the window, now
  also say which fixture and step failed, without paths or values.

- The window's operations no longer follow a symbolic link that a caller names
  directly (#339). Before this, a link inside the workspace to a saved test
  outside it was preflighted and executed, with one delivery, although the
  listing and the file dialog never offered it. Preflight, durable execution,
  a suite run and its released references, run-history and packet reads of a
  suite job, a packet's specification, a controlled reduction's test,
  environment, reset plan and policy, a practice run's test, the protection
  document and the entries packed under it, the private local state an export
  or a support summary is bound to, and the built reproducer a revision is
  copied from now accept only one regular file or one real folder of the open
  workspace, as the listing and `ChooseRunSpec` already did. A link out of the
  workspace, a link to another of its entries, `..`, an absolute path, a FIFO
  and an entry of the wrong kind are refused before anything is read through
  them or sent. No run contract, retained result or `readmit` command changes.

- The interaction journeys now carry a person's investigation through to an
  executed regression test of a system readmit did not write (#109). The test
  kit gains an independent downstream scheduling system — an MLLP receiver
  written from the protocol description, sharing no readmit code, with a real
  defect and its fix — and journeys drive, through the window alone, a named
  nonproduction environment with its one approved destination and a reachability
  check that sends nothing; an observation of the system's export through a
  declared window, where a missing, stale or truncated export is an error and
  never an empty result; a test authored, preflighted and sent once that fails
  on the defect with the observed acknowledgement beside the expected one,
  passes once the system is fixed and fails again when the defect returns; a
  crash while a send waits on its acknowledgement, during which the privacy
  status reports the run active, and which reopens to an interrupted,
  delivery-uncertain run and never resends; and a crash while a test is half
  authored, which reopens to the draft. The command line reads every target, run
  and completion the window wrote and reports what the window showed. The
  journeys found and fixed eight defects: the environment, scenario, hub,
  commercial and disclosure panels' opening reads, and reopening a folder the
  window knows, were answered busy and left showing it; the privacy status was
  answered busy whenever an operation ran, so it could never say one was active,
  and now reads which named operation holds the slot instead of claiming it; a
  committed import's draft was dropped only after every queued keystroke
  retention; the environment panel reopened its forms while a document read was
  in flight, so a late read replaced what was typed; a file name there could not
  be typed in full; a saved test was not offered to the run panel until the
  folder was reopened; an observation source saved once could not be saved again
  while the panel said it was saved; and the run folder a send was about to
  write was recorded in a form the session refuses, so a crash during a send
  reopened without the run. These journeys are not installed-package acceptance.

- A hub sign-in that does not complete no longer leaves its loopback listener
  open or holds the window's operation slot (#326). A refused or forged
  browser return, a timeout, a refused code and a wait refused as busy now
  close the listener and forget the attempt at once, and a cancelled sign-in
  keeps no session. The hub panel offers **Cancel sign-in** for a browser that
  was closed, which releases the slot immediately instead of after three
  minutes. The panel keeps the connection it had and shows why the sign-in
  did not complete, and the next action goes through; nothing is retried and
  nothing is sent to the hub. No hub contract, token or stored session format
  changes.

- Choosing a saved test or suite through the durable-run panel's file dialog
  no longer refuses a file of the open workspace because the dialog spelled
  its path through a symbolic link, as it does for a workspace under `/tmp` or
  `/var` on macOS (#327). The folder the answer names is now compared with the
  workspace as the filesystem resolves it rather than as text, and the entry
  must be a regular file as the listing requires. That also refuses what the
  textual comparison let through: a symbolic link inside the workspace, which
  could lead to a file outside it, and a `..` taken after a link inside the
  workspace to a folder outside it. No run contract changes.

- The window's own interruption and measurement paths run through the shared
  interaction harness (#110). The capture panel's Cancel stops the collector it
  started; reopening the window after a kill during collection calls nothing
  that listens, sends, collects, reaches a hub or runner, resets or polls; a
  note the window called retained survives a kill; and an import cancelled while
  it writes registers nothing. An editor no longer says a draft is retained
  while a later keystroke's retention is still in flight. With a draft's
  identity now taken when a write is sent, a queued edit continues only a draft
  of its own kind and workspace, and nothing queued behind a retention that
  found its draft gone is written until the person decides. An opt-in
  measurement journey times import, indexing, grid paging, search and draft
  retention in the window over the largest case a bundle admits, and checks the
  grid draws a bounded number of rows; its published samples are from a loaded
  host, are not native painted frames, and meet no envelope target.

- Cancellation, interruption and recovery on the merged application surfaces are
  tested and hardened, and the opt-in facade measurement covers them (#110). A
  cancellation reaches an operation from the moment it holds the slot, and one
  that arrives while admission waits answers `cancelled` rather than
  `permission_denied`. An import stops between synced payload writes instead of
  after the last one, leaving an incomplete case every reader refuses, no
  receipt and nothing registered. A source collection stopped part way answers
  `cancelled`, a cancelled index rebuild keeps the index it was replacing, and
  the collector's Cancel control now names the operation it stops. Every local
  shell document is acknowledged only once the directory entry naming it is
  synced too. Process-kill, file-size-limit and cancellation tests cover import,
  drafts, collection, rebuild and reopening, which starts no listener, send or
  background work. The published samples are from a loaded development host and
  meet no envelope target; UI-path measurement through the shared real-UI
  harness is separate work.

- Three interaction journeys extend security and accessibility acceptance to
  the real window (#111): keyboard-only operation from dialogs to inspection
  with region navigation, pane resizing and text scaling; hostile markup in
  evidence, file names and searches shown as escaped text; and a note restored
  after a reopen that a released activation still refuses to store. The last
  found that typing faster than the draft store answered kept one draft per
  early keystroke, so a reopen could offer back a truncated note; an editor
  now retains its work under one identity.

- Local adversarial acceptance covers the application surfaces (#111). Every
  window operation is handed a FIFO, a link, a sparse gigabyte-long document, a
  directory and an entry leaving the workspace, and must answer promptly within
  a memory bound; opening a named environment, choosing or importing a scenario
  file by path and classifying the entry a preflight names no longer block on
  or read such an entry to its end, and pasted content is staged only into a
  folder the window opened, never creating one or an empty folder inside a
  sealed case. Startup, restoration and inspection are shown to reach no
  configured destination and resolve no name. The privacy status now also
  discloses connectivity checks, fixture resets and send-policy name
  resolution, and names the kept commercial selection. Under an expired
  license the window, like the command line, still diagnoses, exports and
  imports profile packages, backs up, verifies, restores, recovers, archives,
  deletes and takes an upgrade's rollback point, while a connectivity check, a
  fixture reset and an observation collection now reserve a runner instance
  as their commands do and exporting an edited test or assertion set admits
  the author; every desktop ledger row is checked against the admission its
  method takes. The hub no longer admits a history or notification search as
  authoring, so a read-only viewer can search. Choosing a hub configuration
  through its dialog is no longer refused as busy. Planted patient values and a
  planted credential stay out of results and shell state, a reset credential is
  refused on the observation read path, and a real customer hub over mutual
  TLS and PostgreSQL is exercised by several signed-in windows for roles,
  conflicts, stale support approvals, revocation, session expiry and restored
  offline work. The launcher now requires 75 named tests; real-UI keyboard and
  screen-reader journeys remain open.

- The capability ledger #244 is tracked against is now a checked
  `readmit-capability-ledger/v2` document. Every row names its shared backend
  operation, the canonical documents it reads and writes and its
  prerequisites, and an implemented row names the frontend interaction test
  and the Go parity test that prove it, as references `internal/capability`
  resolves against the source tree instead of prose; a command's stated
  activation is checked against the admission it declares. A row that is not
  customer work carries a typed disposition — developer tooling, vendor
  issuance, machine interface, not served, or superseded by a named
  implemented row. The ledger now also covers the commercial portal (its
  account events, the vendor issuers and the vendor's signing), the
  `readmit-hub` binary's operator commands, the command-line parents that run
  work of their own, and the repository's Makefile targets and tools scripts
  as recorded developer tooling; the hub's route inventory now lists what its
  dispatcher actually serves. Every row was re-verified against the code:
  rows whose screen is unreachable or whose interaction or parity evidence is
  missing are open, owned by new issues #290–#317, and named in the window's
  support guidance. A `readmit-capability-ledger/v1` document still reads
  under v1's rules, which now also refuse two rows sharing an id, as v1 always
  stated. No command, facade method, route or customer contract changes.

- Two interaction journeys now drive the desktop window against the real
  application rather than stubbed answers (#109). A journey harness mounts the
  production window in a test window and drives it with real keyboard and
  pointer events, while every call reaches the real desktop facade over real
  files through a test-only bridge that carries it the way the native shell
  does, except that a call the shell would never answer fails the journey; the
  host's dialogs are answered as a person answers them, and closing the window
  ends the process so a reopen reads only what is on disk. The guided sample
  runs end to end — authored, failing on the defect, passing once corrected,
  and reopened with both verdicts — and so does the start of an investigation
  over a person's own export: activation, a new project, import with the
  export's own framing, registration, a declared-retention index, search, the
  original bytes, and a reopen; the command line reads what the window wrote
  and must agree. Every pull request and every push to main runs them, and a
  failing journey fails the desktop check. They found and fixed three defects
  the stubbed tests could not: a project created in a subfolder left the
  window on the folder around it, an import that registered a case left the
  project overview empty, and a navigation read that met another read holding
  the facade's one operation slot was answered busy, which could drop "Reopen
  where you were" or leave a reopened case without its grid. These journeys
  are not installed-package acceptance.

- The desktop shell connects the entry experience to the real application
  surfaces and states the privacy status per operation (#266). The window's
  first run presents its two actual beginnings as clear choices — a real
  project over your own evidence in a folder you choose, or the free guided
  sample with its deterministic evidence and genuinely failing and passing
  saved tests — with the sample named as practice rather than as a substitute
  for own-evidence onboarding, and the license state shown as the activation
  store holds it, with activation reachable where it lives. A refused
  workspace open, a refused case verification and a run-preflight refusal
  each offer the action that repairs them — the folder dialog again, the
  folder listing, the environment and activation screens — instead of advice,
  and the two remaining CLI-directed hints name the in-app action. The
  blanket "no network request of any kind" claim is replaced by a
  per-operation disclosure of every deliberately configurable activity —
  durable execution, runner execution, capture and collection, observation
  windows, the customer hub and the commercial portal — naming its
  destination, the data it carries, the authorization it requires and, read
  from the window's own state without contacting anything, whether it is
  idle, active, configured, offline or connected right now. Feature and
  support guidance is derived from the checked capability ledger and the
  verified qualification state: the connector and database refusals #35 and
  #75 own, the de-identification and external-equivalence declines and the
  unsigned preview status are stated, and every ledger row still open is
  named as still without a checked screen, kept in agreement with the ledger
  by a check that fails in both directions. The download page separates the
  published prerelease candidate from CI's installation-tested previews and
  from a published complete product, and states the desktop packages,
  prerequisites and signature truth as the packaging tests prove it.

- The desktop shell completed the privacy journey through the existing
  disclosure, protection and sharing operations (#260). The privacy panel
  derives a fail-closed export review from actual workspace entries — the case,
  the original specification, the disclosure policy and the complete
  inventory — into a fresh review entry and a separate private local-state
  entry; a blocked review lists every unresolved export surface as the explicit
  blocker it is, and nothing is left unresolved quietly. The reviewed packet is
  exported only under an approval naming the exact materialized review
  identity, typed fresh and never retained, so any edit, changed source or
  stale approval is the refusal the command line gives rather than a warning.
  Every result states what it establishes — a disclosure-reviewed extract — and
  declines external regression equivalence by name; no re-execution happens
  while preparing an export and no synthetic result substitutes for external
  proof. The protection panel registers a control as a structured reference to
  a key readmit never holds (the key masked, locator arguments counted), and
  packs, inspects, opens and discards encrypted transfer packages under it,
  showing recipient, generation and retention facts beside what encryption does
  not establish. The support panel authors the sharing policy through
  structured controls, previews the value-free summary — the preview is every
  byte the bundle will hold — and publishes into a natively chosen local directory
  (or a fresh workspace entry) only under the exact preview identity, with a
  no-auto-upload path and the exclusions stated; a team transfer remains the customer hub's separate
  authenticated workflow. Nothing here is uploaded, no private linkage is read
  by the window, and no secret value crosses into the renderer.

- The desktop shell assembles actual investigation packets and opens portable
  reports entirely in the application (#259). The investigation-packet panels
  select verified case evidence, the exact historical specification, the
  retained current result and an optional actual baseline from what the
  workspace really holds; the preview verifies every input through the same
  readers assembly uses and shows missing and mismatched inputs, observation
  boundaries, lifecycle facts and the packet's limitations before anything is
  written — a missing baseline is stated as absent and a rewritten
  specification is named, never silently substituted. Assembly runs the
  existing retained-packet operation into a fresh protected destination and
  reads the sealed identity back, registering the packet in the workspace
  navigation; export seals the packet byte for byte beside the five inert
  offline renderings (offline HTML, PDF, Markdown, strict JSON, JUnit) into a
  natively chosen new folder with no external rendering service; and packets
  and portable reviews open read-only through the same verifiers the command
  line uses, with the report text revealed only deliberately. Opening a packet
  or a review acquires no send or mutation authority, and integrity is shown
  separately from source authenticity, disclosure approval and
  regression-equivalence claims. A verified packet is handed to privacy
  review as a separate deliberate step; nothing is uploaded or shared by
  assembling or exporting. The synthetic report capability keeps its existing
  honest label and behavior on the command line.
- The desktop privacy pane configures customer runners, recurring regression
  and CI handoffs through the application (#263). The runner configuration
  (`readmit-runner/v1`), the hub's runner-policy grants
  (`readmit-runner-policy/v1`), job documents (`readmit-runner-job/v1`) and
  schedule revisions (`readmit-hub-schedules/v1`) are generated and validated
  through the shared strict readers rather than hand-authored JSON; credential
  members are references into the customer's own store and never values. The
  panel displays a configured runner's health, engine pin and retained work,
  enrolls with a probe that reports the hub's reasoned refusals, executes one
  explicitly pinned job through the runner's own admission with its leases,
  quotas and duplicate-admission rules unchanged, and reads recovery as a read
  that never resends uncertain delivery. Schedule previews compute effective
  zone timing, missed windows and DST gaps with the backend's own functions and
  preview the exact fixed notification body an approved route may emit. CI
  handoffs for the documented POSIX, GitHub Actions and Azure DevOps workflows
  are generated in-app with the six provisioned variables validated, and
  retained CI and change-gate summaries plus a gate policy's canonical identity
  are inspected in-app. GUI-prepared suites, runner jobs and CI handoffs
  execute through the unchanged CLI/runner contracts with equivalent verdicts,
  proven by differential tests against a real hub and the actual CLI
  executable. Installing services, provisioning external credentials,
  restarting the hub with a new schedule policy and authorizing third-party CI
  services remain explicit customer-administrator actions; the application
  commits nothing to any repository and uploads nothing.
- The desktop privacy pane completed the license and commercial journey (#265).
  A received entitlement and trust document verify through native dialogs with
  the shared v1/v2 readers; a verified v2 document configures the local
  activation (author, device, runner authority chosen from what the document
  assigns, private folder chosen natively) without hand-authored
  operation-policy JSON, re-verifying at creation and never overwriting.
  Renewal and the one approved extension install later issues, refusing
  transfers, superseded sequences and released activations; the installed
  document exports byte for byte; runner capacity is shown and admissions are
  released or reconciled explicitly. The commercial portal destination is
  operator-supplied `readmit-commercial-destinations/v1` configuration shown
  before deliberate navigation, a visible prerequisite until configured;
  payment completion never activates anything, and checkout return, cancelled
  payment and pending issuance change nothing locally. The pane issues no
  entitlement, holds no signing key, and contacts no service on its own.
- The desktop shell connects authored tests and suites into preflight,
  execution, run history and linked assertion evidence (#257). The durable-run
  panels select a saved test or approved suite from the workspace (native file
  selection stays as the advanced path), generate and validate fresh output
  destinations, and preflight locally with no network or verdict: exact input,
  target and environment, effective configuration, observation and reset
  requirements, pinned engine versions, limits and the operation guard's own
  admission decision. Execution is bound to the identity the preflight fixed,
  so a changed input invalidates it; cancellation names its own operation and
  duplicate clicks cannot start two runs. Live progress reads the journal
  read-only through the recovery vocabulary; completed and partial outputs
  register in run history and reopen read-only through the existing result,
  recovery and engine-pin readers, with per-assertion expected/observed values
  revealed only deliberately and evidence links naming the retained payloads
  and the source case. Suites execute one environment through the existing
  durable queue with its declared isolation. Operation completion, assertion
  failure, execution error and delivery uncertainty stay separate facts.
- The desktop shell manages regression suites visually (#258). Suites are
  authored through structured controls over the canonical readmit-suite/v1
  contract and reopen CLI-written documents without dropped clauses or
  migrations; the exact expansion previews before preparation with effective
  inputs, bindings, release pins, dependencies and the shared-resource
  serialization rule, never reordering selected input. Release sidecars,
  expectation impact, private preparation, coverage authoring and assessment
  over the retained bytes (explicit denominator, exclusions with reason and
  expiry, unknown executions, stability evidence) and environment promotion
  review/approval against exact pins all run through the same internal/suite
  operations the command line uses. Promotion grants no send authority;
  execution hands the suite entry to the durable-run panels' own preflight and
  explicit send decision, with no path copied by hand.
- The desktop shell gained authenticated team collaboration, conflict resolution
  and project administration (#262). Assignments, evidence-linked comments,
  review requests and approvals bind hub OIDC identity; shared documents use
  explicit revision tips with compare/keep-both/resolve; offline draft branches
  reconcile after reconnect with expected-head semantics; admin remove-user,
  retention, retire and audit-export reuse reviewed lifecycle commands; custody
  limits for already-downloaded copies are explained. From the suite panel's
  release comparison, a released expectation is requested and approved for team
  review against the digest of the exact reviewed bytes under the signed-in
  identity, and the hub panel above the privacy screens announces a sharing
  policy, requests and records team approval of a published support summary by
  its exact bytes, then downloads the approved summary through the hub's
  approval-gated export route; the promotion tab's local approver label and the
  privacy screens' local approval inputs stay deliberate local acts, never
  filled from the team journeys.

- The desktop shell authors every supported test and assertion operator through
  structured visual controls (#256). Guided test authoring covers
  `ledger_count`, `ledger_equals` and `ack_field_equals` with inspected-field
  selectors, coverage preview, known-good-run suggestion review and finding
  promotion provenance; saving returns the written identity into the durable-run
  panel without sending. Assertion sets use a typed panel over all sixteen
  `readmit-assertion-set/v1` operators, conditions and quantifiers, with
  canonical JSON remaining an advanced path. Unknown versions and operators are
  refused or opened read-only; historical specs are never silently downgraded.
- The desktop shell gained visual authoring for observation sources and
  completion windows (#251). Supported file-export, HTTPS API, downstream-capture
  and database configurations use structured controls; database adapters remain
  unqualified pending #75. Local validation never queries an endpoint; collection
  requires explicit authorization. Saved documents use the shared Go readers and
  writers, reopen CLI configurations without dropping fields, and bind verified
  window references into guided test authoring. Capture completion offers
  observation binding through BindCaptureObservation into that setup panel.
- The desktop shell gained evidence capture, approved source collection and
  test responders (#250). Operators register directory and transfer sources with
  structured controls and native file/program selection, diagnose access, and
  stage collections with receipts through the same Go operations the command
  line uses. An MLLP collector and the separately labelled built-in SIU fixture
  are previewed value-free (address, policy, credential references, retention
  and limits) and start only on explicit authorized action; listening,
  collecting, stopping, stopped and failed states plus captured counts are
  shown without leaking values. Stop/cancel uses the shared engine; capture
  journals reopen read-only and never invent completion or restart a listener
  merely because a project reopens. Finalizing collected material imports a
  verified case, links receipt and provenance, and offers exploration and
  observation binding on the existing import and explorer surfaces.
- The desktop shell gained graphical synthetic scenario authoring (#254). Supported
- Desktop project maintenance: native pickers for backup create/verify/restore, quota and disposable index rebuild, archive/delete with selection-bound confirmation, schema migration preview, and offline staged-upgrade check/prepare with explicit installer handoff. Hub administration remains separate.

  ADT, SIU, ORM and ORU lifecycle sequences are designed with structured controls,
  previewed through the shared engine, generated into new artifacts with exact
  provenance, registered in the project, and continued into inspection or a test
  draft by reference. Scenario library entries can be saved, versioned, compared
  and imported/exported without silently updating pinned profiles. GUI and CLI
  generation agree for the same declared inputs; the frozen guided sample is
  unchanged.
- The desktop shell completes visual correlation, diagnosis, finding review and
  comparison policies (#253). Correlation and sequence-analysis rules, diagnose
  configurations and normalization policies are authored with structured
  controls and saved through the same Go readers the command line uses.
  Diagnosis runs over the verified case under a named builtin or saved config,
  groups findings by signature, and records confirm, dismiss and scoped
  suppression decisions with rationale; only explicitly confirmed, expressible
  findings promote into the existing test-authoring draft with provenance, and
  unsupported or unreviewed findings stay visible without becoming expectations.
  Collection comparison still shows every raw difference; a separate
  policy-scoped preview lists every suppressed, retained, undecided and
  unaddressed difference beside the rule that addressed it, without changing
  source bytes. Retained diagnosis and finding-review directories are byte-
  identical to `readmit diagnose` / `readmit diagnose review` output.

- The desktop shell gained in-app case index building, guided rebuild, and
  content search (#248). Opening a case bundle auto-selects an applicable
  verified index or displays an unindexed view showing available inspection
  capabilities without implying the case is empty. Stale, expired, damaged, or
  unsupported indexes provide guided rebuilds through the existing Go index
  builder into new derived artifacts without terminal commands. The in-app
  builder supports structured field selection (up to 16 canonical selectors)
  and explicit retention choices (`values`, `digests`, `states`, with instant
  expiry or deliberate indefinite retention) without silent PHI retention
  defaults. Workspace search distinguishes metadata matches from indexed
  content hits, routing content hits directly to the exact case, occurrence,
  and field in the inspector without requiring an already opened grid.

- The desktop shell gained a connected project workspace (#245). Creating and
  opening projects, editing settings and declared interface versions,
  registering a workspace case, and changing a registered case's title, owner,
  status, interface version, tags and incidents now run through the same
  shared Go operations the command line uses, each successful write returned
  re-read from disk. The project overview re-verifies every registered entry
  and reports the same evidence states `project show` reports. Workspace
  listings distinguish what each entry declares (results, durable runs,
  reviews, indexes, targets, rules, plans, specs, packs, analyses) and the
  index, rules, plan, pack and review pickers offer applicable entries instead
  of every entry labelled unsupported. Search results open the artifact they
  matched, breadcrumbs give back-navigation, and a checked
  `readmit-capability-ledger/v1` document seeds #244's coverage ledger with
  checks that keep new capabilities from shipping without a row. Existing
  CLI-created projects open unchanged; the shell's plain-folder inspection and
  free guided sample are unchanged.

- The desktop shell preserves all unsaved work. A new bounded, versioned
  editor draft store (`readmit-desktop-drafts/v1`, outside evidence, per
  viewer) retains notes from their first letter — before they have a name or a
  title — test drafts, canonical edits and reproducer plans under internal
  identities, each dropped only once its work is stored. Navigation commits
  only after the facade accepts it, so a cancelled folder dialog, a refused
  open or a late stale response never clears the open investigation; a refused
  retention is visible with retry, keep-as-new or explicit discard; closing
  asks only when text was not durably acknowledged; and reopening the retained
  workspace, case and region happens only on an explicit button, re-verifying
  evidence and refusing what moved or changed instead of rebinding a draft
  silently. `readmit-desktop-session/v1` is unchanged and its readers still
  restore older documents.

- The desktop frontend gained a behavior-test suite over the typed facade

  boundary (`npm test`, Vitest with React Testing Library, pinned and

  development-only). The desktop workflow executes it beside the type check

  and production build and publishes the output, so a failing component test

  fails the `desktop` check. Existing panel behavior is pinned; no Go

  dependency changed.

- Local adversarial acceptance refuses dirty candidates and source changes during

  execution, retaining the final revision alongside its finite boundary results.

- `observe collect` can read a bounded key column from approved PostgreSQL, SQL

  Server and Oracle views using selected pure-Go drivers, bound filters,

  verified TLS and endpoint-scoped credential references. The database server

  version matrix remains unqualified; see `docs/observe.md` for owner lab gates.



- `import engine` retains raw containers and a finite source-raw XML subset in

  v5 cases with explicit adapter provenance. Mirth/OIE compatibility remains

  unqualified until both pinned real-engine export matrices pass.



- Desktop regions and operation states now offer offline contextual help, fixed

  support codes, setup/recovery guidance and finite message-family recipes.



- `diagnose groups` compares recurring rule signatures across selected cases, keeps

  every finding and unsupported item, and shows representatives and affected

  occurrences without inferring population-wide rates or shared root causes.



- Case verification reuses its confined payload-directory handle within each

  read, reducing repeated traversal while still rereading every payload.

  Performance qualification accepts explicit operation policy and distinguishes

  oversized-input refusal from licensing failure. The full envelope remains open.



- Packaged CLI acceptance now runs 23 public journeys on all five native

  platforms, including portable CI failure propagation and signed trial expiry.

  Missing or skipped journeys fail; receipts retain the exact candidate identity.



- `profile export` and `profile import` transport reviewed local interface contracts,

  exact metadata pins, version seals and source/license provenance with integrity

  checks. Existing profiles and evidence remain unchanged.



- When a macOS disk-image verification attach fails, cleanup releases only its

  own newly created devices after rechecking image and mount associations. The

  original tool failure remains visible, and verification never retries attach.



- The desktop records explicit correlation accept/reject/add decisions with a

  local analyst and reason in immutable review revisions. Original machine

  findings remain intact; changed mappings invalidate dependent reviewed views.

- Sequence explanations distinguish duplicate bytes, declared likely retries,

  windowed missing ACKs, unobserved downstream links and clock uncertainty.

  Coverage stays operator-declared and absence never proves message loss.



- Git source diffs remain visible when a stray NUL appears in Go, TypeScript,

  TSX, Markdown, JSON, Python or YAML source; binary evidence keeps its existing

  byte-preserving attributes.



- Administrator and security guides connect deployment, identity, runner/scheduler

  recovery, retention and incident procedures, with explicit SBOM and owner gates.



- Local performance qualification measures CLI peak RSS, exact 5 GiB scanning,

  indexed search, facade navigation, cancellation and interruption recovery.

  Eight isolated suite executions are verified against a loopback barrier.

  The published local navigation result exceeds the proposed target; native

  webview and complete project-scale acceptance remain unproven.



- `share verify` checks reviewed support bundles offline; the support runbook

  covers synthetic reproduction, customer consent, custody and incident escalation.



- Local adversarial acceptance now requires named tests to run without skips,

  covers hostile CLI text and records browser keyboard/renderer evidence.

  Native screen-reader and deployment acceptance remain open.



- Native desktop packages now select Wails production mode and link the macOS

  file-dialog framework. Installed startup checks require a real webview, and

  packaged CLI/runner journeys plus retained native sample results provide

  partial acceptance evidence with external release gates still open.



- Vendor administration records business contacts and invoice references, reissues

  named-author/device and active-runner assignments without sequence collisions,

  and reports annual paid support scope. Commercial terms remain review drafts.



- Opt-in same-host hub schedules persist daily timezone occurrences before

  ordinary runner execution, report missed windows and uncertain recovery, and

  send only explicitly approved fixed summaries. Artifact-only backups refuse

  initialized scheduling deployments rather than omit durable claims.



- Reviewed CI change gates compare pinned baseline results, released expectations,

  coverage and execution pins, then retain private snapshots for offline

  reassessment. Unknown evidence and expired retention never pass.



- Desktop install checks download exact preview packages onto fresh native

  runners, verify installed license/source material and the expected engine with

  empty PATH, and retain synthetic evidence/state across removal. Signed release

  and clean managed-machine acceptance remain explicit owner gates.



- Hub lifecycle APIs preserve offline revision branches and explicit resolutions,

  persist project user removals, export authenticated audit history, and record

  retention/retirement without erasure claims. Backup v4 and verification preserve

  recovery bytes, review links and removal decisions.

- Explicit sharing policies generate reviewed value-free support summaries from

  actual reports and derived reviews; original evidence remains local. Exact

  approvals bind source, policy, specification and output bytes, with no external

  equivalence or source-authentication claim. Team reviews bind current policy

  versions and authenticated reviewers; history v2 and backup v5 preserve the

  old contract boundaries.



- Saved-suite CI execution adds fixed-label JSON/JUnit gates while retaining raw

  evidence privately, with tested customer-hosted pipeline examples and explicit

  coverage, cancellation and no-retry behavior.



- Managed/offline installation runbook documents native silent deployment, staged

  webview dependencies, offline activation, direct runner TLS, least privilege

  and explicit signed-candidate/enterprise-policy acceptance gates.

- `redact reexecute` binds reviewed transformations and actual original phase

  evidence before an explicitly authorized durable rerun. Matching criteria stay

  distinct from external equivalence, and new evidence requires disclosure review.



- Customer-hosted case reviews add authenticated assignments, threaded evidence

  comments, exact test-release approvals, internal notifications and searchable

  decisions. Explicit revision conflicts and backup v3 preserve immutable links.



- Explicit suite promotion approvals bind exact environment configuration, inputs,

  released expectations and isolation declarations. Every queued plan is checked

  before the first send; target software revisions remain operator assertions.



- Controlled-stop tests now synchronize actual receiver consumption and verify

  exact retained prefixes, including reads returning after shutdown. Unread

  socket data remains outside captured evidence; runtime behavior is unchanged.



- Reviewed test releases retain baseline approvals, immutable predecessor identities

  and exact profile pins. Desktop and CLI review share stale-approval refusal;

  suites can require explicit release pins and report declaration impact without

  changing expectations or treating review as execution proof.



- Suite coverage assesses explicitly declared requirements against verified retained

  executions. Skips, unsupported/disabled declarations and quarantines stay visible

  with reasons and expiry; exclusions never count as passes or shrink the denominator.

  Selected prior suites reuse retained-run comparison for possible flakiness.



- Reusable regression suites bind templates and typed data rows to named sites

  and environments, retain tags/owners, check sequence, and execute setup

  dependencies through the durable scheduler. Skips never count as passes;

  cancellation preserves uncertain delivery for read-only recovery.

- Customer runners add scoped mTLS enrollment, exact environment/version admission,

  renewable exclusive leases, local durable service execution, conservative

  recovery, resource bounds and customer-signed staged-update verification.



- `scenario check-library` verifies separately versioned template plans against

  independently authored lifecycle and wire expectations. The shipped SIU

  cancellation/booking oracle covers two streams; target outcomes remain unverified.

- Desktop canonical test import, full-document editing and new-file export retain

  all supported v1 clauses and exact reviewed bytes. The shared CLI reader

  rejects unsupported schemas/operators; desktop and CLI verdict parity is

  tested for passing and failing exported expectations.

- `scenario generate` adds deterministic data rows and typed missing/null,

  retransmission, delayed/out-of-order, encoding and timezone variants over the

  four fixture lifecycle templates. A separate generation record retains every

  input and intended arrival; raw streams carry no fabricated target verdicts.



- Desktop execution comparison separates behavior from input/target/environment/

  rule drift, binds optional baseline approvals to retained specifications, keeps

  repeated failures visible and states unknown revisions and observation gaps.

  Flakiness signals require distinct comparable retained executions; cancelled

  or incomplete journals never become stable passes.



- Byte-preserving raw and MLLP syntax inspection with explicit format reporting.

- v2.5.1 field labels, positional fallback, distinct empty/null/omitted states.

- Values hidden by default; `--show-values` explicitly displays escaped bytes.

- `--roundtrip NEW_FILE` writes an exact copy without overwriting evidence.

- `capture FILE... --output NEW_DIRECTORY` preserves occurrences, malformed

  bytes, source provenance, distinct times, and explicit ACK correlation gaps.

- `timeline BUNDLE` verifies and reopens `readmit-case/v1` evidence directories.

- Copy-stable content identity and deterministic generated provenance support.

- `listen` demonstrates fixed and defective SIU rescheduling with identical AA

  ACKs, an atomically exported ledger, and recorded case evidence.



- `diagnose` produces evidence-linked JSON/Markdown findings for the named SIU

  fixture profile, with explicit unsupported cases and capture-window limits.

- A separately named `readmit-lifecycle-v1` profile and

  `readmit-lifecycle-diagnosis/v1` ruleset, selected explicitly in the same

  `readmit-diagnose-config/v1` file, add independently selectable rules for ADT

  registration, admission, transfer, discharge, update, cancellation and merge

  occurrences and for SIU appointment booking, rescheduling, modification,

  cancellation and no-show occurrences. Its correlation rules are bounded

  not-observed hypotheses over the whole verified window: they relate identities

  within one configured namespace, never order occurrences, and run no ADT or SIU

  state machine. Every lifecycle finding carries the observed capture window, and

  `readmit-siu-v1` gains no member and changes no byte.

- A separately named `readmit-order-v1` profile and `readmit-order-diagnosis/v1`

  ruleset, selected explicitly in the same `readmit-diagnose-config/v1` file, add

  independently selectable rules for order and result correlation, output

  repeated in the capture, a status progression the named profile declares, the

  acknowledgement stages an occurrence's own header asks for, and the error

  location an acknowledgement declares inside the occurrence it answers. Status

  progression compares the statuses one identity carries and never reconstructs

  their order, an acknowledgement stage is evaluated only where the header asks

  for it unconditionally, and clinical content is never interpreted. Both

  existing diagnosis contracts gain no member and change no byte.

- `diagnose review DIAGNOSIS --case CASE --decisions FILE` records analyst

  confirmations, dismissals and scoped suppressions over one diagnosis, suggests

  the evidence that would settle each finding, and promotes a confirmed one into

  draft regression-test assertions. Machine evidence and human judgment are

  separate documents — `readmit-finding-decisions/v1` names the report it was

  read against by digest, `readmit-finding-review/v1` joins the two — and

  `readmit-diagnosis/v1`, `readmit-test/v1` and `readmit-test-draft/v1` gain no

  member and change no byte. A finding nobody decided promotes nothing, a

  promotion reads its expected value out of the verified case rather than out of

  the report, a promoted expectation carries a value only from a code vocabulary

  the named profile declares, and a confirmed finding this release cannot

  express as a test is recorded with a named reason rather than dropped or

  approximated.



- `synth` creates a reproducible synthetic SIU family with separate regression,

  cancellation, and known-invalid bundles from four explicit generator inputs.

- Five native-tested targets, SHA-256 checksums, and GitHub binary provenance.

- `diff` compares fields with explicit source mappings or declared alignment keys,

  reports ambiguous/inserted/missing occurrences, and lists scoped ignore rules.

- `test` evaluates unchanged declarative assertions against wire ACKs and

  session-bound ledger observations, with distinct pass/failure/error codes

  and a verifiable result directory.

- `replay` previews by default, sends only to explicitly configured test targets,

  preserves selected source values unless a named transformation is requested,

  and records exact transport evidence and uncertain outcomes in a new run.

- `redact` creates separate derived evidence with explicit policies and a

  fail-closed export review. Approved fixture exports regenerate their results

  and diagnosis, verify failure preservation, and exclude private mappings.



- `collect` receives HL7 of any message type over bounded MLLP under a strict

  JSON acknowledgement policy, labels every retained source explicitly, and

  seals a `readmit-case/v4` collection record that never reports application

  processing from an accept acknowledgement.

- A native desktop shell opens a workspace folder, lists what it declares, and

  verifies a case through the same reader the command line uses. It ships the

  frozen synthetic sample workspace and reopens recent folders. It is a separate

  build and is not included in these archives; it has its own unsigned native

  packages, described below.

  The window is navigated from the keyboard alone: five regions in a fixed focus

  order, a command palette, a search over what the open workspace and its project

  declare, keyboard-resizable evidence and inspector panes, light and dark, and

  text from 100% to 200%. Every status carries its own word and its own shape, so

  none is told apart by colour, and the window states that there is no telemetry,

  crash reporting, update check or analytics and names every file it keeps.

  It survives an interruption honestly: the workspace, case, region and run a

  viewer had open and every note they had typed and not stored are retained in a

  versioned `readmit-desktop-session/v1` document outside evidence and restored

  when the window opens, while a run that was in flight is reopened read-only

  and stays interrupted with its delivery uncertain. Recovery never resumes,

  restarts or resends: executing again needs a deliberate action and a new

  output folder.



- `collect` handles original and enhanced acknowledgement workflows: stage

  specific correlation with separate commit and application codes, declared

  MSH-15/MSH-16 conditions, application acknowledgements delivered to a

  separately configured endpoint under `--application-ack-timeout`, and named

  errors for acknowledgement modes it does not support. A commit acceptance is

  never recorded as an application result, and an undelivered or timed-out

  stage is retained as unanswered rather than as an application failure.



- `project` creates and manages an interface investigation: a versioned

  `readmit-project/v1` document holding declared interface versions, registered

  cases, and the title, tags, ownership, status and linked incidents of each

  one, kept beside evidence and never inside it. Provenance is read from the

  verified bundle rather than declared, and the case identity the project

  records is the same value the command line, the desktop shell and an exported

  packet name.



- `project` separates immutable evidence from editable working copies: a

  versioned `readmit-revisions/v1` document beside the evidence holds the notes

  and drafts a person maintains and the lineage of every revision. `project

  revise` registers derived evidence with the verified identity of its parent

  and the operation manifest the derived bundle declares; `project note` creates

  or replaces one note. `readmit-project/v1` is unchanged. A transformation is

  registered with its lineage or not at all: evidence that is not one is refused

  as a revision, `project add` refuses evidence that is, and one name and one

  piece of evidence are held by exactly one of the two documents. The desktop

  shell can edit a note without any edit reaching an import, a finalized run or

  any other retained artifact.



- `secret` references credentials without holding them: a versioned

  `readmit-secrets/v1` document registers the store an operator declared, the

  one purpose and endpoint address a credential may be bound to, the absolute

  path of the program that reads it back from that store, and the recorded

  rotation. No command accepts a credential value, nothing renders one, and a

  resolved value refuses to be serialized, so a target configuration, a project,

  a manifest, a report and local browser state carry a reference or nothing.

  `readmit-target/v2` adds that reference; `readmit-target/v1` and

  `readmit-run/v1` are unchanged. `secret scan` resolves the registered

  credentials and checks named files and directories, including the reference

  document itself, for those exact bytes, their JSON escapes and their base64

  encodings, reporting locations and never a value. It is one check on known

  values, not an assessment that a file is safe to share.



- `report` runs the named synthetic regression and seals its reproducer,

  verified failure/pass results, diagnosis, diff, profiles, and rerun procedure

  in one packet. Offline verification and separate rerun workspaces are included.



- Released behavior is now also checked against an independently implemented HL7

  endpoint, a hand-authored corpus that no readmit command produced, and

  mutation tests that require those checks to fail when behavior changes. No

  supported integration-engine export corpus is covered yet.



- `license` verifies a signed organization entitlement offline: a versioned

  `readmit-entitlement/v1` document, an Ed25519 signature checked against an

  explicitly selected trust store, seat and runner scope with signed device

  bindings, import and byte-identical export, renewal by issue sequence, signing

  key rotation and revocation, device transfer by release and reissue, and a

  configurable grace window. No network request, activation service or update

  check is involved, no price, plan or grace duration is chosen by the engine,

  and expiry withdraws granted capabilities only: existing evidence stays

  readable and exportable, and a revocation issued after signing is documented

  as something local verification cannot observe.



- `import` brings multi-file, folder and ZIP evidence into a `readmit-case/v1`

  case under an explicit declaration of framing, batch boundary, terminator,

  encoding, direction and members, stated as flags with no defaults or saved as

  a reusable `readmit-import-plan/v1` document. `--preview` reports every member

  and record it would extract without writing anything, and an import records

  what it did write in a `readmit-import-receipt/v1` receipt beside the case.

  Ambiguous splitting, contradicted framing or encoding, and unsafe archive

  entries are refused by name; malformed records are retained whole and

  quarantined with their reason.



- `index` builds a derived, disposable `readmit-index/v1` file over one case so

  declared fields can be searched without reading every message again. What is

  retained, in what form and until when are three declarations with no defaults;

  `states` retains nothing read out of a message and `digests` retains no value

  bytes, which is not de-identification. A damaged, truncated, expired or stale

  index is refused and rebuilt from the canonical case directory, never repaired

  and never served; an index cannot be written inside evidence, and the case

  stays readable and unchanged throughout.

- `protect` encrypts evidence with a key readmit never holds: a versioned

  `readmit-protection/v1` control registers the program that reads the key back

  from an OS or customer-managed store, the declared at-rest control on the

  volume, the recorded rotation, the lifecycle state and the retention period.

  `protect pack` writes a versioned `readmit-transfer/v1` package with

  AES-256-GCM content and an encrypted `readmit-transfer-index/v1` index, so the

  packed names and sizes are protected with the content and the plaintext

  descriptor names only the control that opens it. The evidence `protect pack`

  reads is unchanged and it writes no plaintext temporary copy of its own, which

  is a statement about that command and about no other; a wrong key, a

  rotated-away key, a truncated package, an altered one and a destination inside

  retained evidence are each refused by name. A declared storage control is

  recorded as declared and never verified, nothing is encrypted implicitly,

  retirement is not revocation, and `protect discard` refuses a package inside

  its declared retention period before stating that unlinking is not erasure on

  a solid-state device, a copy-on-write filesystem, a snapshot, a backup, a

  replica or a recipient's machine.

- `backup` copies a whole project directory into a verified `readmit-backup/v1`

  directory, reads one back whole, and restores it somewhere else with every

  registered identity intact, because a bundle identity covers relative paths

  and contents only. Incomplete evidence is displayed rather than replaced: a

  registered case that is missing, unreadable or no longer the evidence the

  project recorded is recorded and restored exactly that way, and both commands

  exit non-zero. The completion marker is written last, so an interrupted backup

  is refused rather than restored; an altered manifest, an altered or missing

  stored file, an unrecorded one, an absolute or traversing recorded path, and a

  destination inside the project or the backup are each refused by name. A

  derived index is never copied: the backup records the declarations it was

  built under and the restore builds it again from the restored canonical case,

  so a damaged index is a rebuild and no retained value is held twice.



- `import --recipe` maps CSV, JSON, XML and timestamped text envelopes through a

  reusable `readmit-mapping-recipe/v1` document that supplies the payload,

  observed time, source, direction and channel through typed operators alone.

  Previews and receipts record the recipe verbatim with the SHA-256 identity of

  its declarations. Observed times must carry their own UTC offset and

  directions are translated through the recipe's declared table, so nothing is

  inferred from a name, a timestamp or a message. CSV and XML are read from the

  member's own bytes rather than through readers that normalize line endings. A

  record whose declared values do not resolve is retained whole with a named

  reason and no provenance; a member whose structure contradicts the recipe is

  refused.



- `target` records, validates and diagnoses one named nonproduction

  environment. `readmit-target/v3` adds the environment name, its recorded

  classification, an explicit TLS server name and a client certificate whose

  private key stays a credential reference; `readmit-target/v1` and `/v2` are

  read exactly as before and neither is migrated. `target check` opens one

  connection, completes TLS and reports the negotiated session, the verified

  server name and the subject, issuer and expiry of every presented

  certificate, with expired certificates, untrusted authorities, hostname

  mismatches, refused connections, timeouts and rejected client certificates

  each named separately, and a certificate verification refused is reported so

  the failure can be diagnosed. `replay` and `test` verify against the same

  declared server name; this release's transport presents no client

  certificate, so a configuration declaring one is refused for replay rather

  than sent without it. A check never sends an HL7 payload, and it says so: reaching

  an endpoint is transport evidence, not evidence of application processing, and

  a certificate expiry is reported rather than acted on. The recorded

  classification is displayed by every command that shows a target, including

  every replay, and is never treated as permission: a person labelling an

  endpoint nonproduction is not proof the address is safe to send to.



- A recorded classification can only refuse. `production` refuses a replay

  during preparation, before a plan exists, so `replay` and `test` are both held

  to it. What a send may reach is decided separately, against the addresses the

  configuration resolves to at the point of sending: `replay --policy FILE

  --decision NEW_FILE` reads a `readmit-send-policy/v1` document of approved

  destinations and denies a class nobody recorded, a name resolving to several

  addresses, a name that does not resolve and an address no approved destination

  contains. Without a policy, a literal loopback address is the only destination

  that remains. The `readmit-send-decision/v1` document recording what was

  allowed or denied and why is retained before anything is opened; a decision

  can stop a send and cannot retract bytes already sent. `target check --policy`

  reports the same decision from the same rule and sends nothing. `listen` and

  `collect` refuse a nonloopback bind unless `--approved-bind` is passed.



- The desktop shell finds evidence through a filterable message grid over the

  rebuildable case index. It renders one bounded window of occurrences at a time

  and asks for the next, so a large case is never drawn at once, and every

  window re-verifies the case and re-checks the index against it, so a view is

  never served from an index the evidence no longer supports or one whose

  declared retention has ended. Saved filters are a

  versioned `readmit-filters/v1` document of this viewer's own local state,

  narrowing by occurrence type, source, observed time, decoded field state, ACK

  outcome and field values; the selected filter is kept when navigating from one

  case to another and when the window is opened again. Every grid states how

  many records the filter excluded, and separately how many the index could not

  settle and how many the case itself could not decode, so a filtered view never

  reads as though the case held nothing else. A saved filter holds what a person

  typed to filter by and the window's privacy status names it; no message byte,

  field value or original source path crosses the boundary into the interface.

  `readmit-index/v1` and every case, project, run, result and report contract are

  unchanged.



- `observe` owns the source-neutral contracts a regression assertion about an

  external system rests on. A `readmit-observation-window/v1` document declares

  the source identity and scope in view, the watermark the window opens at, how

  state that existed before it opened is handled, and the completion rule:

  the observed state must hold still across a declared number of samples

  spanning a declared quiet period, inside a declared deadline, so polling never

  stops at the first convenient answer. A

  `readmit-observation-completion/v1` record retains what a collector reported

  and the verdict that follows, names the boundary, window and samples it

  evaluated, and is re-decided against its declared window rather than trusted.

  An observed empty state is evidence; a collector that never ran, stale data, a

  truncated capture, a lost connection, an ambiguous source status and an

  unsupported source kind are each execution errors, and none of them may be

  read as proof that something is absent. State already present when the window

  opened is never evidence that the run produced it. `readmit-observation/v1`,

  `readmit-result/v1`, `readmit-test/v1`-`v2` and `readmit-job/v1` are unchanged.



- `observe collect` is the first source-specific collector, filling in the slots

  those contracts already declare rather than adding a second set. A

  `readmit-observation-source/v1` document declares a bounded JSON, CSV, XML or

  text export on disk, or a bounded read of an approved HTTPS API, with the

  envelope its output is carried in and the locator of the record key. Exports

  are divided by the readers a mapping recipe already uses, so nothing is

  normalized and an HL7 payload inside a record keeps its bytes. Every

  observation states how old the material it read is, and a cached or aged

  answer is stale evidence rather than current evidence. Destinations go through

  the same send policy a replay is held to and the connection uses the address

  that policy checked; TLS 1.2 is the floor with verification always on and an

  explicit customer authority supported. A credential is a reference readmit

  never stores, writes or renders, read through the one mechanism it has for

  reading a value out of a store it does not own. Retries are bounded, recorded

  and applied only to a read that produced no answer at all. The original

  material each read observed is retained unchanged beside the record, and a

  disabled collector, a stale read, a truncated export, a lost connection, an

  unauthorized read and an ambiguous response each produce a named execution

  error rather than a passing absence assertion.



- `corpus generate` writes a reproducible performance corpus from four declared

  inputs and records them, with the corpus length and digest, in a

  `readmit-corpus/v1` manifest written after it. `corpus scan` reads one back

  under the same `readmit-import-plan/v1` an import declares, through a 64 KiB

  window in bounded parsing batches, holding one record and one batch whatever

  the stream's length; it reports the peak it actually held beside the bound it

  is held to, renders one bounded window of records, acknowledges an interrupt

  within one record without leaving an artifact behind, and names the case

  bundle bounds a stream is already past instead of widening them. A

  `readmit-benchmark/v1` report publishes the declared corpus, the declared

  bounds, what the run measured and the machine it measured on, with the

  performance envelope proposed for the product recorded explicitly as

  engineering targets rather than as measurements. The desktop message grid now

  virtualizes its rows, so what it draws is decided by the viewport rather than

  by the size of the window or the case. Every existing case, index, project,

  run, result and report contract is unchanged.



- `target reset` returns one named nonproduction environment to its declared

  starting state through reviewed reset actions, and never through code a

  document supplied. A `readmit-reset-plan/v1` document an operator selects

  explicitly declares actions from a closed set of typed Go operators, each

  carrying the one authority it requires: a step a person performs and confirms

  by name, a read of exactly the one receiver observation file named inside the

  plan's own directory, and one connection that sends no HL7 payload. The

  contract holds no command, script, interpreter or argument; a test spec cannot

  name a plan or an action; and reset prose stays operator-readable text that

  nothing executes. A reset refuses every environment nobody recorded as

  nonproduction, holds its one connection to the same approved-destination

  decision a send is held to, and retains a `readmit-reset-outcome/v1` document.

  A reset that failed, or that could not be confirmed, exits 2 as an execution

  error rather than an assertion failure or a pass. `readmit-target/v1`-`/v3`,

  `readmit-test/v1`, `readmit-observation/v1`, `readmit-send-policy/v1` and

  `readmit-send-decision/v1` are unchanged.



- `collect` serves several peers at once (`--max-connections`, up to 64), each

  becoming its own case source and its own recorded session, with receipts

  sealed in evidence order. A peer beyond the limit waits for a free slot rather

  than being accepted and dropped. `--max-sessions` and `--max-capture-bytes`

  declare a connection budget and a capture quota; reaching either, or

  `--max-messages`, stops the capture in a controlled way, and nothing is read

  once the quota cannot hold it. The listen socket accepts TLS and mutual TLS

  (`--tls-certificate`, `--tls-key-reference`, `--secrets`, `--client-ca`);

  declaring a client authority requires and verifies a client certificate it

  issued, the private key stays a credential reference readmit never stores, and

  one implementation now applies the TLS 1.2 floor, the always-on verification

  and the explicit-authority rule for every path that negotiates TLS.

  `--journal` retains the new `readmit-capture-journal/v1` record — every

  inbound frame's bytes synced before it is answered, and each acknowledgement's

  intent synced before it is written — and `collect status JOURNAL` recovers an

  interrupted capture read-only. Recovery separates acknowledgements that were

  sent, that were attempted and did not complete, and whose outcome was never

  recorded at all; it never resends, never resumes, and never reports an

  incomplete capture as a finished one. The capture's own exit status is

  unchanged by the journal. A

  `readmit-receiver-policy/v3` fault plan still requires one connection at a

  time, and the separate application acknowledgement endpoint remains plain

  transport. `readmit-collection/v1`-`/v3`, `readmit-receiver-policy/v1`-`/v3`,

  `readmit-case/v4` and `readmit-job/v1` are unchanged.



- `source collect` brings evidence in from an approved customer-controlled

  source: a `readmit-source/v1` document declares an export directory this

  machine can open, or a remote export reached by running the customer's own

  read-only transfer program, and each entry is streamed under the same

  `readmit-import-plan/v1` an import declares, so a collection holds one record

  at a time and stages original bytes that `import` then reads. The declared

  quota is applied to the source's own listing before anything is read and

  refuses rather than truncates; identical bytes under a second name are

  recorded as a duplicate of the first, by an identity over names and contents

  only; every attempt starts an entry again from nothing and only a transport

  failure is retried, so a retry never turns an uncertain read into a confident

  one. `source diagnose` reports what access was actually available by

  attempting it and collects nothing. A source that could not be listed, an

  entry no attempt read whole, a quota refusal, a cancellation, a destination no

  explicitly selected policy approved, and an application interface — which this

  release declares and does not collect from, with the read-only contract one

  would have to satisfy documented instead — are each execution errors rather

  than evidence that nothing was there. A remote source's credential is a

  `source-endpoint` reference registered in `readmit-secrets/v1`, presented to

  the transfer program on standard input and never as an argument; readmit

  implements no SSH client and cannot establish what answered. Every existing

  case, index, project, run, result, report and observation contract is

  unchanged.



- The shared profile-pack contract, `readmit-profile-pack/v1`, is defined and

  read by `internal/profilepack` with independently authored positive and

  negative fixtures. A pack declares its identity and version, its source,

  extraction, license and rights-review provenance, and its coverage per HL7

  version (2.3.1 through 2.8.2) and message family (ADT, SIU, ORM, ORU) with

  separate parse, labels, structural and workflow support. A combination the

  pack does not declare is unknown, untested and unsupported do not pass, a

  field name is answered only under a combination whose labels are supported,

  and no v1 pack may claim structural or workflow support. No pack is bundled,

  no command reads one, the bundled `readmit-field-labels/v1` dictionary is

  unchanged, and correlation, the profile and reproducer editors, typed

  assertions and the seven-version library remain separate deliveries.



- The set of packs a release carries is a profile library, read by

  `internal/profilelibrary` from one directory of pack documents. It adds no

  document and no member: opening a library refuses two packs that declare the

  same HL7 version and message family, two packs sharing an id, a document the

  pack reader refuses, a member larger than a pack may be and anything in the

  directory that is not a regular pack document, including a symbolic link, so

  one pack answers a combination or nobody does. There is no precedence, no nearest

  version, no fallback between families or levels and no merge of two packs'

  labels. The published matrix states all 28 combinations of the seven versions

  and four families, covered or not, and every answer names the pack that gave

  it. A library is bundleable only when it holds at least one pack and every

  pack records an approved rights review; that check reads what was written

  down and performs no review. No pack and no library is bundled, so every

  combination this release publishes is unknown, and extracting the seven

  version packs and obtaining the rights review of each exact extraction remain

  the repository owner's work.



- `run` cancels, recovers and cleans up without a duplicate effect: `run start

  --deadline` bounds a run and records `timed_out` with any in-flight delivery

  uncertain; a cancellation after a synced intent stops the send and stays

  uncertain; a failed or short sent-prefix or journal write is sticky, halts

  further sends, retains the readable prefix and is reported beside the

  summary; and the journal limit refuses a record before any byte of it. `run

  status --recovery` reports a `readmit-run-recovery/v1` classification of

  every occurrence as `not_attempted`, `acknowledged` or `uncertain`, whether

  completion was recorded, the state of the run's `readmit-run-lease/v1` lease,

  and whether a resume would repeat only never-attempted work. `run resume` is

  the one deliberate re-execution, into a new output, refusing after any send or

  an unrecorded completion; `run clean` removes only a stale lease and never

  evidence. `readmit-job/v1` is unchanged, the desktop facade is unchanged, and

  no scheduler, admission or automatic resend is added.

- `license` reads a second entitlement contract, `readmit-entitlement/v2`,

  beside v1: named authors each assigned at most two devices, and runner

  capacity counted in execution instances active at once and divided among

  customer-controlled authorities the document names. `license import --author

  --device` activates one assigned device under a `readmit-entitlement-store/v2`

  record, transfer is a reissue at the next sequence plus `license release`,

  and `license runner init|admit|renew|release|reconcile|show` keep an

  authority's local `readmit-runner-admission/v1` record under one exclusive

  update: an instance that stops without releasing holds its capacity until an

  operator reconciles it or it renews, admission and renewal are refused after

  expiry while started work keeps its bounded lease and still settles, and a

  copy of the record elsewhere is documented

  as a second authority the engine cannot detect. Independent issuer vectors

  are committed for both versions under a test-only seed. `readmit-entitlement/v1`,

  `readmit-entitlement-trust/v1` and `readmit-entitlement-store/v1` gain no

  member and change no byte; the D6 clock guard is not delivered here and stays

  #116's separate versioned state.

- Purchasing, invoicing, renewal and cancellation happen in the merchant of

  record's hosted portal, outside this repository. The vendor half is

  `internal/billing`: a `readmit-billing-account/v1` ledger moved by signed

  `readmit-billing-event/v1` payment events, authenticated against the same

  Ed25519 trust store contract the customer side uses. An event identifier is

  deduplicated so redelivery leaves the ledger byte for byte unchanged, and a

  late event may stop a renewal but never change what is granted next, so a

  payment that occurred before, or in the same recorded second as, a

  cancellation cannot restore the renewal it withdrew. A verified completed

  payment issues the next organization-scoped entitlement; an invoice alone

  issues nothing, and only an approved net-terms invoice issues a separately

  bounded provisional term; a mid-term increase needs explicitly settled

  proration; a downgrade applies at renewal; cancellation, refund and chargeback

  stop future renewal while the paid-through term and its grace stand, delete

  nothing and withdraw no document already signed. No command reads, writes or

  signs either contract, no payments dependency, HTTP client, webhook endpoint

  or merchant account is added, prices stay in provider configuration, and no

  member of either contract can carry a case title, an endpoint, a patient

  identifier or an evidence hash. Merchant onboarding, production prices,

  provider sandbox acceptance and the issuer signing identity remain launch

  gates. Existing entitlement contracts gain no member and change no byte.

- A support matrix, `docs/support-matrix.md`, states per workflow, source and

  platform exactly what is implemented and what is not available, with the

  documentation page and the test behind each row; it ships in every archive.

  `samples/synthetic-walkthrough` is a scripted, synthetic-only evaluation

  (inspect, synth, timeline, capture, index, diagnose, diff, a local test

  preview, a sealed loopback report and its verification) that a test runs

  against the built executable. `site/` holds a static, dependency-free preview

  site whose every claim is traced to a page and a test in `site/CLAIMS.md`. It

  contains no screenshots: none has been captured from a built release yet, and

  none will be mocked. No contract changes.

- `correlate CASE --rules FILE` correlates evidence across declared sources and

  namespaces under a `readmit-correlation-rules/v1` document: typed

  `acknowledges`, `control-id` and `identifier` operators, each inside a

  `source`, `session` or `declared` scope, with assigning authorities mapped

  explicitly so equal identifier strings in different namespaces stay distinct.

  Linkage an occurrence declares about itself is distinguished from equality a

  rule inferred, and colliding identifiers — an ambiguous acknowledgement, a

  control ID duplicated inside one source, an unqualified identifier, equal

  strings under different configured authorities — are reported with every

  candidate and never merged. A rule whose scope the case cannot supply is not

  applied and says so. The `readmit-correlation/v1` report is derived and

  disposable and carries no identifier bytes; `readmit-case/v1` and every other

  existing contract gain no member.



- A team models its own interface contract in `readmit-local-profile/v1`, read

  and edited by `internal/localprofile`. One document pins the exact profile

  pack and the one HL7 version and message family it constrains, and carries

  site-defined Z-segments and constrained standard fields with a usage code, a

  typed conditional requirement, a cardinality, a data type, a local code

  table, an assigning authority and a date-handling rule. A typed editor adds,

  replaces and removes each of them; every change is held to the whole contract

  before it is kept, so a refused change leaves the profile exactly as it was,

  reverting restores what was opened, and the document is written

  deterministically. Resolving a profile against its pinned pack answers, per

  rule, whether the pack declares it, the profile overrode it, the profile

  declared it alone, or nobody did — and because `readmit-profile-pack/v1`

  carries field labels and nothing else, every constraint is reported as local

  with no profile backing, and an unpinned pack contributes nothing at all. No

  message is evaluated against a profile, no profile is bundled, no command

  reads one and no window edits one yet; `readmit-profile-pack/v1` and every

  other existing contract gain no member and change no byte.



- `readmit-assertion-set/v1` is the shared contract for expressing what an

  interface should have produced, read by the new `internal/assertion`. Sixteen

  typed operators in four families — field equality, inequality and the four

  shapes, bounded text patterns, numeric ranges and tolerances; a temporal

  window; record counts, uniqueness, membership, order, multiplicity and

  bounded absence; and the two relationships that relate two values or two

  observations — address values through the one shared field selector. A finite

  equality condition and three per-record quantifiers replace an expression

  language, and one table binds each operator to the subject it reads and the

  expectation it takes, so an incompatible pairing is refused when the document

  is read. Present, empty, explicit null and omitted stay four separate

  answers; a value an operator's type cannot read, a timestamp carrying no

  offset, text past the match bound and a quantifier over no record are

  undecided rather than passed or failed; an unmet condition is skipped and

  asserts nothing; and a collection question asked of an observation that did

  not complete is an execution error, never a count of zero. Regular

  expressions are bounded and compiled by the reader, decimals are compared as

  exact rationals rather than as floats, and evaluation opens nothing, sends

  nothing and retains nothing, so a cancelled evaluation produces no verdict

  and recovers by running again. No command reads a set; `readmit-test/v1`,

  `readmit-result/v1`, `readmit-observation/v1`,

  `readmit-observation-window/v1` and `readmit-observation-completion/v1` gain

  no member and change no byte. Visual authoring, suggestion and review, and

  round-trip execution of these operators remain separate deliveries, and no

  profile-specific rule is validated against a profile pack here.



- The desktop shell extracts and edits a reproducer out of a verified case and

  writes it as a separate revision. A `readmit-reproducer-plan/v1` plan is an

  ordered list of typed operators `internal/reproducer` interprets: retain and

  drop occurrences, retain the acknowledgements the case itself correlated,

  retain the earlier occurrences of the same source that declare an identity an

  operator named, and replace or clear one declared field position. Undo removes

  the last step and the plan is replayed from what remains, so dropping an

  occurrence and undoing that drop returns its edits. A build writes a new

  `readmit-case/v3` bundle under the `readmit-reproducer/v1` derivation with a

  `readmit-reproducer/v1` transformation manifest beside it naming the parent

  hash, every retained occurrence with the relation that retained it, and the

  position, operator and prior state of every edit — never the bytes an edit

  replaced. An acknowledgement the case recorded as ambiguous names several

  candidate messages and none is chosen; an occurrence nothing decoded and one

  declaring none of the named identity fields match nothing. An omitted

  position, `MSH-1`, `MSH-2`, a value carrying a delimiter or a control byte,

  two edits over the same or overlapping bytes and a nonstandard delimiter

  declaration are each refused. The case read is never changed, the destination

  must be a new entry outside every retained artifact, and nothing here is a

  de-identification, minimality or sharing claim: reduction, replay

  transformations and reproducer comparison are separate deliveries, and

  `redact` remains where review and approval live. `readmit-case/v1`, `v2` and

  `v4`, every existing `v3` bundle, the project, index, run, result, report and

  observation contracts and the command-line tree are unchanged; no command

  reads a plan.

- Durable runs record which engine evaluated them. Every job retains a

  `readmit-engine/v1` pin beside its plan, written before the first journal

  record, naming the build that executed the run, the test-spec contract that

  spec declared and the semantic profile applied to its observations, and the

  released executable carries a stamped build identity rather than `dev`.

  `readmit run status JOB --engine` reports exactly the retained document,

  whether or not this build reads it, so the versions are visible before any

  refusal. The desktop panel and the command line remain two ways into one

  evaluator and an enrolled runner will be the third; a run started through

  either of the two pins the same versions. A build identity this release does

  not recognize is recorded, never refused; a spec or profile version it does

  not evaluate is refused by name by `run status`,

  `--recovery`, `run resume`, `run clean` and the desktop's **Recover

  evidence**, and the refusal changes nothing. A job retaining no readable pin

  is refused as one this release did not write, which includes one written by a

  development build from before this contract; durable runs have never appeared

  in a published archive, so no released artifact is affected. The pin is a

  sibling document outside the journal's hash chain, like the lease and the

  send decision. `readmit-job/v1`, `readmit-run/v1`, `readmit-result/v1` and

  `readmit-test/v1` gain no member and change no byte. Identity parity between

  a published desktop installer and a published command-line archive stays a

  packaging and signing gate; the desktop shell now has unsigned native

  packages, and neither they nor a signed installer are published.



- Changing a local profile is versioned, compared and reported against the

  saved tests that pin it, by the new `internal/profileversion`.

  `readmit-profile-version/v1` seals one profile at one version over the length

  and SHA-256 of its canonical document, so writing the same rules two ways

  seals identically and changing one rule does not; a profile that changed

  under the version it still claims is refused by name rather than compared.

  A comparison names every part that differs — the pinned base, a segment, a

  constrained position, a local code table, an assigning authority, a date rule

  — as added, removed or changed, with the members that differ stated.

  `readmit-profile-references/v1` records which saved tests and cases pin which

  version, as the consumer's own document: `readmit-test/v1` and every other

  existing contract gain no member and change no byte. A pin is an exact id and

  version together with the checksum of the document that version stood for, so

  an index says which rules each saved test was written against without holding

  the profile. An assessment lists the saved tests written against the version

  that changed, tells them apart from the ones already on the new version, the

  ones a version bump left untouched and the ones on another version, and

  states no verdict — no message is evaluated against a profile in this

  release. No pin moves on its own: an upgrade names one saved test, the

  version it is on and the version it moves to, refuses a profile rewritten

  under a version a saved test already pins, and there is no call that upgrades

  everything at once. Nothing here opens a file, no command reads either

  document and no window edits one yet.



- The desktop shell authors a regression test from selected evidence, so a

  `readmit-test/v1` spec is no longer written by hand. `internal/testauthor`

  holds a `readmit-test-draft/v1` draft bound to the case a person verified and

  answers it one typed stage at a time — the test name, the occurrences it

  sends, the target configuration, the outcome boundary, the observation source,

  the reset instructions an operator reads and readmit never executes, and the

  expectations. Each answer carries only the member its own stage declares and

  replaces that stage rather than adding to it, and the whole draft is checked

  afterwards, so withdrawing a message an expectation reads, or changing the

  boundary under an expectation it cannot make, is refused and leaves the draft

  exactly as it was. Withdrawing an answer is answering the stage with nothing,

  and choosing the ACK boundary withdraws an observation source the ledger

  boundary had asked for and reports that stage as unanswered again rather than

  generating a spec carrying both. Choosing the boundary fixes the initial state

  and the evidence fixes the send order, so neither is answered twice. An

  acknowledgement or an occurrence nothing decoded as a sent message, a target

  the shared reader refuses, a production-classified environment, an observation

  source at the ACK boundary, a record count at that boundary, an

  acknowledgement expectation naming a message the test does not send and a

  position outside MSA and ERR are each refused where the answer is given rather

  than when the spec is run. A save writes one new entry of the workspace

  exclusively and reads it back, so the identity reported is the identity of the

  bytes on disk; a failed write leaves no partial file, and an unsaved draft is

  unstored work the window loses. `readmit-test/v1` gains no member and changes

  no byte, `readmit-desktop-session/v1` gains no member, no command reads a

  draft, and no evidence is changed. `internal/testrunner` exports the two

  initial states, its assertion and expected-text bounds and one expected-value

  check it already performed, so the authoring flow holds an answer to this

  contract's own rule rather than to a copy of it; the contract's bytes,

  members and behaviour are unchanged. `readmit-assertion-set/v1` is not authored

  here because no command in this release executes one; suggesting expectations

  from a run, approving them, previewing what one inspects, and importing or

  editing an existing spec remain separate deliveries.



- `observe collect` observes a **downstream HL7 capture** and binds what it holds

  to what a run produced, so a regression assertion can inspect the system

  actually under test instead of a readmit-only appointment ledger. A

  `readmit-observation-source/v2` document declares the sealed case a receiver

  retained, the occurrence kinds in scope, the bound on what one read may take,

  and one shared field selector naming the position the record key sits at —

  `SCH-1.1`, `MSA-2` — which is both the whole mapping from what the run

  produced to what the downstream system received and the whole of what leaves

  the capture. Nothing is asked of the system under test: no acknowledgement

  receipt, no ledger export and no readmit-specific message, and the fixture

  receiver's `appointment-ledger` boundary is neither used nor needed. The

  capture is opened through the one verifying case reader and never written to;

  the snapshot names its `identity.sha256` marker rather than copying evidence

  readmit already retained. A capture states its age from the times it recorded, and

  because it is an ordered log rather than one state of one age the watermark is

  applied to both ends of what it covers: one reaching back past the window is

  stale rather than counted, because counting the earlier occurrences would

  attribute state that was already there to the run and dropping them would

  decide the count by a filter the retained record cannot show. The completion

  rule is the source-neutral one every other collector is held to, and a capture

  that is absent, that does not

  verify, that is past its declared occurrence bound, that is stale, that holds

  an occurrence nobody could parse or that does not hold the declared position

  each produce a named execution error rather than a passing absence assertion.

  The capture transport is a new version string, not a member on the contract

  that shipped: `readmit-observation-source/v1` is still read, still refuses a

  `capture` member, and `readmit-observation-window/v1`,

  `readmit-observation-completion/v1`, `readmit-observation-evidence/v1`,

  `readmit-observation/v1` and every case contract gain no member and change no

  byte.



- The desktop shell compares two collections of the open workspace side by side.

  It calls the same `internal/diff` engine `readmit diff` runs and renders the

  `readmit-diff/v1` report it returns as rows: no contract gains a member, no

  byte of one changes, and no command-line output is parsed. Records pair by the

  occurrence identity two copies of one verified case already carry, or by the

  field selectors named as keys; a regenerated control ID stays an ordinary

  field change rather than becoming a pairing, and two collections with no known

  mapping and no declared key are refused with what to declare instead of being

  paired by position. Both panes are columns of one row list, so a record only

  one side holds keeps a row of its own with the other side stated as empty, and

  every candidate of a duplicated key keeps one too because none of them is

  chosen. A window states where it begins and how many rows the comparison

  holds, beside the counts of everything paired, changed, inserted, missing,

  unaligned and not compared, how many keys were ambiguous, the versioned engine

  contract the rows came from and the boundary that was compared. An ambiguous

  key says to name another field beside it, because the keys together form one

  ordered composite key. A row names the positions that differ

  as canonical field selectors with the bundled label and the decoded state of

  each side; no value, no alignment-key value and no message byte crosses the

  facade, and reading the bytes stays the inspector. No ignore rule is applied,

  so nothing the comparison found is suppressed before it is shown, and evidence

  nothing decoded is listed rather than compared around. The comparison is bound

  to the identity the window verified, reads two entries of the open folder and

  writes nothing anywhere. Ignore and normalization policies, reviewed

  baselines, drift separation, the acknowledgement boundary and comparing runs,

  results or standalone files in the window are separate deliveries.



- `explain RUN --assertions SET` re-decides a `readmit-assertion-set/v1`

  document against one run bundle's own retained evidence and writes the chain

  of reasoning to the console, so a verdict can be followed without opening a

  JSON file: the outcome and its counts, every assertion with the expectation it

  declared and the value it was decided on, the occurrence and payload file each

  value came from, the run's per-message and whole-run timings, and the input

  case, target configuration and contract versions it ran under. `input` is what

  the run sent and `observed` is what the interface answered, both addressed by

  the occurrence id a case bundle names a message by; a payload the run retained

  nothing for and one that does not parse are an `unknown_message` execution

  error rather than an omitted value. A collection assertion reaches real

  records without widening anything:

  `readmit-observation-completion/v1` still retains counts, state digests and

  correlations and gains no member, so the keys are derived again from the

  downstream capture the observation read and refused unless they agree with

  both the settled count and the settled state digest. The occurrences that

  observation recorded as produced are checked against what this run actually

  produced, so a record collected beside some other run is refused rather than

  reported under this run's identity, and one that recorded none says plainly

  that it binds nothing. A bounded file export and

  an approved HTTPS API observation are refused by name rather than answered

  from a second, later reading of material that has moved on, and a scope the

  set asks about with nothing supplied is refused before anything is evaluated

  rather than reported as an incomplete observation. Passed, failed, undecided

  and skipped stay four separate answers; an execution error leaves every

  assertion unevaluated rather than producing a partial verdict; and exit codes

  distinguish a pass, a decided disagreement, and a question that was left open.

  Expected and observed values are message content and are hidden unless

  `--show-values`. Nothing is opened beyond the named artifacts, nothing is sent

  and nothing is written: there is no explanation artifact and no new contract.



- The desktop shell is packaged as the platform's own installer, separately

  from these archives and from any publication: a `.deb` declaring its

  WebKitGTK and GTK dependencies for Ubuntu 24.04 on x86-64 and arm64, a `.dmg`

  and a managed-installation `.pkg` for Intel and Apple silicon macOS, and a

  per-machine `.msi` with silent install and removal for Windows x64, which

  refuses a machine without the WebView2 runtime instead of installing a window

  that cannot open. Each package is built on the machine it targets and then

  installed, checked and removed there by that platform's own installer, and

  each build records a `readmit-desktop-package/v1` manifest naming every

  package, its SHA-256 and `"signed_for_distribution": false`. The packaged shell carries the

  same engine stamp as the command line of the same commit, so an installed

  application and an archived executable report one build. The installation

  check runs the installed executable for its identity, which resolves every

  library it links against; **no window is opened on any target**, because no

  runner has a display. **These packages are development previews that are not

  signed for distribution:** no Developer ID signature, no notarization, no

  stapling and no Windows code signature, they are published nowhere, and

  Gatekeeper, SmartScreen or endpoint policy may refuse them. On Apple silicon

  the application carries the ad-hoc signature the linker applies, because

  macOS runs no arm64 executable without one; it names no authority and is not

  distribution signing, and the installation test fails if an authority ever

  appears. Removing the application never removes

  evidence, a project, or the local shell-state documents.



- `run queue PLAN --send --runs DIR` executes several durable runs in one

  foreground command against a `readmit-run-queue/v1` document. A job declares

  `shared` or `isolated` state, and only an explicit isolated declaration lets

  two jobs reach one environment at the same time: a shared job holds the named

  environment and the endpoint its target records for the whole run, and the

  bounded parallelism the queue declares never overrides that. `after` names the

  jobs that must have passed first, so a setup that did not pass takes the tests

  that needed its state with it instead of letting them run against state nobody

  established. Admission reads the `lease.json` beside the other runs in the

  same directory and refuses to join a holder, including a lease a stopped

  writer could not release, which `run clean` removes; it is a read and not a

  filesystem lock, so one runs directory is one queue's and that limit is

  stated rather than assumed away. Every spec is read before the first run

  starts, a cycle or an existing run directory refuses the queue whole, and

  cancelling it starts nothing further while the runs in flight record their

  own stop and their own uncertainty. The queue reports

  `readmit-run-queue-report/v1`, a separate document naming each job's

  admission — executed, never created, refused or skipped — the resources it

  declared, what it waited for and its own unchanged `readmit-job/v1` summary.

  `readmit-job/v1` and `readmit-run-lease/v1` gain no member and change no byte.



- `upgrade check --candidate STAGED_DIRECTORY` is the explicit, offline update

  check an administrator runs before installing a newer readmit: it reads the

  `readmit-desktop-package/v1` manifest of a candidate somebody staged, re-reads

  every package file against its recorded digest, refuses a staged directory

  holding a file the manifest does not record, and reports what the build

  running the check makes of each project and durable run named — `readable`,

  `unsupported` where it names a contract version this build does not read, or

  `unreadable`. At least one must be named: a check that reviewed nothing is

  not a compatibility review and is never reported as ready. It contacts no

  update service, downloads nothing and writes nothing, and **no evidence is

  converted, migrated in place or rewritten**:

  there is no converter for an unknown contract in either direction.

  `upgrade prepare PROJECT --output NEW_ARCHIVE --approve` takes the verified

  `readmit-backup/v1` recovery archive the upgrade is rolled back to, and

  writes nothing at all without that approval. Neither command installs

  anything: the installation is the platform's own installer run with

  elevation, which is where an administrator approves it. The plan is a new

  `readmit-upgrade-plan/v1` contract; no existing document gains a member.

  Because every package this repository builds records

  `"signed_for_distribution": false`, the check refuses every candidate staged

  from one — signing, notarization and publication remain the owner's gate.



- `scenario preview SCENARIO` shows an interface workflow designed as a

  sequence. A `readmit-scenario/v1` document binds one fixture lifecycle

  profile — `readmit-adt-lifecycle-v1` or `readmit-siu-lifecycle-v1` — to the

  identities it keeps linked from the first step to the last, the state each of

  them starts in, when each event happens relative to a declared base time, and

  the outcome its author intended for every step. The profile's typed

  transitions decide whether that is what the sequence does: a step declared

  accepted that the lifecycle refuses, and a step declared refused that it

  takes, each refuse the whole scenario by name, so a negative case nobody

  confirmed is never mistaken for one that was. A refused step changes nothing,

  which is how cancellation and merge cases live inside one workflow — cancelling

  before a booking, cancelling a cancellation, rescheduling after one, merging a

  patient identity into itself, merging one already merged away, and any event

  on a visit whose identity was merged away. Two editable templates ship. The

  preview names scenario-local subjects and never the identifiers a document

  declares, it is a pure function of that document, and it generates no message

  bytes and reads no evidence.

- `transform CASE --rules FILE --plan FILE` previews relationship-preserving

  replay transformations of a case and writes nothing at all. A

  `readmit-transform-plan/v1` document is an ordered list of typed operators

  `internal/transform` interprets — rename the values one declared correlation

  rule relates, shift every supported timestamp by one explicit duration, and

  reorder, duplicate or drop an entry of the sequence a replay would send —

  bound to the verified case identity and to the SHA-256 of the exact

  `readmit-correlation-rules/v1` declarations whose relations it preserves.

  Which occurrences belong together stays `readmit correlate`'s answer: one

  surrogate is assigned per relation, so occurrences a rule related stay

  related, occurrences it related to nothing stay apart, a control ID

  duplicated inside one source stays a duplicate, a copy receives its

  original's value, and the acknowledgement of a renamed message has its MSA-2

  rewritten onto it. An acknowledgement this case tied to several candidates,

  two changes over the same or overlapping bytes, a timestamp that is not whole

  seconds, a nonstandard delimiter declaration and a plan that drops every

  occurrence are each refused; equal identifier bytes under no configured

  assigning authority, an acknowledgement naming no message here and an

  occurrence nothing decoded are reported and left exactly as they are. The

  `readmit-transform-preview/v1` preview records positions, states, relation

  numbers and the pinned profile pack's declared support at all four levels —

  never a value byte, before or after — and only a supported outcome passes: a

  combination the pack does not declare supported at the parse level, and the

  date fields a shift does not move, are each recorded explicitly rather than

  left to a doc page.

  Every entry names the case occurrence and case source it came from across

  reorders and duplications. `readmit-reproducer-plan/v1`,

  `readmit-reproducer/v1`, `readmit-correlation-rules/v1` and

  `readmit-correlation/v1` gain no member and change no byte, and no derived

  evidence and no new derivation name is produced. Reduction, applying these

  operators inside the reproducer editor, and any de-identification, minimality

  or sharing claim remain separate deliveries.



- The desktop shell walks a **guided sample without a terminal**: it creates the

  synthetic sample workspace, an authored regression test is saved into it, that

  saved spec is run against a practice receiver the application binds on a

  loopback port of this machine as the fixture's defect makes it behave and

  fails, and the same spec is run against the corrected receiver and passes. The

  case, the spec and both results are ordinary artifacts the command line reads.

  Where somebody is in the path is read back out of the folder every time rather

  than remembered, so no tutorial state exists to disagree with the evidence. The

  sample workspace is now a workspace rather than a family: the generated case

  bundles are byte-identical to `readmit synth`, the `family.json` completion

  record is not kept because nothing may be written inside retained evidence, and

  two entries that are not evidence are written beside the cases — a loopback

  practice endpoint, so a test has a target to name, and a states-only index of

  the sample case, so the message grid the authoring flow selects occurrences

  from can be opened without `readmit index build`. That sample index is the only

  one this product builds without being asked each time, and its three retention

  declarations are fixed and published. None of this is in these archives, and no

  desktop package is signed for distribution.



- Bounded delta reduction with a controlled oracle: `internal/reduce` shrinks

  the sequence a regression test sends against a chosen assertion-failure

  signature declared in `readmit-reduction-plan/v1`, and reports every group,

  every trial and exactly what it established in `readmit-reduction/v1`. The

  oracle is not trusted: the unreduced sequence must reproduce the signature a

  declared number of times before a group is removed, the sequence that survives

  must reproduce it that many times again, and an oracle that disagrees with

  itself withdraws the result instead of reporting one. Every trial is one

  durable run of a narrowed copy of the selected `readmit-test/v1` spec into its

  own fresh destination, preceded by the explicitly selected

  `readmit-reset-plan/v1`; only a confirmed reset lets a candidate execute, and

  nothing is resumed, retried or resent. A run that timed out, was cancelled,

  errored, did not finish or left a delivery uncertain decides nothing and stops

  the reduction under its own name, and no such state can be declared as the

  signature at all: a timeout is never an equivalent reduced failure. A run that

  also failed an assertion the signature does not name failed differently.

  Messages one declared correlation rule related are one group under

  `group-by-correlation/v1` and are removed together or not at all, the

  occurrences the signature is stated about are pinned and recorded as pinned,

  and equality the rules could not stand behind is reported as

  `ungrouped-collision` rather than grouped anyway. The claim is exact:

  `group-1-minimal` is 1-minimality over the declared grouping as this oracle

  answered and never a global minimum; a spent trial budget reports `bounded`,

  which rules out no removal, and one spent before the surviving sequence was

  re-confirmed says so under its own name; a partition with nothing removable in

  it reports `not_attempted` rather than a vacuous minimum; and an interrupted

  or undecided reduction establishes nothing at all. The report carries no value byte. Nothing is written into

  evidence: no case, no revision, no export and no third derivation name, so

  ADR-0004's closed set is unchanged, and `readmit-reproducer-plan/v1`,

  `readmit-reproducer/v1`, `readmit-transform-plan/v1`,

  `readmit-transform-preview/v1`, `readmit-test/v1`, `readmit-result/v1`,

  `readmit-job/v1` and `readmit-reset-plan/v1` each gain no member. There is no

  `readmit reduce` command in this release, and no global-minimum, field-level,

  de-identification or sharing claim.

- The desktop shell lays one verified case out as a synchronized event sequence

  over the lanes of its declared sources. Every occurrence of every source is in

  one list, in the order the recorded observed times put them; an occurrence the

  case recorded no time for is listed after all of them, in the order its own

  source recorded it, and is never interleaved among them, because sorting an

  unknown time into a position among known ones invents a precision the evidence

  does not have. A lane spans its own source's clock and the panel states that

  no clock is assumed to agree with another, that no offset is corrected, and

  that order is not causality. Each event carries the time the capture observed

  beside the time the message declares, the latter displayed only when its bytes

  can be nothing but a timestamp, exactly as the default `readmit timeline`

  displays the same field. Opening an event selects the original occurrence in

  the inspector and lists everything recorded about it: the case bundle's own

  same-source acknowledgement match, and, where a rules document of the

  workspace is named, the links, collisions and unsupported items of

  `readmit correlate`'s own `readmit-correlation/v1` report over the same

  evidence — observed linkage and inferred linkage stated apart, never blurred.

  There is no default rule set here either, so a sequence asked for without one

  shows what the evidence itself recorded and says that no rule was applied. The

  gaps are the case's own: an unacknowledged message, an acknowledgement that

  resolved to nothing or to more than one thing, and every occurrence with no

  observed or declared time, each counted over the whole case beside the window.

  None of them is explained: a missing acknowledgement stays missing evidence

  rather than becoming proof that none was sent. No contract gains a member,

  nothing is written anywhere, and no value crosses the boundary but a declared

  timestamp.

- The desktop shell **compares two built reproducer revisions**: how they are

  related — the same evidence, one built from the other, both built from one

  case, or neither — what the second plan does differently, and every occurrence

  they retain differently. A setup dependency that stopped being retained is its

  own named outcome, separate from a selection a person stopped making, because a

  reschedule without the booking it refers to may no longer reproduce anything.

  Name the run retained for each revision and each expectation is reported as

  failed on both sides, passed on both sides, a verdict that moved, or one no

  execution reached; a run counts as proof of a revision only when it was

  executed against that revision's derived case, and a revision nobody has run

  claims nothing rather than reading as one that passed. There is no overall

  verdict, and an execution error is never substituted for a surviving failure.

  This compares plans and manifests, never messages: the two derived cases are

  not compared byte for byte, because where one revision edits a position the

  other left alone, the other's bytes there are the original evidence's own value

  and the bytes an edit replaced are recorded nowhere. No contract is added and

  none changes — `readmit-reproducer-plan/v1`, `readmit-reproducer/v1` and

  `readmit-case/v3` gain no member and change no byte, no derived evidence and no

  new derivation name is produced, and the comparison is a typed value the window

  renders rather than a stored document. Only an expectation's identifier, its

  operator and the two verdicts cross the boundary; the values it expected and

  observed do not.



- `message_timeout` bounds the network exchange only. A durable run persists what

  it sent between writing the message and waiting for the acknowledgement, and

  that local durability is no longer charged to the configured window: the

  window moves on by exactly what persistence took. A receiver that answers

  within the budget is acknowledged however slow this sender's storage is, and a

  receiver that does not answer within it is still an uncertain delivery.

  `readmit-target/v3` gains no member and changes no byte.

- `drift LEFT RIGHT` names which of four retained records differ between two

  artifacts, and keeps them apart: the input that went in, the target

  configuration it was sent to, the engine build and spec contract that

  evaluated it, and the profile that evaluation named. Each is read from the one

  record that carries it — a case identity and a run's declared replay

  operators, the retained target record, and the `readmit-engine/v1` pin a

  durable run keeps beside its plan — and each is reported with its own outcome.

  A cause no side retained is `undeclared` and a cause the evidence does not

  settle is `undecided`, with a named reason; neither is agreement. A profile

  identity this release cannot resolve to content stays `undecided` rather than

  becoming no drift, because no pack is extracted and no library is bundled, and

  two equal unresolvable names are not established to be equal rules. A pin this

  build does not read keeps its document fingerprint and is compared raw: the

  same bytes are no drift, different bytes settle neither the environment nor

  the rule. Any unsettled cause makes the whole attribution `undecided`, more

  than one change names all of them and chooses none, and a single named change

  is still not a claim that a verdict moved because of it. The receiving

  application's own revision is always stated `unknown`. `readmit-drift/v1` is a

  new document: `readmit-diff/v1`, `readmit-engine/v1`, `readmit-target/v1` and

  the profile-version contracts gain no member and change no byte, and the field

  comparison `readmit diff` produces is untouched. No address, path, field value

  or message byte appears in a drift report.



- `normalize LEFT RIGHT --policy FILE` runs the same field comparison

  `readmit diff` runs under an authored `readmit-normalization-policy/v1`

  document, and shows what that policy hid. A rule is typed data interpreted by

  a Go operator, never an expression: an `ignore` for a genuinely volatile

  field, a `timestamp` compared to a declared precision, or a `numeric` value

  compared within a declared unsigned tolerance, each scoped to exactly one

  canonical selector with no wildcard and no category. Two rules resolving to

  one selection, a signed tolerance, a parameter on the wrong operator, an

  unknown operator, a repeated id and an unknown member each refuse the whole

  document rather than half-applying it. Every rule is reported with the

  selections it addressed and how each was settled, including a rule that

  addressed nothing, and every difference the comparison found is listed

  whatever a rule said about it: a suppressed one names the rule that

  suppressed it, so what was hidden and by which rule is always answerable. A

  field this build did not decode keeps the raw comparison's own word,

  `uncompared`, is counted apart from the differences and is `undecided`

  whatever any rule says, because an evidence gap is not a difference. A

  rule that cannot read the values it was scoped to is `undecided` with a named

  reason — a value coarser than the precision it is compared at, two declared

  UTC offsets that are not identical, a value absent on one side, text a

  numeric rule was scoped to, or evidence this build did not decode — and that

  is neither agreement nor a reason to conceal the difference. No rule

  suppresses an inserted, missing, ambiguous or unaligned occurrence, and the

  alignment, declared keys and field scope are restated in every report. No

  value appears in any rendering: there is no `--show-values` here and

  `readmit-normalization/v1` has no member one could live in, so a difference is

  a canonical selector, the bundled label and each side's decoded state. Both

  documents are new: `readmit-diff/v1` gains no member, changes no byte, and its

  raw comparison reports every difference a policy suppressed exactly as it did

  before. No outcome here is a verdict.



- The desktop shell reviews and transforms the whole case. It previews a

  `readmit-transform-plan/v1` document over the verified case through the engine

  `readmit transform` runs — every position the plan would rewrite, what happened

  to every declared relation, what the pinned profile pack declares about the

  transformed sequence, and everything left exactly as the evidence has it —

  and it writes nothing at all: no case, no run, no derived bundle, and no

  operator is added to `readmit-reproducer-plan/v1`. Beside it, the window reads

  an export review back through the same verified offline reader the export gate

  uses, groups every located finding by where it is and what kind of content

  it is, so a case's source filenames never collapse into its messages and no

  surface is silently omitted, and states the reviewer's decision as its own named

  outcome: an incomplete review cannot authorize disclosure whatever identity

  approves it, and an approval that does not name the identity the bytes on disk

  have now is stale, because that identity binds the input, the policy, the

  specification and the output. The window records no approval and exports

  nothing, never reads the private mappings, offsets and residual values, and

  labels a reviewed extract as a disclosure-reviewed extract rather than as a

  regression-equivalent packet: equivalence needs evidence from the actual

  external target, and no review or local hash claims certification, Safe Harbor

  status or authentication of the source. No contract gains a member, and no

  value crosses either boundary — a change is a position and a relation number,

  a finding is a location, and reading a transformed value is the inspector over

  the derived case the review names.



- The window proposes regression-test expectations from a run somebody has

  already reviewed, and records none of them without a person's act. A reviewed

  `readmit-result/v1` directory is opened through the reader `readmit test`

  verifies one with; the record count it settled on and the value each

  acknowledgement held at a named MSA or ERR position are proposed, read exactly

  as the evaluator reads them. A run whose own expectations did not hold, one

  that replayed different evidence, and one observed at another boundary are each

  refused by name. A proposal the run cannot justify is its own named outcome

  carrying the reason and no value, never a silent omission and never an

  inclusion. Approving is a separate call: every proposal is reported as

  approved, rejected or not reviewed, a review that decides nothing approves

  nothing, and the proposals are derived from the run again when the decisions

  are applied, so no suggested value crosses back towards the draft. A reviewer

  edits the identifier and the value while approving; `readmit-test/v1` declares

  no explanation and no tolerance member and gains none. The draft also reports

  what its expectations decide and leave undecided, as positions rather than

  values, and a suggestion carries only what the expectation it proposes would

  carry — no ledger record and no message the run sent.

  `readmit-test/v1` and `readmit-test-draft/v1` gain no member and change no

  byte, and nothing is stored: a suggestion set and an approval are typed values

  the window renders.



Download the archive for your OS and architecture and compare its SHA-256 with

`checksums.txt` before extraction. Run `readmit inspect testdata/fixtures/adt-cr.hl7`

from the extracted directory (`.\readmit.exe` on Windows). No Go installation is

needed. See the bundled README for accepted formats, limits, and platform floors.



Inspection and capture remain local and byte-preserving. `listen` is a bounded

local MLLP test fixture for the documented SIU profile, not a production receiver

or general HL7 conformance validator. All bundled fixtures are synthetic.



Prerelease binaries have no Apple notarization or Windows code signing.



- `scenario preview` reads `readmit-order-scenario/v1` ORM/ORU templates with

  immutable placer/filler bindings, repeated textual observations and typed

  fixture status transitions. Positive/negative lifecycle expectations are

  checked without displaying identifiers or values. The existing scenario

  contract is unchanged; templates generate no messages and claim no external

  target or HL7 conformance.



- A separately packaged customer-controlled Go artifact hub stores immutable byte

  objects with PostgreSQL metadata, requires mutual TLS, and provides explicit

  schema migration and verified offline backup/restore. It is an operator-only

  deployment foundation; OIDC/project authorization and team collaboration remain

  separate work. The CLI and desktop remain independent of the service.

- `baseline review`, `approve` and `show`, also available in the desktop inspector,

  retain explicit local approvals of immutable regression expectation revisions.

  Exact spec/parent commitments reject stale approvals; values remain hidden

  until requested. Local reviewer declarations do not authenticate team identity.



- Customer-hosted hub team access validates pinned RFC 9068 access tokens from

  an OIDC provider, enforces project roles and certificate-bound scoped tokens,

  and isolates artifact reads/writes/exports. Metadata v3 and backup/v2 retain

  project links and prevent legacy-route bypass after enabling team mode; old

  backup/v1 remains readable. Execution and approval routes authorize then

  explicitly refuse unsupported operations pending their delivery tickets.



- `report assemble` creates `readmit-retained-packet/v1` from selected actual

  case/spec/current and optional baseline evidence, preserving original bytes

  and provenance. `report verify-retained` verifies it offline, reevaluating

  verdicts and source bindings. Missing baseline stays missing; execution errors

  and uncertain durable lifecycle remain explicit. The packet is customer-local,

  not disclosure-approved, and synthetic report/v1 remains unchanged.





- Portable retained reports: `report export` creates a sealed private review with

  byte-identical retained evidence and consistent inert HTML, PDF, Markdown,

  strict JSON and JUnit. `report review` verifies offline in read-only mode;

  sensitivity and disclosure restrictions remain attached to every format.



- Complete local evaluation: explicit signed v2 trial issuance and one approved extension, mandatory operation admission across CLI/desktop/hub/runner, visible UTC rollback recovery, and ungated evidence access and frozen practice. Production signing/issuer service and commercial approval remain owner gates.
