package entitlement

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"slices"
	"strconv"
	"time"
)

const (
	// TrustSchema is the only trust store contract version this release reads.
	TrustSchema = "readmit-entitlement-trust/v1"

	// MaxTrustedKeys bounds one trust store. Rotation adds a key and retires
	// the previous one, so a store holding more than this is a mistake rather
	// than a long-lived vendor.
	MaxTrustedKeys = 64
)

// KeyStatus is the closed set of trust states. An unknown status is refused: a
// status this release cannot interpret is never treated as any other one, and
// never as trusted.
type KeyStatus string

const (
	// KeyActive signs and verifies.
	KeyActive KeyStatus = "active"
	// KeyRetired verifies only what it signed before it was retired. This is
	// what makes rotation possible: a new key takes over issuing while every
	// outstanding licence keeps verifying until it is replaced.
	KeyRetired KeyStatus = "retired"
	// KeyRevoked verifies nothing at all, whenever it was issued. Revocation is
	// how a compromised key is withdrawn, and it reaches a machine only when an
	// updated trust store does.
	KeyRevoked KeyStatus = "revoked"
)

var keyStatuses = []KeyStatus{KeyActive, KeyRetired, KeyRevoked}

// Key is one vendor signing key an organization trusts.
type Key struct {
	ID        string    `json:"key_id"`
	Algorithm string    `json:"algorithm"`
	PublicKey string    `json:"public_key"`
	Status    KeyStatus `json:"status"`
	Retired   time.Time `json:"retired,omitzero"`
	Revoked   time.Time `json:"revoked,omitzero"`
}

// Trust is the set of vendor signing keys this installation accepts. It is an
// explicitly selected file rather than hidden configuration or an environment
// variable, and this release embeds none: the vendor's production signing
// identity is an owner decision made outside the engine.
type Trust struct {
	Schema string `json:"schema"`
	Keys   []Key  `json:"keys"`
}

// DecodeTrust reads a trust store. Unknown members and unknown versions are
// errors; there is no migration and no repair.
func DecodeTrust(data []byte) (Trust, error) {
	trust, err := decode[Trust](data, TrustSchema, trustKind)
	if err != nil {
		return Trust{}, err
	}
	if err := validateTrust(trust); err != nil {
		return Trust{}, err
	}
	return trust, nil
}

// EncodeTrust writes a validated trust store deterministically.
func EncodeTrust(trust Trust) ([]byte, error) {
	if err := validateTrust(trust); err != nil {
		return nil, err
	}
	return encode(trust, trustKind)
}

func validateTrust(trust Trust) error {
	if trust.Schema != TrustSchema {
		return ErrUnsupportedVersion
	}
	if len(trust.Keys) == 0 {
		return errors.New("a trust store declares at least one signing key")
	}
	if len(trust.Keys) > MaxTrustedKeys {
		return errors.New("a trust store declares at most " + strconv.Itoa(MaxTrustedKeys) + " signing keys")
	}
	for i, key := range trust.Keys {
		if err := validateKey(key); err != nil {
			return err
		}
		if i > 0 && trust.Keys[i-1].ID >= key.ID {
			return errors.New("trusted keys must be unique and sorted by identifier")
		}
	}
	return nil
}

func validateKey(key Key) error {
	if err := identifier(key.ID); err != nil {
		return errors.New("signing key identifier: " + err.Error())
	}
	if key.Algorithm != Algorithm {
		return errors.New("unsupported entitlement signature algorithm")
	}
	if _, err := publicKey(key); err != nil {
		return err
	}
	if !slices.Contains(keyStatuses, key.Status) {
		return errors.New("signing key status: not one of active, retired, revoked")
	}
	// The instant a key left service is recorded with the status that names it
	// and with no other, so a retired key always has a retirement to compare an
	// issue time against and a status can never be read from a stray timestamp.
	for _, m := range []struct {
		status  KeyStatus
		member  string
		value   time.Time
		present bool
	}{
		{KeyRetired, "retirement time", key.Retired, !key.Retired.IsZero()},
		{KeyRevoked, "revocation time", key.Revoked, !key.Revoked.IsZero()},
	} {
		if (key.Status == m.status) != m.present {
			return errors.New("a signing key records a " + m.member + " exactly when it is " + string(m.status))
		}
		if m.present {
			if err := instant(m.value); err != nil {
				return errors.New(m.member + ": " + err.Error())
			}
		}
	}
	return nil
}

func publicKey(key Key) (ed25519.PublicKey, error) {
	value, err := base64.StdEncoding.DecodeString(key.PublicKey)
	if err != nil || len(value) != ed25519.PublicKeySize {
		return nil, errors.New("trusted key is not a base64 ed25519 public key")
	}
	return ed25519.PublicKey(value), nil
}

// SigningKey resolves a trusted key by identifier for something signed at an
// instant, refusing an unknown, revoked or already-retired key by name. It is
// exported so a second signed contract of this repository — the vendor's own
// billing events — is authenticated by the same store, the same key states and
// the same algorithm rather than by a second trust mechanism.
func (t Trust) SigningKey(id string, signed time.Time) (Key, ed25519.PublicKey, error) {
	return t.key(id, signed)
}

// key resolves the signing key of a document. A key this store does not name,
// one that was revoked, and one that was already retired when the entitlement
// was issued are each refused by name rather than treated as untrusted alike.
func (t Trust) key(id string, issued time.Time) (Key, ed25519.PublicKey, error) {
	index := slices.IndexFunc(t.Keys, func(k Key) bool { return k.ID == id })
	if index < 0 {
		return Key{}, nil, ErrUnknownKey
	}
	key := t.Keys[index]
	switch key.Status {
	case KeyActive:
	case KeyRetired:
		if !issued.Before(key.Retired) {
			return Key{}, nil, ErrRetiredKey
		}
	case KeyRevoked:
		return Key{}, nil, ErrRevokedKey
	default:
		return Key{}, nil, errors.New("signing key status: not one of active, retired, revoked")
	}
	public, err := publicKey(key)
	if err != nil {
		return Key{}, nil, err
	}
	return key, public, nil
}
