package desktop

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/hubclient"
	"github.com/bharm16/readmit/internal/operation"
)

// The hub panel's operator-only mode (#312). A hub served without an access
// policy is an opaque digest-addressed store: GET and PUT /v1/artifacts/{digest}
// under mutual TLS, with no identity provider, no project and no sign-in, so
// the panel's team mode can never reach it. This mode reaches it through the
// same hubclient transport, on an explicit act for every read and store: the
// person chooses the operator's configuration, connects, and names each
// artifact by its digest or chooses the file to store. A read is written only
// once its bytes hash to the digest asked for, into a new file the person
// names in the save dialog, with the custody notice; a store is admitted
// against this computer's license before anything is chosen or sent, and the
// hub admits it again through its own operation policy. The selection and the
// connection last while the window is open and are never remembered.

const (
	operatorConfigTitle = "Choose the operator-only hub configuration"
	operatorStoreTitle  = "Choose the file to store in the operator-only hub"
	operatorSaveTitle   = "Name the file to save the artifact as"

	operatorNotConnected = "not connected to the operator-only hub"
)

// ChooseOperatorHubConfig presents the host's file dialog for the
// configuration of an operator-only hub (readmit-hub-operator-client/v1).
// Choosing reads that one local file and reaches no hub; a choice that does
// not validate, or a dismissed dialog, leaves the selection as it was. A new
// choice ends any connection the previous one made.
func (a *App) ChooseOperatorHubConfig() HubResult {
	return run(a, true, false, func(ctx context.Context) HubResult {
		files, declined := a.chooseFiles(ctx, operatorConfigTitle, "Hub configuration", "*.json")
		if len(files) == 0 {
			return HubResult{State: declined.state, Reason: declined.reason}
		}
		if len(files) != 1 {
			return HubResult{State: Failed, Reason: "choose one operator-only hub configuration file"}
		}
		cfg, err := hubclient.ReadOperatorConfig(files[0])
		if err != nil {
			return HubResult{State: Failed, Reason: err.Error()}
		}
		a.hubMu.Lock()
		a.hubOperatorConfigPath = files[0]
		a.hubOperatorConfig = &cfg
		a.hubOperatorClient = nil
		a.hubMu.Unlock()
		return HubResult{State: Completed, ConfigPath: files[0], HubURL: cfg.Hub}
	})
}

// ConnectOperatorHub resolves the selected configuration's client identity,
// connects to the operator-only hub over mutual TLS and checks its health
// probes. Connecting reads and stores nothing.
func (a *App) ConnectOperatorHub() HubResult {
	return runNamed[HubResult, *HubResult](a, profiles["ConnectOperatorHub"], func(ctx context.Context) HubResult {
		a.hubMu.Lock()
		cfg, path := a.hubOperatorConfig, a.hubOperatorConfigPath
		a.hubMu.Unlock()
		if cfg == nil {
			return HubResult{State: Failed, Reason: "no operator-only hub configuration selected"}
		}
		client, err := hubclient.NewOperator(ctx, *cfg)
		if err != nil {
			return HubResult{State: Failed, Reason: fmt.Sprintf("cannot create mutual TLS client: %v", err), ConfigPath: path, HubURL: cfg.Hub}
		}
		if err := client.CheckHealth(ctx); err != nil {
			return HubResult{State: Failed, Reason: fmt.Sprintf("hub connection failed: %v", err), ConfigPath: path, HubURL: cfg.Hub}
		}
		a.hubMu.Lock()
		a.hubOperatorClient = client
		a.hubMu.Unlock()
		return HubResult{State: Completed, Connected: true, ConfigPath: path, HubURL: cfg.Hub, CustodyWarning: custodyNotice}
	})
}

// DisconnectOperatorHub ends the operator-only connection and presents the
// custody notice: copies already read stay where they were saved.
func (a *App) DisconnectOperatorHub() HubResult {
	return run(a, false, false, func(context.Context) HubResult {
		a.hubMu.Lock()
		a.hubOperatorClient = nil
		path, hubURL := a.hubOperatorConfigPath, ""
		if a.hubOperatorConfig != nil {
			hubURL = a.hubOperatorConfig.Hub
		}
		a.hubMu.Unlock()
		return HubResult{State: Completed, ConfigPath: path, HubURL: hubURL, CustodyWarning: custodyNotice}
	})
}

