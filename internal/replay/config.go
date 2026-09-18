package replay

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/secret"
)

// ReadDeclaredTarget reads and validates one configuration exactly as it is
// written: every path it declares is left as the file declares it. An editor
// reads a configuration this way, so rewriting one cannot turn a relative
// reference into a path that resolves on one machine only.
//
// Validation runs before anything is anchored. Anchoring an absent member would
// turn it into the target file's own directory, and a refusal would then name
// the wrong member.
func ReadDeclaredTarget(path string) (Target, error) {
	target, _, err := readDeclared(path)
	return target, err
}

// readDeclared also hands back the physical path it read, so a caller that has
// to anchor the configuration's declared references resolves the file once.
func readDeclared(path string) (Target, string, error) {
	resolved, err := artifactpath.Resolve(path)
	if err != nil {
		return Target{}, "", errors.New("cannot resolve target configuration")
	}
	data, err := readLocal(resolved, 64<<10)
	if err != nil {
		return Target{}, "", errors.New("cannot read target configuration")
	}
	var target Target
	if err := json.Unmarshal(data, &target, json.RejectUnknownMembers(true)); err != nil {
		return Target{}, "", errors.New("invalid target configuration JSON")
	}
	if err := validateTarget(target); err != nil {
		return Target{}, "", err
	}
	return target, resolved, nil
}

// ReadTarget performs bounded local reads only. Relative CA, client certificate
// and secrets paths are resolved against the target file, never the caller's
// working directory, and the credential reference is bound to this target's own
// purpose and address before the configuration is handed back.
func ReadTarget(path string) (Target, error) {
	target, resolved, err := readDeclared(path)
	if err != nil {
		return Target{}, err
	}
	target = anchor(target, filepath.Dir(resolved))
	if _, err := BindCredential(target); err != nil {
		return Target{}, err
	}
	return target, nil
}

// anchor resolves the paths a configuration declares against the directory the
// configuration itself lives in, never the caller's working directory. The
// declared form is what the file holds; this is what reading it means.
func anchor(t Target, directory string) Target {
	if t.CAFile != "" {
		t.CAFile = artifactpath.JoinReference(directory, t.CAFile)
	}
	if t.ClientCertificate != "" {
		t.ClientCertificate = artifactpath.JoinReference(directory, t.ClientCertificate)
	}
	if t.Credential.Declared() {
		t.Credential.SecretsFile = artifactpath.JoinReference(directory, t.Credential.SecretsFile)
	}
	return t
}

// VerifiedServerName is the name a target's certificate is verified against:
// the server name it declares, or the host of its address when it declares
// none. One function owns the rule, so a diagnosis and a replay reading the
// same configuration verify the same name rather than two different ones.
func VerifiedServerName(t Target) string {
	if t.ServerName != "" {
		return t.ServerName
	}
	host, _, _ := net.SplitHostPort(t.Address)
	return host
}

// BindCredential returns the secret reference a target declares, bound to this
// target's own purpose and address. A reference scoped to another endpoint is
// refused rather than presented here, and no value is read: binding settles
// what a credential may be used for, never what it is.
func BindCredential(t Target) (secret.Reference, error) {
	if !t.Credential.Declared() {
		return secret.Reference{}, nil
	}
	document, err := secret.ReadStore(t.Credential.SecretsFile)
	if err != nil {
		return secret.Reference{}, err
	}
	return secret.Bind(document, t.Credential.Reference, secret.MLLPEndpoint, t.Address)
}

