package help

// article is one stored article. Its members are this package's own and are
// copied out by Read, so a caller can never change what Help says.
type article struct {
	id      string
	title   string
	kind    Kind
	steps   []string
	body    []string
	action  *Action
	related []string
}

// taskOrder is the order Help lists its five tasks in.
var taskOrder = []string{"import-messages", "investigate-a-case", "create-a-regression-test", "connect-a-test-system", "share-a-report"}

// articles is every article this build ships, by identity. The five task
// articles are the locked initial copy: their titles, steps, actions and
// related links change only with that copy.
var articles = index(
	article{
		id: "import-messages", title: "Import messages", kind: Task,
		steps: []string{
			"Open a project and choose Import.",
			"Choose files, a folder or a ZIP, or paste messages.",
			"Check the detected format. Change it when the preview does not match your input.",
			"Review the messages and choose Import. The new case opens automatically.",
		},
		action:  &Action{ID: StartImport, Label: "Start import"},
		related: []string{"supported-formats", "unparsed-messages"},
	},
	article{
		id: "investigate-a-case", title: "Investigate a case", kind: Task,
		steps: []string{
			"Open a case to read its messages.",
			"Use Search or Filter to narrow the list.",
			"Select a message to open Fields, Raw or Hex.",
			"Use Timeline for event relationships and Findings for analysis results.",
		},
		action:  &Action{ID: OpenCases, Label: "Open cases"},
		related: []string{"message-timestamps", "missing-evidence", "hidden-values"},
	},
	article{
		id: "create-a-regression-test", title: "Create a regression test", kind: Task,
		steps: []string{
			"Select messages in a case and choose Create test, or choose New test in Tests.",
			"Name the test and choose its environment and outcome.",
			"Add the checks and any required observation or reset setup.",
			"Review the definition and choose Create test.",
			"Choose Run from the saved test to review its destination before Send.",
		},
		action:  &Action{ID: NewTest, Label: "New test"},
		related: []string{"ack-checks", "observation-results", "reset-setup"},
	},
	article{
		id: "connect-a-test-system", title: "Connect a test system", kind: Task,
		steps: []string{
			"Open Environments and choose Add environment.",
			"Enter the address, classification and transport settings.",
			"Save the environment, then choose Test connection.",
			"Add an observation when the test needs downstream records.",
		},
		action:  &Action{ID: AddEnvironment, Label: "Add environment"},
		related: []string{"tls-certificates", "credential-references", "production-refusal"},
	},
	article{
		id: "share-a-report", title: "Share a report", kind: Task,
		steps: []string{
			"Create a report from a saved run or open an existing report.",
			"Choose Share and select the contents and destination.",
			"Set the required redaction treatments and resolve outstanding items.",
			"Inspect the actual output in Preview.",
			"Choose Export for a local file or Send for the displayed remote destination.",
		},
		action:  &Action{ID: OpenReports, Label: "Open reports"},
		related: []string{"original-evidence", "redaction-review", "encryption"},
	},

	article{
		id: "supported-formats", title: "Supported formats", kind: Troubleshooting,
		body: []string{
			"An import reads files, folders and ZIP archives that are already on this computer. Other archive types, such as tar and 7z, are not read, and an archive inside a folder or another archive is imported as an ordinary file rather than expanded.",
			"Every import is read under one format: the framing (one message per file, MLLP frames, or a batch divided at each MSH segment or at the header segments of an HL7 batch file), the segment terminator (CR, LF or CR LF), the encoding (UTF-8, US-ASCII, ISO-8859-1 or unknown) and the direction (inbound, outbound or unknown). The format shown before you choose Import is the one the import uses.",
			"The format is checked wherever the bytes can contradict it. An input that could be read more than one way is refused rather than guessed: MLLP framing on a file with no MLLP start block, a terminator that does not reach every message header, or UTF-8 or US-ASCII on bytes that are not valid in that encoding. Nothing is written; change the format to the one the input actually uses and preview again.",
			"Bytes are kept exactly as they were read. Nothing is transcoded, repaired or normalized, and no message value, including MSH-18, decides the encoding. ISO-8859-1 and unknown make no claim that any value can be decoded.",
			"One import holds at most 128 sources, 10,000 messages, 16 MiB per source and 64 MiB of original evidence. An input past a limit is refused, never imported in part. Benchmarks in Tools reads a larger file to see what it holds and what reading it costs, without importing it.",
		},
		related: []string{"import-messages", "unparsed-messages"},
	},
	article{
		id: "unparsed-messages", title: "Unparsed messages", kind: Troubleshooting,
		body: []string{
			"A message whose format was declared correctly but whose content is broken is still imported. A truncated MLLP frame, a payload that is not a parsable message, a batch envelope segment, or an input in which the declared boundary finds no message is kept with all of its bytes and marked unparsed.",
			"Nothing is repaired, dropped or resynchronized inside a broken frame. An unparsed message keeps a short diagnostic of why it could not be parsed, and its original bytes stay readable in Raw and Hex.",
			"An unparsed message has no fields, so a search, filter or check that reads a field does not match it.",
			"When every message of an input is unparsed, the input probably uses another framing or terminator. Import it again under the format it actually uses; the first case is kept as it was.",
		},
		related: []string{"import-messages", "supported-formats"},
	},
	article{
		id: "message-timestamps", title: "Message timestamps", kind: Troubleshooting,
		body: []string{
			"A message can carry three different times. They mean different things, and one is never used in place of another.",
			"Message time is MSH-7 as the sender declared it. It is shown literally, with its original precision and offset, and is not converted or checked as a calendar date. Empty, null, omitted and unparsed values stay separate unknowns.",
			"Observed time is when the message was seen. It is known only when it was supplied explicitly, as a capture supplies it. Otherwise it is unknown, even when a message time or an import time exists.",
			"Import time is when the import ran: one time for the whole import. A file's creation, modification or arrival time is never used as a message time.",
			"The timeline lists messages in the order of their source and their position in it. It does not invent an order across sources from their times.",
		},
		related: []string{"investigate-a-case", "missing-evidence"},
	},
	article{
		id: "missing-evidence", title: "Missing evidence", kind: Troubleshooting,
		body: []string{
			"Only a completed observation can show that something is absent, and only within the source, scope and window it recorded.",
			"A collector that was disabled, returned stale data, lost its connection, reached a limit or received an ambiguous answer also observed nothing, but that is not evidence of absence. Each is reported as what happened — incomplete, missing, ambiguous, stale, truncated, unsupported, failed, cancelled or timed out — and a check that depends on it is an execution error, not a pass or a failure.",
			"Unknown and unsupported are never a pass, and a timeout is not a negative result from the receiving system.",
			"A message that is not in a case may simply predate the capture or import. Collect the missing evidence and run again rather than asserting that it never existed.",
		},
		related: []string{"investigate-a-case", "observation-results"},
	},
	article{
		id: "hidden-values", title: "Hidden values", kind: Troubleshooting,
		body: []string{
			"Message values are hidden until you choose to show them. Positions, states, labels and byte offsets are shown without the values.",
			"Show values reveals the values of the open case or file only, and resets when another is opened. Expected and observed values in a run result, and the values a replay preview changes, are revealed the same way, by one deliberate action each time.",
			"A revealed value is shown in this window and is not saved.",
			"Hidden values do not make the window safe to capture or share. Anything you reveal is visible to anyone who can see the screen, and a screenshot keeps it.",
		},
		related: []string{"investigate-a-case", "redaction-review"},
	},
	article{
		id: "ack-checks", title: "ACK checks", kind: Troubleshooting,
		body: []string{
			"An ACK check reads the acknowledgement the receiving system sent back for a message: its acknowledgement code and the control ID it acknowledges.",
			"An AA acknowledgement says only that the receiver accepted the message at that stage of the protocol. It does not show that the receiver stored, applied or forwarded anything, so AA alone is not enough to show that a regression is fixed.",
			"To check what happened downstream, add an observation of the downstream records to the test and check what it observed.",
		},
		related: []string{"create-a-regression-test", "observation-results"},
	},
	article{
		id: "observation-results", title: "Observation results", kind: Troubleshooting,
		body: []string{
			"An observation reads downstream records during a declared window after a test sends, from the source the observation declares.",
			"Complete is the only trustworthy status: every sample was an observation, and the records held still for the declared quiet period before the deadline. Only a complete observation carries a record count.",
			"Every other status is an execution error that names the earliest point at which the observation stopped being trustworthy: incomplete (the deadline passed before the records held still), missing, ambiguous, stale (records from before the window came back), truncated, unsupported, failed, cancelled or timed out.",
			"An observation that did not complete never becomes a passing check that something is absent.",
		},
		related: []string{"create-a-regression-test", "missing-evidence"},
	},
	article{
		id: "reset-setup", title: "Reset setup", kind: Troubleshooting,
		body: []string{
			"A reset returns a test environment to the starting state a test declares. It runs reviewed actions in order and records what each one established.",
			"A reset runs only against an environment classified as nonproduction. Production and unclassified environments are refused before any file is read or connection opened, and an action that opens a connection is held to the same approved destinations as a send.",
			"A reset passes only when every action is confirmed. An unconfirmed, failed, refused or cancelled reset is an execution error, never a failed check: a fixture that did not reset says nothing about whether the expectation was wrong. An action after one that stopped is recorded as not attempted.",
		},
		related: []string{"create-a-regression-test", "production-refusal"},
	},
	article{
		id: "tls-certificates", title: "TLS certificates", kind: Troubleshooting,
		body: []string{
			"TLS 1.2 is the minimum and TLS 1.3 is permitted. Certificate chain and server name verification always run; there is no setting that turns them off.",
			"A CA file replaces the system's trusted roots for that environment. The server name is the name the certificate is verified against when the address reaches the system by IP address or through a tunnel; without one, the address's host is used.",
			"Test connection reports the negotiated version and cipher suite, the verified server name, whether a client certificate was requested and presented, and each certificate's subject, issuer and validity. Expiry is reported, never acted on: a certificate already outside its validity fails like any other untrusted certificate.",
			"A client certificate's private key is a credential. It is named through a credential reference and read from its store only for the connection.",
		},
		related: []string{"connect-a-test-system", "credential-references"},
	},
	article{
		id: "credential-references", title: "Credential references", kind: Troubleshooting,
		body: []string{
			"Readmit holds no credentials. A credential reference records what a credential is for, what it may be presented to, how to read it from an operating system credential store or a secret provider you run, and when it was last rotated.",
			"There is no field, prompt or file through which a credential value enters Readmit, and nothing displays one. Environments, runs and reports carry the reference, never the value.",
			"Check credential runs the store's own program to confirm the credential can be read, without showing it. Record the rotation when you change the credential in its store.",
		},
		related: []string{"connect-a-test-system", "tls-certificates"},
	},
	article{
		id: "production-refusal", title: "Production refusal", kind: Troubleshooting,
		body: []string{
			"Readmit refuses to send to an environment classified as production. The refusal happens while a send is prepared, before anything is sent, and applies to every test run and replay. A reset refuses production and unclassified environments the same way.",
			"A classification is a claim somebody recorded, not something Readmit verified, so it can only refuse. Nonproduction grants nothing on its own: what a send may reach is decided against the addresses the environment resolves to at the moment of the send and the destinations its send policy approves. Unclassified is refused rather than read as nonproduction.",
			"The refusal has no override. Run the test against a nonproduction test system.",
		},
		related: []string{"connect-a-test-system", "reset-setup"},
	},
	article{
		id: "original-evidence", title: "Original evidence", kind: Troubleshooting,
		body: []string{
			"Imported and captured messages are kept exactly as they were read. A case stores every source byte for byte, including framing and malformed parts, with a SHA-256 digest of each message and of each source, and every read verifies them before anything is shown.",
			"Nothing in Readmit edits original evidence. Redaction, transformation and export write new, separate output and leave the originals unchanged.",
			"A matching digest shows the bytes are unchanged since they were stored. It does not show who produced them or that their content is clinically correct.",
			"Original evidence can hold patient data. Share reviewed, derived output rather than the originals.",
		},
		related: []string{"share-a-report", "redaction-review"},
	},
	article{
		id: "redaction-review", title: "Redaction review", kind: Troubleshooting,
		body: []string{
			"Redaction applies explicit, named treatments to write a separate derived case and test. The originals are unchanged, and the link from original to replacement values stays in a private folder that is never shared.",
			"The review covers 18 categories of identifiers. A category no treatment covers stays unassessed, and a field treatment covers only the locations it lists. Every outstanding item must be resolved before the output can be approved.",
			"The check for known remaining values searches their raw, JSON-escaped and base64 forms. It cannot find every encoding, unknown identifiers, clinical context, images or inference risks, so a clean check never resolves an outstanding item.",
			"Date shifting is a testing treatment: it keeps precision and can keep what dates imply about age.",
			"Readmit does not label any output de-identified or compliant with any standard, and approving it is not evidence of its legal status.",
		},
		related: []string{"share-a-report", "original-evidence"},
	},
	article{
		id: "encryption", title: "Encryption", kind: Troubleshooting,
		body: []string{
			"Readmit encrypts packages with a key that stays in an operating system credential store or a key provider you run. It holds no key material: an encryption control records a reference to the key and when it was rotated.",
			"Without the referenced key, an encrypted package cannot be read at rest or in transit, and an altered package is refused rather than decrypted.",
			"Encryption does not show who wrote a package and protects nothing from someone who holds the key. It does not remove identifiers: a packaged message still holds every identifier it held. The files a package was made from stay on disk unchanged, and opening a package writes plaintext protected only by the volume and its file permissions.",
			"Encryption establishes no legal status or certification.",
		},
		related: []string{"share-a-report", "credential-references"},
	},
)

func index(list ...article) map[string]article {
	byID := make(map[string]article, len(list))
	for _, stored := range list {
		if _, repeated := byID[stored.id]; repeated {
			panic("help: article " + stored.id + " is declared twice")
		}
		byID[stored.id] = stored
	}
	return byID
}
