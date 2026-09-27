package smartbackend_test

import (
	"context"
	"encoding/base64"
	"encoding/json/v2"
	"testing"

	"github.com/bharm16/readmit/internal/smartbackend"
)

func TestSMARTRegisteredRSAMinimumIsActualBitLength(t *testing.T) {
	f := newFixture(t, "RS384")
	raw, _ := json.Marshal(f.config)
	var weak smartbackend.Config
	if err := json.Unmarshal(raw, &weak); err != nil {
		t.Fatal(err)
	}
	modulus, err := base64.RawURLEncoding.DecodeString(weak.Key.JWKS.Keys[0].N)
	if err != nil {
		t.Fatal(err)
	}
	if len(modulus) != 256 || modulus[0]&0x80 == 0 {
		t.Fatal("fixture is not an actual 2048-bit control")
	}
	// The encoded length stays 256 bytes, but the significant modulus is 2047 bits.
	modulus[0] = 0x7f
	weak.Key.JWKS.Keys[0].N = base64.RawURLEncoding.EncodeToString(modulus)
	raw, _ = json.Marshal(weak)
	if _, err := smartbackend.Prepare(raw, f.policy, nil); err == nil {
		t.Fatal("2047-bit registration accepted")
	}
	if _, _, err := f.client.Session(f.authority, nil).Execute(context.Background(), f.plan("GET", "/Patient/fixture"), f.authority, nil); err != nil {
		t.Fatal("2048-bit positive control refused", err)
	}
}