// ReadOperatorHubArtifact reads the artifact an operator-only hub stores
// under digest into a new file the person names in the host's save dialog.
// A digest that is not whole, a name already taken or one inside evidence is
// refused before anything is asked of the hub, and the bytes are written,
// owner-only and exclusively, only once they hash to the digest; otherwise
// nothing is written.
func (a *App) ReadOperatorHubArtifact(digest string) HubTransferResult {
	return runNamed[HubTransferResult, *HubTransferResult](a, profiles["ReadOperatorHubArtifact"], func(ctx context.Context) HubTransferResult {
		client := a.operatorHubClient()
		if client == nil {
			return HubTransferResult{State: Failed, Reason: operatorNotConnected}
		}
		if err := hubclient.CheckDigest(digest); err != nil {
			return HubTransferResult{State: Failed, Reason: err.Error()}
		}
		named, declined := a.chooseDestination(ctx, operatorSaveTitle)
		if named == "" {
			if declined.state == Cancelled {
				declined.reason = "no file was named"
			}
			return HubTransferResult{State: declined.state, Reason: declined.reason}
		}
		destination, err := artifactpath.Destination(named)
		if err != nil {
			return HubTransferResult{State: Failed, Reason: err.Error()}
		}
		if _, err := os.Lstat(destination); !errors.Is(err, fs.ErrNotExist) {
			return HubTransferResult{State: Failed, Reason: "a file is already there; name a new file for the artifact"}
		}
		body, err := client.ReadArtifact(ctx, digest)
		if err != nil {
			if errors.Is(err, hubclient.ErrOperatorAccessRefused) {
				return HubTransferResult{State: PermissionDenied, Reason: err.Error(), TransferState: "permission_denied"}
			}
			return HubTransferResult{State: Failed, Reason: err.Error(), TransferState: "failed"}
		}
		if err := operation.WriteNewFile(destination, body, "cannot create the artifact file; nothing was written", "cannot write the artifact file; nothing was kept"); err != nil {
			return HubTransferResult{State: Failed, Reason: err.Error(), TransferState: "failed"}
		}
		return HubTransferResult{State: Completed, TransferState: "completed", Digest: digest, Size: int64(len(body)), Path: destination, Warning: custodyNotice}
	})
}

// StoreOperatorHubArtifact stores the file the person chooses in the host's
// file dialog under its SHA-256 digest in the operator-only hub. It is
// authoring: this computer's license admits the author before any dialog
// opens or anything is sent, and the hub admits the store again through the
// author its operation policy binds to this client certificate. An answer
// that never arrived leaves the store uncertain, never completed; storing
// the same file again is safe.
func (a *App) StoreOperatorHubArtifact() HubTransferResult {
	return runNamed[HubTransferResult, *HubTransferResult](a, profiles["StoreOperatorHubArtifact"], func(ctx context.Context) HubTransferResult {
		client := a.operatorHubClient()
		if client == nil {
			return HubTransferResult{State: Failed, Reason: operatorNotConnected}
		}
		files, declined := a.chooseFiles(ctx, operatorStoreTitle, "All files (*.*)", "*.*")
		if len(files) == 0 {
			return HubTransferResult{State: declined.state, Reason: declined.reason}
		}
		if len(files) != 1 {
			return HubTransferResult{State: Failed, Reason: "choose one file to store"}
		}
		source, err := artifactpath.Resolve(files[0])
		if err != nil {
			return HubTransferResult{State: Failed, Reason: err.Error()}
		}
		body, err := operation.ReadInputFile(source, hubclient.MaxArtifactBytes)
		if err != nil {
			return HubTransferResult{State: Failed, Reason: err.Error()}
		}
		digest, err := client.StoreArtifact(ctx, body)
		switch {
		case errors.Is(err, hubclient.ErrOperatorStoreRefused):
			return HubTransferResult{State: PermissionDenied, Reason: err.Error(), TransferState: "permission_denied"}
		case errors.Is(err, hubclient.ErrStoreUncertain):
			return HubTransferResult{State: Failed, Reason: err.Error(), TransferState: "uncertain"}
		case err != nil:
			return HubTransferResult{State: Failed, Reason: err.Error(), TransferState: "failed"}
		}
		return HubTransferResult{State: Completed, TransferState: "completed", Digest: digest, Size: int64(len(body)), Path: source}
	})
}

func (a *App) operatorHubClient() *hubclient.OperatorClient {
	a.hubMu.Lock()
	defer a.hubMu.Unlock()
	return a.hubOperatorClient
}
