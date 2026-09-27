- Tests is now a list of the project's saved tests with their case, latest
  compatible result (— when none has run) and update time, with Search, Filter
  and Library (#553). New test, a case's Create test and a confirmed finding
  open one editor — Setup, Checks, Review — that saves the whole test once with
  Create test; nothing is saved per field and nothing runs on creation. Checks
  are listed and edited in their own sheets: Record count (explicit zero kept),
  Exact records (every identifier part, in order, or No records) and ACK field
  (Present, Empty, Null or Not present). Suggest checks shows a passing run's
  proposals undecided and adds only the ones you accept. A saved test opens on
  Setup with Checks and History (versions and the runs of each); Edit saves a
  new version and refuses one changed since you opened it; Run opens the run
  review. Duplicate, Export test, Import test, Edit JSON and Details are in its
  menu.
