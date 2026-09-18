package replay_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/replay"
)

// environmentTarget is the named-environment form of the same endpoint: a
// readmit-target/v3 configuration carrying the classification, the TLS server
// name and the client certificate that version adds.
func environmentTarget(address string) replay.Target {
	config := target(address)
	config.Schema = replay.TargetSchemaV3
	config.Name = "lab-siu"
	config.Classification = replay.Nonproduction
	config.Transport = "tls"
	config.ServerName = "lab.example.invalid"
	config.ClientCertificate = "client.pem"
	config.Credential = replay.Credential{SecretsFile: "secrets.json", Reference: "lab-mllp"}
	return config
}

// A readmit-target/v3 configuration names the environment it describes and
// records the class of that environment where every reader can see it.
func TestTargetV3RecordsANamedEnvironmentAndItsClassification(t *testing.T) {
	directory := t.TempDir()
	credentialStore(t, directory, "127.0.0.1:2575")
	path := writeTarget(t, directory, environmentTarget("127.0.0.1:2575"))
	loaded, err := replay.ReadTarget(path)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if loaded.Name != "lab-siu" || loaded.Environment() != replay.Nonproduction {
		t.Fatalf("named environment %q classified %q", loaded.Name, loaded.Environment())
	}
	if loaded.ServerName != "lab.example.invalid" {
		t.Fatalf("server name %q", loaded.ServerName)
	}
	if !filepath.IsAbs(loaded.ClientCertificate) || filepath.Base(loaded.ClientCertificate) != "client.pem" ||
		filepath.Dir(loaded.ClientCertificate) != filepath.Dir(loaded.Credential.SecretsFile) {
		t.Fatalf("the client certificate was not anchored to the target file: %q", loaded.ClientCertificate)
	}
	production := environmentTarget("127.0.0.1:2575")
	production.Classification = replay.Production
	classified, err := replay.ReadTarget(writeTarget(t, freshStore(t, "127.0.0.1:2575"), production))
	if err != nil {
		t.Fatalf("a production-classified environment must be configurable and visible: %v", err)
	}
	if classified.Environment() != replay.Production {
		t.Fatalf("classification %q", classified.Environment())
	}
}

// A version with no classification member reads as unclassified. An absent
// claim is never read as a nonproduction one.
func TestTargetWithoutAClassificationReadsAsUnclassified(t *testing.T) {
	directory := t.TempDir()
	previous, err := replay.ReadTarget(writeTarget(t, directory, target("127.0.0.1:2575")))
	if err != nil {
		t.Fatalf("readmit-target/v1: %v", err)
	}
	if previous.Environment() != replay.Unclassified || previous.Name != "" {
		t.Fatalf("readmit-target/v1 classified %q", previous.Environment())
	}
	second := t.TempDir()
	credentialStore(t, second, "127.0.0.1:2575")
	carried, err := replay.ReadTarget(writeTarget(t, second, credentialTarget("127.0.0.1:2575")))
	if err != nil {
		t.Fatalf("readmit-target/v2: %v", err)
	}
	if carried.Environment() != replay.Unclassified {
		t.Fatalf("readmit-target/v2 classified %q", carried.Environment())
	}
}