// WriteTarget records a target configuration at path. It validates before
// writing, so a configuration readmit could not read back is never produced,
// and it writes the file in full to a new owner-only file renamed onto the
// destination artifactpath returned, so a reader never observes a partial
// configuration and a failed write leaves the previous one exactly as it was.
// The incomplete file must not exist, so an interrupted write is reported
// rather than overwritten.
func WriteTarget(path string, t Target) error {
	if err := validateTarget(t); err != nil {
		return err
	}
	data, err := json.Marshal(t, json.Deterministic(true))
	if err != nil {
		return errors.New("cannot encode the target configuration")
	}
	data = append(data, '\n')
	// Both files this writes are reserved by artifactpath, and the file it
	// renames onto is the destination artifactpath itself returned, so neither
	// path is derived from the other.
	destination, err := artifactpath.Destination(path)
	if err != nil {
		return errors.New("cannot write a target configuration here")
	}
	// A declared credential is bound against the directory this configuration
	// will be read from, before anything is written, so an editor never records
	// a configuration that reading it back would refuse. No value is read to do
	// it: binding settles what a credential may be used for, never what it is.
	if _, err := BindCredential(anchor(t, filepath.Dir(destination))); err != nil {
		return err
	}
	incomplete, err := artifactpath.Destination(path + ".incomplete")
	if err != nil {
		return errors.New("cannot write a target configuration here; an interrupted write may be retained beside it")
	}
	file, err := os.OpenFile(incomplete, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("cannot create the new target configuration; an interrupted write is retained")
	}
	_, writeErr := file.Write(data)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		os.Remove(incomplete)
		return errors.New("cannot write the new target configuration")
	}
	if err := os.Rename(incomplete, destination); err != nil {
		os.Remove(incomplete)
		return errors.New("cannot replace the target configuration")
	}
	return nil
}

func validateTarget(t Target) error {
	if t.Schema != TargetSchema && t.Schema != TargetSchemaV2 && t.Schema != TargetSchemaV3 || !t.TestEndpoint {
		return errors.New("target must use readmit-target/v1, readmit-target/v2 or readmit-target/v3 and explicitly mark test_endpoint true")
	}
	// A member is never added to a version already released. A credential
	// reference belongs to readmit-target/v2 alone, so a v1 target that carries
	// one is refused rather than read as though v1 had always allowed it.
	if t.Credential.Declared() {
		if t.Schema == TargetSchema {
			return errors.New("a credential reference requires readmit-target/v2 or readmit-target/v3")
		}
		if t.Credential.SecretsFile == "" || t.Credential.Reference == "" {
			return errors.New("a credential reference names both a secrets document and a reference in it")
		}
	}
	if err := validateEnvironment(t); err != nil {
		return err
	}
	host, port, err := net.SplitHostPort(t.Address)
	p, portErr := strconv.Atoi(port)
	if err != nil || host == "" || len(host) > 253 || strings.ContainsAny(host, " /%\\") || portErr != nil || p < 1 || p > 65535 {
		return errors.New("target requires an explicit host and numeric port")
	}
	for _, r := range t.Address {
		if r < 33 || r > 126 {
			return errors.New("target address must contain printable ASCII without spaces")
		}
	}
	if t.Transport != "plain" && t.Transport != "tls" || t.Transport == "plain" && t.CAFile != "" {
		return errors.New("target transport must be plain or verified tls; CA requires tls")
	}
	// Do not resolve names to decide whether approval is necessary. Even a name
	// commonly used for loopback requires explicit approval, avoiding DNS trust.
	ip := net.ParseIP(host)
	if (ip == nil || !ip.IsLoopback()) && !t.ApprovedTransport {
		return errors.New("nonloopback targets and hostnames require approved_transport true")
	}
	for _, value := range []string{t.ConnectTimeout, t.MessageTimeout} {
		d, err := time.ParseDuration(value)
		if err != nil || d <= 0 || d > 5*time.Minute {
			return errors.New("target timeouts must be positive durations at most five minutes")
		}
	}
	if t.MaxACKBytes < 1 || t.MaxACKBytes > 1<<20 {
		return errors.New("max_ack_bytes must be between 1 and 1048576")
	}
	return nil
}

