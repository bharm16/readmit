package smartbackend

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha512"
	"crypto/x509"
	"encoding/base64"
	"encoding/json/v2"
	"encoding/pem"
	"math/big"
	"time"

	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/secret"
)

func publicKey(j JWK) (crypto.PublicKey, error) {
	bad := failure(RegistrationMissing)
	dec := base64.RawURLEncoding.DecodeString
	if j.Use != "sig" {
		return nil, bad
	}
	if j.Kty == "RSA" && j.Alg == "RS384" && j.Crv == "" && j.X == "" && j.Y == "" {
		n, e := dec(j.N)
		ex, ee := dec(j.E)
		if e != nil || ee != nil || len(n) < 256 || len(n) > 512 || len(ex) < 1 || len(ex) > 4 || n[0] == 0 || ex[0] == 0 {
			return nil, bad
		}
		modulus := new(big.Int).SetBytes(n)
		if modulus.BitLen() < 2048 || modulus.BitLen() > 4096 {
			return nil, bad
		}
		v := new(big.Int).SetBytes(ex).Int64()
		if v < 3 || v%2 == 0 || v > 1<<31-1 {
			return nil, bad
		}
		return &rsa.PublicKey{N: modulus, E: int(v)}, nil
	}
	if j.Kty == "EC" && j.Alg == "ES384" && j.Crv == "P-384" && j.N == "" && j.E == "" {
		x, ex := dec(j.X)
		y, ey := dec(j.Y)
		if ex != nil || ey != nil || len(x) != 48 || len(y) != 48 {
			return nil, bad
		}
		p := &ecdsa.PublicKey{Curve: elliptic.P384(), X: new(big.Int).SetBytes(x), Y: new(big.Int).SetBytes(y)}
		if !p.Curve.IsOnCurve(p.X, p.Y) {
			return nil, bad
		}
		return p, nil
	}
	return nil, bad
}

type signer struct {
	client  *Client
	now     time.Time
	check   func(context.Context) error
	failure error
}

func (s signer) Identity() string { return s.client.identity }
func (s *signer) Material(ctx context.Context, t networkaction.RuntimeTarget) (networkaction.RuntimeMaterial, error) {
	material, err := s.material(ctx, t)
	s.failure = err
	return material, err
}
func (s *signer) material(ctx context.Context, t networkaction.RuntimeTarget) (networkaction.RuntimeMaterial, error) {
	if s.check(ctx) != nil || t.Check(ctx) != nil {
		return networkaction.RuntimeMaterial{}, failure(AuthorityChanged)
	}
	c := s.client.config
	v, err := (secret.Locator{Command: c.Key.Locator.Command, Arguments: c.Key.Locator.Arguments}).Read(ctx)
	if err != nil {
		return networkaction.RuntimeMaterial{}, failure(KeyUnavailable)
	}
	block, rest := pem.Decode(v.Expose())
	if block == nil || len(rest) != 0 {
		return networkaction.RuntimeMaterial{}, failure(KeyUnavailable)
	}
	var key crypto.Signer
	switch block.Type {
	case "PRIVATE KEY":
		p, e := x509.ParsePKCS8PrivateKey(block.Bytes)
		if e == nil {
			key, _ = p.(crypto.Signer)
		}
	case "RSA PRIVATE KEY":
		key, _ = x509.ParsePKCS1PrivateKey(block.Bytes)
	case "EC PRIVATE KEY":
		key, _ = x509.ParseECPrivateKey(block.Bytes)
	}
	if key == nil {
		return networkaction.RuntimeMaterial{}, failure(KeyUnavailable)
	}
	expected, _ := publicKey(s.client.public)
	a, _ := x509.MarshalPKIXPublicKey(key.Public())
	b, _ := x509.MarshalPKIXPublicKey(expected)
	if networkaction.Digest(a) != networkaction.Digest(b) {
		return networkaction.RuntimeMaterial{}, failure(KeyUnavailable)
	}
	if s.check(ctx) != nil || t.Check(ctx) != nil {
		return networkaction.RuntimeMaterial{}, failure(AuthorityChanged)
	}
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return networkaction.RuntimeMaterial{}, failure(KeyUnavailable)
	}
	header := map[string]string{"alg": c.Algorithm, "kid": c.Key.Kid, "typ": "JWT"}
	if c.Key.JWKSURL != "" {
		header["jku"] = c.Key.JWKSURL
	}
	claims := struct {
		Iss string `json:"iss"`
		Sub string `json:"sub"`
		Aud string `json:"aud"`
		Exp int64  `json:"exp"`
		Iat int64  `json:"iat"`
		Jti string `json:"jti"`
	}{c.ClientID, c.ClientID, c.Audience, s.now.Add(2 * time.Minute).Unix(), s.now.Unix(), base64.RawURLEncoding.EncodeToString(nonce)}
	h, _ := json.Marshal(header)
	p, _ := json.Marshal(claims)
	input := base64.RawURLEncoding.EncodeToString(h) + "." + base64.RawURLEncoding.EncodeToString(p)
	sum := sha512.Sum384([]byte(input))
	var signature []byte
	switch k := key.(type) {
	case *rsa.PrivateKey:
		if c.Algorithm != "RS384" {
			return networkaction.RuntimeMaterial{}, failure(KeyUnavailable)
		}
		signature, err = rsa.SignPKCS1v15(rand.Reader, k, crypto.SHA384, sum[:])
	case *ecdsa.PrivateKey:
		if c.Algorithm != "ES384" || k.Curve != elliptic.P384() {
			return networkaction.RuntimeMaterial{}, failure(KeyUnavailable)
		}
		r, z, e := ecdsa.Sign(rand.Reader, k, sum[:])
		err = e
		if e == nil {
			signature = append(r.FillBytes(make([]byte, 48)), z.FillBytes(make([]byte, 48))...)
		}
	default:
		err = failure(KeyUnavailable)
	}
	if err != nil {
		return networkaction.RuntimeMaterial{}, failure(KeyUnavailable)
	}
	return networkaction.AssertionMaterial([]byte(input + "." + base64.RawURLEncoding.EncodeToString(signature))).WhileAuthorized(s.check), nil
}
