package desktop

import "github.com/bharm16/readmit/internal/operationguard"

// profiles declares, for every facade operation that runs under a name, the
// profile it runs under (runNamed): the name the privacy status discloses it
// by, whether a panel's cancel control can stop it, and the admission it takes
// before its work starts. Every execution is here, so every one is admitted,
// bounded, rechecked before each job it queues and settled by the operation
// guard, as the command line admits the same operation. The capability
// ledger's rows for these operations state exactly these admissions, and the
// facade's tests read the ledger against this table.
//
// A connectivity check and a queued or single run take execution admission
// alone, as their commands do. A capture, which collects from a saved source,
// a fixture reset and an observation collection also admit the
// author, which their commands do not, and so do runner enrollment, which its
// command leaves to the hub, and runner execution, whose jobs the runner
// admits one by one.
var profiles = map[string]operationguard.Profile{
	// Runs, practice and the proofs that send to their own loopback receivers.
	"StartDurableRun":           {Name: runOperation, Interruptible: true, Execution: operationguard.Execute},
	"ResumeDurableRun":          {Name: runOperation, Interruptible: true, Execution: operationguard.Execute},
	"StartSuiteRun":             {Name: runOperation, Interruptible: true, Execution: operationguard.Execute},
	"SendReplay":                {Name: replayOperation, Interruptible: true, Execution: operationguard.Execute},
	"ReexecuteReviewedEvidence": {Name: reexecutionOperation, Interruptible: true, Execution: operationguard.Execute},
	"RunPractice":               {Name: practiceOperation, Interruptible: true},
	"DeriveExportReview":        {Name: privacyOperation, Interruptible: true, Author: true},
	"ExportDerivedPacket":       {Name: privacyOperation, Interruptible: true},
	"GenerateSyntheticPacket":   {Name: syntheticPacketOperation, Interruptible: true},

	// The customer runner.
	"EnrollRunner":     {Name: "runner-enrollment", Author: true},
	"ExecuteRunnerJob": {Name: runnerOperation, Interruptible: true, Author: true, Execution: operationguard.ExecuteEachJob},

	// Sources and capture.
	"StartCapture": {Name: captureOperation, Interruptible: true, Author: true, Execution: operationguard.Execute},
	// The built-in SIU fixture of the library's sample data, stopped by the
	// capture's own cancel.
	"StartSampleFixture": {Name: captureOperation, Interruptible: true, Author: true, Execution: operationguard.Execute},

	// The environment and observation.
	"CheckTarget":                 {Name: targetCheckOperation, Interruptible: true, Execution: operationguard.Execute},
	"CheckEnvironment":            {Name: targetCheckOperation, Interruptible: true, Execution: operationguard.Execute},
	"ResetTarget":                 {Name: targetResetOperation, Interruptible: true, Author: true, Execution: operationguard.Execute},
	"EvaluateSendPolicy":          {Name: sendPolicyOperation},
	"CheckEnvironmentDestination": {Name: sendPolicyOperation},
	"PreviewReplay":               {Name: replayPreviewOperation, Interruptible: true},
	"StartReduction":              {Name: reductionOperation, Interruptible: true, Execution: operationguard.Execute},
	"CollectObservation":          {Name: "observation", Interruptible: true, Author: true, Execution: operationguard.Execute},

	// The customer hub. A lifecycle command that only exports takes no
	// author admission, posted or reconciled (postHubLifecycle).
	"DiagnoseHub":              {Name: hubRequestOperation},
	"ConnectHub":               {Name: hubRequestOperation},
	"StartHubAuth":             {Name: hubSignInStartOperation},
	"CompleteHubAuth":          {Name: hubSignInOperation, Interruptible: true},
	"HubStatus":                {Name: hubRequestOperation},
	"ListHubProjectArtifacts":  {Name: hubRequestOperation},
	"DownloadHubArtifact":      {Name: hubRequestOperation},
	"UploadHubArtifact":        {Name: hubRequestOperation, Author: true},
	"ListHubReviews":           {Name: hubRequestOperation},
	"ReadHubTeam":              {Name: hubRequestOperation},
	"ReconcileTeamTransfer":    {Name: hubRequestOperation},
	"ListHubMembers":           {Name: hubRequestOperation},
	"ListHubReviewers":         {Name: hubRequestOperation},
	"DownloadHubFile":          {Name: hubRequestOperation},
	"DownloadHubSummary":       {Name: hubRequestOperation},
	"ReadHubSupportSummary":    {Name: hubRequestOperation},
	"PreviewHubRetention":      {Name: hubRequestOperation},
	"ApplyHubRetention":        {Name: hubRequestOperation, Author: true},
	"OpenHubConflict":          {Name: hubRequestOperation},
	"SearchHubReviews":         {Name: hubRequestOperation},
	"ListHubNotifications":     {Name: hubRequestOperation},
	"SearchHubNotifications":   {Name: hubRequestOperation},
	"PostHubReview":            {Name: hubRequestOperation, Author: true},
	"SuiteReviewers":           {Name: hubRequestOperation},
	"PostHubSupportReview":     {Name: hubRequestOperation, Author: true},
	"ListHubLifecycle":         {Name: hubRequestOperation},
	"PostHubLifecycle":         {Name: hubRequestOperation, Author: true},
	"ReconcileHubOfflineDraft": {Name: hubRequestOperation, Author: true},
	"DownloadHubExport":        {Name: hubRequestOperation},
	"ConnectOperatorHub":       {Name: hubRequestOperation},
	"ReadOperatorHubArtifact":  {Name: hubRequestOperation},
	"StoreOperatorHubArtifact": {Name: hubRequestOperation, Author: true},
	"ListSchedules":            {Name: hubRequestOperation},
	"CommandSchedule":          {Name: hubRequestOperation, Author: true},

	// Credential references and protection, which run declared programs.
	"TestSecretReference":      {Name: secretTestOperation, Interruptible: true},
	"CheckCredential":          {Name: secretTestOperation, Interruptible: true},
	"RecordCredentialRotation": {Name: secretRotationOperation, Interruptible: true, Author: true},
	"RotateSecretReference":    {Name: secretRotationOperation, Interruptible: true, Author: true},
	"ScanSecrets":              {Name: secretScanOperation, Interruptible: true},
	"RotateProtectionControl":  {Name: protectOperation, Interruptible: true, Author: true},
	"UpdateProtectionControl":  {Name: protectOperation, Interruptible: true, Author: true},
	"CheckProtectionControl":   {Name: protectOperation, Interruptible: true},
	"PackProtectedPackage":     {Name: protectOperation, Interruptible: true},
	"OpenProtectedPackage":     {Name: protectOperation, Interruptible: true},

	// Local work a panel names so its own cancel control stops it.
	"PreviewImport":            {Name: importOperation, Interruptible: true},
	"ProbeImport":              {Name: importOperation, Interruptible: true},
	"ImportCase":               {Name: importOperation, Interruptible: true, Author: true},
	"RetryCaptureFinalization": {Name: importOperation, Interruptible: true, Author: true},
	"GenerateCorpus":           {Name: corpusOperation, Interruptible: true, Author: true},
	"ScanCorpus":               {Name: corpusOperation, Interruptible: true},
	"GenerateInput":            {Name: corpusOperation, Interruptible: true, Author: true},
	"StartBenchmark":           {Name: corpusOperation, Interruptible: true},
	"ExplainRun":               {Name: explanationOperation, Interruptible: true},
	"CompareRuns":              {Name: runComparisonOperation, Interruptible: true},
	"PublishSupportSummary":    {Name: supportOperation, Interruptible: true},
	"VerifyCIGate":             {Name: ciGateVerifyOperation, Interruptible: true},
	"CheckScenarioLibrary":     {Name: scenarioCheckOperation, Interruptible: true},
	"GenerateScenarioCases":    {Name: scenarioCasesOperation, Interruptible: true, Author: true},
	"SuiteCoverage":            {Name: suiteCoverageOperation, Interruptible: true},

	// The Findings view's analysis and comparison, which write into the
	// project only when an analysis is made or a comparison saved.
	"AnalyzeCase":         {Name: analysisOperation, Interruptible: true, Author: true},
	"FindSimilarFindings": {Name: analysisOperation, Interruptible: true},
}
