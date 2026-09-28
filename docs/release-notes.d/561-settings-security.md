- Settings → General now shows the saved theme, text size and local reviewer
  name, and keeps them between sessions (#561). Edit previews a theme or text
  size as you choose it; Cancel puts the saved values back. Text size now
  offers 175%. The reviewer name labels your local review decisions; it is not
  a sign-in. About shows the application's version, the build's source
  revision and time, whether it was built with local changes, and its release
  channel (development preview, unsigned). Check update opens Storage's staged
  update; with no project open, it first asks which project to prepare the
  update for.
- Settings → Security lists the connections this computer has configured —
  environments, observation sources, the team hub, the runner you last set up
  and the customer portal — with each one's actual state: Active while
  something reaches it, Connected only for a live team hub session, and
  Checked with its time after an explicit check. Opening Security contacts nothing. A connection's details
  show where it goes and what it may carry, with Edit and, for a connected
  hub, Disconnect; Add connection opens the environment, source, team or
  runner setup. After you save, Add connection and Edit return to Security
  with that connection open, and the team, runner and customer portal setups
  return there too. A run, send, reset, collection, capture or runner
  enrollment in progress is listed by the environment, source, runner, test or
  case it reaches, not by a generic activity name. Privacy's Edit names the
  project it applies to and sets a case's saved search fields, how they are
  stored and until when (or indefinitely), and Clear saved searches removes
  the project's saved views. In high-contrast (forced colours) modes, the
  focused Settings entries and icon buttons show a visible outline.
- Encryption controls moved to Settings → Security → Encryption: a list with
  each control's storage declaration, state, generation and rotation, one
  sheet to add or edit a control, and Check control, Record rotation, Export
  control and Retire control. Changing a control's key program is recorded as
  a rotation. Stored key-program arguments are counted, never shown; Replace
  arguments starts blank. Encrypted packages are under Encryption's menu; a
  package is refused, with nothing written, when its control's key was
  rotated after you chose the control, so choose it again.
