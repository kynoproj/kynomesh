/*
Copyright 2026 The Kynoproj Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package tls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateX509KeyPair(t *testing.T) {
	pair, err := GenerateX509KeyPair()
	require.NoError(t, err)
	require.NotNil(t, pair)
	require.NotEmpty(t, pair.Certificate, "leaf cert DER must be present")
	require.NotNil(t, pair.PrivateKey, "private key must be present")

	// Parse the leaf cert and assert the structural bits the broker
	// relies on: SANs, validity, server-auth EKU, ECDSA key.
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	require.NoError(t, err)

	assert.Contains(t, leaf.DNSNames, "localhost")
	assert.Equal(t, []string{"Kynoproj"}, leaf.Subject.Organization)
	assert.Contains(t, leaf.ExtKeyUsage, x509.ExtKeyUsageServerAuth)
	assert.True(t, leaf.BasicConstraintsValid)

	// Validity: at least 360 days into the future (a touch of slack for
	// the difference between cert NotBefore and time.Now() in this test).
	assert.WithinDuration(t, time.Now().Add(certValidity), leaf.NotAfter, 5*time.Minute)

	_, ok := pair.PrivateKey.(*ecdsa.PrivateKey)
	assert.True(t, ok, "expected ECDSA private key, got %T", pair.PrivateKey)
}

func TestGenerateX509KeyPair_FreshEachCall(t *testing.T) {
	// Two consecutive calls must produce different serials and different
	// keys — there is no caching layer, and tests upstream rely on a fresh
	// cert per server boot.
	a, err := GenerateX509KeyPair()
	require.NoError(t, err)
	b, err := GenerateX509KeyPair()
	require.NoError(t, err)

	la, err := x509.ParseCertificate(a.Certificate[0])
	require.NoError(t, err)
	lb, err := x509.ParseCertificate(b.Certificate[0])
	require.NoError(t, err)

	assert.NotEqual(t, la.SerialNumber, lb.SerialNumber)
}

func TestPemBlockForKey_ECDSAReturnsECPrivateKeyBlock(t *testing.T) {
	// Direct round-trip on the ECDSA case so the happy path of
	// pemBlockForKey is exercised independently of GenerateX509KeyPair.
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	block := pemBlockForKey(priv)
	require.NotNil(t, block)
	assert.Equal(t, "EC PRIVATE KEY", block.Type)

	parsed, err := x509.ParseECPrivateKey(block.Bytes)
	require.NoError(t, err)
	assert.True(t, priv.Equal(parsed), "round-tripped key must match the original")
}

func TestPemBlockForKey_UnsupportedReturnsNil(t *testing.T) {
	// The function only handles ECDSA today — an RSA key (or anything
	// else) must fall through to the nil branch. Guards against silently
	// accepting a key type that the rest of the package can't round-trip.
	rsaKey, err := rsa.GenerateKey(rand.Reader, 1024)
	require.NoError(t, err)
	assert.Nil(t, pemBlockForKey(rsaKey))
	assert.Nil(t, pemBlockForKey("not a key"))
}

func TestCreateCerts_ErrorsWhenNeitherServerNorClient(t *testing.T) {
	_, _, _, err := CreateCerts("Kynoproj", []string{"localhost"}, time.Now().Add(time.Hour), false, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must specify either server or client")
}

func TestCreateCerts_ServerLeafIsCASignedAndVerifies(t *testing.T) {
	hosts := []string{"localhost"}
	notAfter := time.Now().Add(certValidity)

	keyPEM, certPEM, caCertPEM, err := CreateCerts("Kynoproj", hosts, notAfter, true, false)
	require.NoError(t, err)
	require.NotEmpty(t, keyPEM)
	require.NotEmpty(t, certPEM)
	require.NotEmpty(t, caCertPEM)

	leaf := parseSingleCert(t, certPEM)
	caCert := parseSingleCert(t, caCertPEM)

	// The leaf must validate against the returned CA — proves the signing
	// chain is wired up correctly.
	roots := x509.NewCertPool()
	roots.AddCert(caCert)
	_, err = leaf.Verify(x509.VerifyOptions{
		Roots:       roots,
		DNSName:     "localhost",
		KeyUsages:   []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		CurrentTime: time.Now(),
	})
	require.NoError(t, err, "server leaf must verify against its CA for ServerAuth")

	// Server-only leaf must NOT carry ClientAuth EKU.
	assert.Contains(t, leaf.ExtKeyUsage, x509.ExtKeyUsageServerAuth)
	assert.NotContains(t, leaf.ExtKeyUsage, x509.ExtKeyUsageClientAuth)

	// CA cert must be a CA with the right key usages.
	assert.True(t, caCert.IsCA)
	assert.NotZero(t, caCert.KeyUsage&x509.KeyUsageCertSign, "CA must be allowed to sign certs")

	// Private key PEM round-trips into an RSA key (matches the package
	// guarantee — CreateCerts uses RSA-2048, distinct from GenerateX509KeyPair's ECDSA).
	keyBlock, _ := pem.Decode(keyPEM)
	require.NotNil(t, keyBlock, "private key PEM must decode")
	assert.Equal(t, "RSA PRIVATE KEY", keyBlock.Type)
	_, err = x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	require.NoError(t, err)
}

func TestCreateCerts_ClientLeafCarriesClientAuthEKU(t *testing.T) {
	_, certPEM, _, err := CreateCerts("Kynoproj", []string{"localhost"}, time.Now().Add(time.Hour), false, true)
	require.NoError(t, err)

	leaf := parseSingleCert(t, certPEM)
	assert.Contains(t, leaf.ExtKeyUsage, x509.ExtKeyUsageClientAuth)
	assert.NotContains(t, leaf.ExtKeyUsage, x509.ExtKeyUsageServerAuth)
}

func TestCreateCerts_BothFlagsProduceBothEKUs(t *testing.T) {
	_, certPEM, _, err := CreateCerts("Kynoproj", []string{"localhost"}, time.Now().Add(time.Hour), true, true)
	require.NoError(t, err)

	leaf := parseSingleCert(t, certPEM)
	assert.Contains(t, leaf.ExtKeyUsage, x509.ExtKeyUsageServerAuth)
	assert.Contains(t, leaf.ExtKeyUsage, x509.ExtKeyUsageClientAuth)
}

func parseSingleCert(t *testing.T, pemBytes []byte) *x509.Certificate {
	t.Helper()
	block, _ := pem.Decode(pemBytes)
	require.NotNil(t, block, "expected a PEM block")
	require.Equal(t, "CERTIFICATE", block.Type)
	cert, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)
	return cert
}