// Every member readmit-target/v3 introduces is refused on the versions that
// never declared it, and v3 itself refuses an absent or unknown classification.
func TestTargetRefusesEnvironmentMembersOutsideTheirVersionOrSet(t *testing.T) {
	const versioned = "require readmit-target/v3"
	for name, build := range map[string]func(*testing.T) (replay.Target, string){
		"name on the first version": func(*testing.T) (replay.Target, string) {
			config := target("127.0.0.1:2575")
			config.Name = "lab-siu"
			return config, versioned
		},
		"classification on the first version": func(*testing.T) (replay.Target, string) {
			config := target("127.0.0.1:2575")
			config.Classification = replay.Nonproduction
			return config, versioned
		},
		"server name on the credential version": func(*testing.T) (replay.Target, string) {
			config := credentialTarget("127.0.0.1:2575")
			config.Transport = "tls"
			config.ServerName = "lab.example.invalid"
			return config, versioned
		},
		"client certificate on the credential version": func(*testing.T) (replay.Target, string) {
			config := credentialTarget("127.0.0.1:2575")
			config.Transport = "tls"
			config.ClientCertificate = "client.pem"
			return config, versioned
		},
		"no classification at all": func(*testing.T) (replay.Target, string) {
			config := environmentTarget("127.0.0.1:2575")
			config.Classification = ""
			return config, "requires an explicit classification"
		},
		"a classification outside the set": func(*testing.T) (replay.Target, string) {
			config := environmentTarget("127.0.0.1:2575")
			config.Classification = replay.Classification("staging")
			return config, "requires an explicit classification"
		},
		"no environment name": func(*testing.T) (replay.Target, string) {
			config := environmentTarget("127.0.0.1:2575")
			config.Name = ""
			return config, "requires a name for the environment"
		},
		"server name without tls": func(*testing.T) (replay.Target, string) {
			config := environmentTarget("127.0.0.1:2575")
			config.Transport = "plain"
			config.ClientCertificate = ""
			return config, "require tls transport"
		},
		"client certificate without tls": func(*testing.T) (replay.Target, string) {
			config := environmentTarget("127.0.0.1:2575")
			config.Transport = "plain"
			config.ServerName = ""
			return config, "require tls transport"
		},
		"client certificate without its private key": func(*testing.T) (replay.Target, string) {
			config := environmentTarget("127.0.0.1:2575")
			config.Credential = replay.Credential{}
			return config, "private key"
		},
		"a server name that is not a host name": func(*testing.T) (replay.Target, string) {
			config := environmentTarget("127.0.0.1:2575")
			config.ServerName = "lab example/invalid"
			return config, "server name must be a host name"
		},
	} {
		config, want := build(t)
		directory := freshStore(t, "127.0.0.1:2575")
		_, err := replay.ReadTarget(writeTarget(t, directory, config))
		if err == nil {
			t.Errorf("%s was accepted", name)
			continue
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%s was refused as %q, want %q", name, err, want)
		}
	}
}

// A recorded configuration is one readmit reads back unchanged, and a
// configuration it would refuse is never written at all.
func TestWriteTargetRecordsOnlyAConfigurationItReadsBack(t *testing.T) {
	directory := freshStore(t, "127.0.0.1:2575")
	path := filepath.Join(directory, "environment.json")
	config := environmentTarget("127.0.0.1:2575")
	config.ClientCertificate = ""
	if err := replay.WriteTarget(path, config); err != nil {
		t.Fatalf("write: %v", err)
	}
	loaded, err := replay.ReadTarget(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if loaded.Name != config.Name || loaded.Environment() != config.Classification || loaded.ServerName != config.ServerName {
		t.Fatalf("recorded %+v, read back %+v", config, loaded)
	}
	edited := config
	edited.Classification = replay.Production
	if err := replay.WriteTarget(path, edited); err != nil {
		t.Fatalf("replace: %v", err)
	}
	if replaced, err := replay.ReadTarget(path); err != nil || replaced.Environment() != replay.Production {
		t.Fatalf("the replaced configuration read back as %+v: %v", replaced, err)
	}
	refused := config
	refused.Classification = replay.Classification("staging")
	fresh := filepath.Join(directory, "refused.json")
	if err := replay.WriteTarget(fresh, refused); err == nil {
		t.Fatal("a configuration outside the classification set was written")
	}
	if _, err := os.Lstat(fresh); err == nil {
		t.Fatal("a refused configuration left a file behind")
	}
	if err := os.WriteFile(path+".incomplete", []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := replay.WriteTarget(path, config); err == nil || !strings.Contains(err.Error(), "interrupted write is retained") {
		t.Fatalf("a retained interrupted write was overwritten: %v", err)
	}
}

// freshStore is one temporary directory holding a secret reference document
// scoped to address, so each case above reads its own target file.
func freshStore(t *testing.T, address string) string {
	t.Helper()
	directory := t.TempDir()
	credentialStore(t, directory, address)
	return directory
}