// validateEnvironment checks the members readmit-target/v3 introduced. They are
// refused outright on the versions that never declared them: a member is never
// added to a released version, so a v1 or v2 configuration carrying one is a
// configuration written against a contract it does not declare.
func validateEnvironment(t Target) error {
	if t.Schema != TargetSchemaV3 {
		if t.Name != "" || t.Classification != "" || t.ServerName != "" || t.ClientCertificate != "" {
			return errors.New("a named environment, classification, server name and client certificate require readmit-target/v3")
		}
		return nil
	}
	if err := environmentName(t.Name); err != nil {
		return err
	}
	// An absent classification is refused rather than defaulted. The default a
	// reader would have to pick is the claim itself, and readmit does not make
	// that claim on an operator's behalf.
	if !slices.Contains(classifications, t.Classification) {
		return errors.New("readmit-target/v3 requires an explicit classification: nonproduction, production or unclassified")
	}
	if t.Transport != "tls" && (t.ServerName != "" || t.ClientCertificate != "") {
		return errors.New("a server name and a client certificate require tls transport")
	}
	if t.ServerName != "" {
		if len(t.ServerName) > 253 || strings.ContainsAny(t.ServerName, " /%\\") {
			return errors.New("the TLS server name must be a host name")
		}
		for _, r := range t.ServerName {
			if r < 33 || r > 126 {
				return errors.New("the TLS server name must be a host name in printable ASCII")
			}
		}
	}
	// The certificate is configuration and the private key is a credential, so
	// the key is named through the same reference every other credential uses
	// and is never a path readmit stores beside the configuration.
	if t.ClientCertificate != "" && !t.Credential.Declared() {
		return errors.New("a client certificate requires a credential reference naming its private key")
	}
	return nil
}

// environmentName bounds the one operator-chosen label readmit prints back.
func environmentName(name string) error {
	if name == "" || len(name) > 64 {
		return errors.New("readmit-target/v3 requires a name for the environment of at most 64 bytes")
	}
	for _, r := range name {
		if r != '-' && r != '_' && r != '.' && (r < '0' || r > '9') && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			return errors.New("the name for the environment holds letters, digits, '-', '_' and '.' only")
		}
	}
	return nil
}

// LoadCA returns the bytes of the explicitly configured CA file a target
// declares, or nothing when it declares none and the system roots apply. The
// contract's owner reads the member so every caller applies one bound and one
// refusal to it.
func LoadCA(t Target) ([]byte, error) {
	if t.CAFile == "" {
		return nil, nil
	}
	path, err := artifactpath.Resolve(t.CAFile)
	if err != nil {
		return nil, errors.New("cannot read configured CA certificates")
	}
	data, err := readLocal(path, 1<<20)
	if err != nil {
		return nil, errors.New("cannot read configured CA certificates")
	}
	if !x509.NewCertPool().AppendCertsFromPEM(data) {
		return nil, errors.New("configured CA file contains no certificates")
	}
	return data, nil
}

func targetRecord(t Target, ca []byte) TargetRecord {
	record := TargetRecord{Address: t.Address, Transport: t.Transport, TestEndpoint: t.TestEndpoint, ApprovedTransport: t.ApprovedTransport, ConnectTimeout: t.ConnectTimeout, MessageTimeout: t.MessageTimeout, MaxACKBytes: t.MaxACKBytes}
	if len(ca) > 0 {
		record.CASHA256 = digest(ca)
	}
	return record
}

func readLocal(path string, maxBytes int) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("input must be a regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("input must be a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(f, int64(maxBytes)+1))
	if err != nil || len(data) > maxBytes {
		return nil, errors.New("input cannot be read within size limit")
	}
	return data, nil
}

func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

// RefusedConnection reports whether err is this platform's connection-refused
// error. It is exported so a diagnostic reaching the same endpoints names a
// refusal exactly as a replay does, rather than keeping a second copy of the
// platform detail that Winsock and POSIX disagree on.
func RefusedConnection(err error) bool { return connectionRefused(err) }
