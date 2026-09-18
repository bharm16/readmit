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
	"strconv"
	"strings"
	"time"
)

// ReadTarget performs bounded local reads only. Relative CA paths are resolved
// against the target file, never the caller's working directory.
func ReadTarget(path string) (Target, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return Target{}, errors.New("cannot resolve target configuration")
	}
	path = resolved
	data, err := readLocal(path, 64<<10)
	if err != nil {
		return Target{}, errors.New("cannot read target configuration")
	}
	var target Target
	if err := json.Unmarshal(data, &target, json.RejectUnknownMembers(true)); err != nil {
		return Target{}, errors.New("invalid target configuration JSON")
	}
	if target.CAFile != "" && !filepath.IsAbs(target.CAFile) {
		target.CAFile = filepath.Join(filepath.Dir(path), target.CAFile)
	}
	if err := validateTarget(target); err != nil {
		return Target{}, err
	}
	return target, nil
}

func validateTarget(t Target) error {
	if t.Schema != TargetSchema || !t.TestEndpoint {
		return errors.New("target must use readmit-target/v1 and explicitly mark test_endpoint true")
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

func loadCA(t Target) ([]byte, error) {
	if t.CAFile == "" {
		return nil, nil
	}
	data, err := readLocal(t.CAFile, 1<<20)
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
