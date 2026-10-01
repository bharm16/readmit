package desktop

import "context"

// The files ChooseEnvironmentFile chooses.
const (
	caCertificateFile       = "ca-certificate"
	clientCertificateFile   = "client-certificate"
	locatorProgramFile      = "locator-program"
	observationInputFile    = "observation-input"
	publicKeysFile          = "public-keys"
	validatorCapabilityFile = "validator-capability"
	isolationRegistryFile   = "isolation-registry"
	connectionExampleFile   = "connection-example"
	exampleValueFile        = "example-file"
	validatorPackageFile    = "validator-package"
)

// ChooseEnvironmentFile presents the host's file dialog for one file an
// environment, credential or observation editor names: a PEM CA certificate
// ("ca-certificate") or client certificate ("client-certificate"), a
// credential locator program ("locator-program"), or an observation's export
// file ("observation-input"), a connection example to import
// ("connection-example") or a file one of its placeholders names
// ("example-file"), or a validator package folder to install
// ("validator-package"). Choosing reads and writes nothing; the editor's one
// Save, or the import, validates what was chosen.
func (a *App) ChooseEnvironmentFile(kind string) PathChoiceResult {
	return run(a, true, false, func(ctx context.Context) PathChoiceResult {
		title, filter, pattern := "", "All files (*.*)", "*.*"
		switch kind {
		case publicKeysFile:
			title, filter, pattern = "Choose the registered public keys", "JSON public keys (*.json)", "*.json"
		case validatorCapabilityFile, validatorPackageFile:
			title := "Choose the installed validator capability"
			if kind == validatorPackageFile {
				title = "Choose the validator package"
			}
			folder, declined := a.chooseFolder(ctx, title)
			if folder == "" {
				return PathChoiceResult{State: declined.state, Reason: declined.reason}
			}
			return PathChoiceResult{State: Completed, Kind: kind, Paths: []string{folder}}
		case isolationRegistryFile:
			title, filter, pattern = "Choose the authorized fixture adapters", "JSON adapter registry (*.json)", "*.json"
		case connectionExampleFile:
			title, filter, pattern = "Import example", "Connection examples (*.json)", "*.json"
		case exampleValueFile:
			title = "Choose a file"
		case caCertificateFile:
			title, filter, pattern = "Choose a CA certificate", "PEM certificates (*.pem *.crt)", "*.pem;*.crt"
		case clientCertificateFile:
			title, filter, pattern = "Choose a client certificate", "PEM certificates (*.pem *.crt)", "*.pem;*.crt"
		case locatorProgramFile:
			title = "Choose the locator program"
		case observationInputFile:
			title = "Choose the export file"
		default:
			return PathChoiceResult{State: Failed, Reason: "unknown environment file kind"}
		}
		files, declined := a.chooseFiles(ctx, title, filter, pattern)
		if len(files) == 0 {
			return PathChoiceResult{State: declined.state, Reason: declined.reason}
		}
		return PathChoiceResult{State: Completed, Kind: kind, Paths: files[:1]}
	})
}
