package githubapptoken

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCreateInstallationToken(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/app/installations/42/access_tokens" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			t.Fatal("request did not include a bearer token")
		}
		_, _ = w.Write([]byte(`{"token":"ghs_test_token"}`))
	}))
	defer server.Close()

	token, err := CreateInstallationToken(context.Background(), TokenRequest{
		AppID: "123", InstallationID: "42", PrivateKey: key, APIURL: server.URL,
		Timeout: time.Second,
	})
	if err != nil || token != "ghs_test_token" {
		t.Fatalf("CreateInstallationToken() = %q, %v", token, err)
	}
}

func TestClientCachesAndRefreshesToken(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, _ = w.Write([]byte(`{"token":"ghs_cached_token","expires_at":"2099-01-01T00:00:00Z"}`))
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{
		TokenRequest: TokenRequest{AppID: "123", InstallationID: "42", PrivateKey: key, APIURL: server.URL},
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := client.Token(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := client.Token(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first != second || requests != 1 {
		t.Fatalf("tokens = %q, %q; requests = %d, want one request", first, second, requests)
	}
	if _, err := client.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want two after refresh", requests)
	}
}

func TestClientRequiresExpiration(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"token":"ghs_token"}`))
	}))
	defer server.Close()
	client, err := NewClient(ClientConfig{
		TokenRequest: TokenRequest{AppID: "123", InstallationID: "42", PrivateKey: key, APIURL: server.URL},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Token(context.Background()); err == nil || !strings.Contains(err.Error(), "expiration") {
		t.Fatalf("Token() error = %v, want expiration error", err)
	}
}

func TestClientRejectsTokenThatExpiresTooSoon(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expiresAt := time.Now().Add(30 * time.Second).UTC().Format(time.RFC3339)
		_, _ = w.Write([]byte(`{"token":"ghs_token","expires_at":"` + expiresAt + `"}`))
	}))
	defer server.Close()
	client, err := NewClient(ClientConfig{
		TokenRequest: TokenRequest{AppID: "123", InstallationID: "42", PrivateKey: key, APIURL: server.URL},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Token(context.Background()); err == nil || !strings.Contains(err.Error(), "expires too soon") {
		t.Fatalf("Token() error = %v, want expiry error", err)
	}
}

func TestCreateJWT(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0)
	token, err := createJWT("123", key, now)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(token, ".")
	signingInput := strings.Join(parts[:2], ".")
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(signingInput))
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, digest[:], signature); err != nil {
		t.Fatal(err)
	}
	var claims struct {
		Issuer string `json:"iss"`
		Issued int64  `json:"iat"`
		Expiry int64  `json:"exp"`
	}
	claimsData, _ := base64.RawURLEncoding.DecodeString(parts[1])
	if err := json.Unmarshal(claimsData, &claims); err != nil {
		t.Fatal(err)
	}
	if claims.Issuer != "123" || claims.Issued != now.Add(-time.Minute).Unix() || claims.Expiry != now.Add(jwtLifetime).Unix() {
		t.Fatalf("unexpected claims: %+v", claims)
	}
}

func TestParsePrivateKey(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemKey := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	loaded, err := ParsePrivateKey(string(pemKey))
	if err != nil || loaded.N.Cmp(key.N) != 0 {
		t.Fatalf("ParsePrivateKey() failed: %v", err)
	}
}
