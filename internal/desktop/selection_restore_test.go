package desktop_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/operationguard"
	"github.com/bharm16/readmit/internal/testlicense"
)

func TestUnreadableOperationSelectionIsReportedUntilExplicitRecovery(t *testing.T) {
	const refusal = "the remembered operation selection cannot be read; choose an activation folder again"
	for _, tc := range []struct {
		name string
		data string
	}{
		{"unknown member", `{"schema":"readmit-desktop-operation-selection/v1","policy":"/unused","activate":true}`},
		{"unsupported version", `{"schema":"readmit-desktop-operation-selection/v2","policy":"/unused"}`},
		{"relative path", `{"schema":"readmit-desktop-operation-selection/v1","policy":"operation-policy.json"}`},
		{"missing policy", `{"schema":"readmit-desktop-operation-selection/v1"}`},
		{"invalid JSON", `operation-policy.json`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			selection := filepath.Join(t.TempDir(), "operations.json")
			if err := os.WriteFile(selection, []byte(tc.data), 0600); err != nil {
				t.Fatal(err)
			}
			app := freshApp(t, &queueChooser{}, selection)
			if got := app.OperationStatus(); got.State != desktop.Failed || got.Reason != refusal || got.Selected {
				t.Fatalf("unreadable selection disappeared: %+v", got)
			}
			if got := string(mustRead(t, selection)); got != tc.data {
				t.Fatalf("startup replaced unreadable selection: %q", got)
			}
			policy := testlicense.New(t)
			if got := app.SelectOperationPolicy(policy); got.State != desktop.Completed || !got.Selected {
				t.Fatalf("explicit choice did not recover: %+v", got)
			}
			if got := app.OperationStatus(); got.State != desktop.Completed || got.Reason != "" || !got.Selected {
				t.Fatalf("status did not recover: %+v", got)
			}
			if got := freshApp(t, &queueChooser{}, selection).OperationStatus(); got.State != desktop.Completed || got.Reason != "" || !got.Selected {
				t.Fatalf("next window did not restore explicit choice: %+v", got)
			}
		})
	}
}

func TestUnreadableCommercialSelectionIsReportedUntilExplicitRecovery(t *testing.T) {
	const refusal = "the remembered commercial selection cannot be read; choose a destinations file again"
	for _, tc := range []struct {
		name string
		data string
	}{
		{"unknown member", `{"schema":"readmit-desktop-commercial-selection/v1","config":"/unused","open":true}`},
		{"unsupported version", `{"schema":"readmit-desktop-commercial-selection/v2","config":"/unused"}`},
		{"relative path", `{"schema":"readmit-desktop-commercial-selection/v1","config":"destinations.json"}`},
		{"missing config", `{"schema":"readmit-desktop-commercial-selection/v1"}`},
		{"invalid JSON", `destinations.json`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			selection := filepath.Join(root, "operations.json")
			remembered := filepath.Join(root, "commercial.json")
			if err := os.WriteFile(remembered, []byte(tc.data), 0600); err != nil {
				t.Fatal(err)
			}
			destinations := filepath.Join(root, "destinations.json")
			if err := os.WriteFile(destinations, []byte(`{"schema":"readmit-commercial-destinations/v1","environment":"sandbox","portal":"https://sandbox-portal.example.test/checkouts"}`), 0600); err != nil {
				t.Fatal(err)
			}
			app := freshApp(t, &queueChooser{batches: [][]string{{destinations}}}, selection)
			if got := app.CommercialStatus(); got.State != desktop.Failed || got.Reason != refusal || got.Portal != "" || got.ConfigPath != "" {
				t.Fatalf("unreadable selection disappeared: %+v", got)
			}
			if got := string(mustRead(t, remembered)); got != tc.data {
				t.Fatalf("startup replaced unreadable selection: %q", got)
			}
			if got := app.ChooseCommercialDestinations(); got.State != desktop.Completed || !strings.HasPrefix(got.Portal, "https://") {
				t.Fatalf("explicit choice did not recover: %+v", got)
			}
			if got := app.CommercialStatus(); got.State != desktop.Completed || got.Reason != "" || got.Portal == "" {
				t.Fatalf("status did not recover: %+v", got)
			}
			if got := freshApp(t, &queueChooser{}, selection).CommercialStatus(); got.State != desktop.Completed || got.Reason != "" || got.Portal == "" {
				t.Fatalf("next window did not restore explicit choice: %+v", got)
			}
		})
	}
}

func TestUnreadableSelectionFileKindsAreReported(t *testing.T) {
	root := t.TempDir()
	selection := filepath.Join(root, "operations.json")
	if err := os.Mkdir(selection, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "commercial.json"), 0700); err != nil {
		t.Fatal(err)
	}
	app := freshApp(t, &queueChooser{}, selection)
	if got := app.OperationStatus(); got.State != desktop.Failed || !strings.Contains(got.Reason, "remembered operation selection cannot be read") {
		t.Fatalf("operation directory was silently ignored: %+v", got)
	}
	if got := app.CommercialStatus(); got.State != desktop.Failed || !strings.Contains(got.Reason, "remembered commercial selection cannot be read") {
		t.Fatalf("commercial directory was silently ignored: %+v", got)
	}
}

func TestInstalledLicenseDoesNotHideAnUnreadableOperationSelection(t *testing.T) {
	root := t.TempDir()
	selection := filepath.Join(root, "operations.json")
	if err := os.WriteFile(selection, []byte(`{"schema":"readmit-desktop-operation-selection/v2","policy":"/unused"}`), 0600); err != nil {
		t.Fatal(err)
	}
	license := filepath.Join(root, "license")
	if err := os.Mkdir(license, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(operationguard.InstalledPolicyIn(license), []byte("invalid policy"), 0600); err != nil {
		t.Fatal(err)
	}
	app := desktop.NewWithInstalledLicense(&queueChooser{}, desktop.ShellDocuments{Folder: root}, license)
	if got := app.OperationStatus(); got.State != desktop.Failed || !strings.Contains(got.Reason, "remembered operation selection cannot be read") {
		t.Fatalf("installed license hid the remembered selection refusal: %+v", got)
	}
}
