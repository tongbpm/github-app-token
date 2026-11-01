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
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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
		if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
			t.Errorf("Accept = %q", got)
		}
		if got := r.Header.Get("X-GitHub-Api-Version"); got != "2022-11-28" {
			t.Errorf("X-GitHub-Api-Version = %q", got)
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

func TestClientConcurrentRefreshesUseOneRequest(t *testing.T) {
	key := testKey(t)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		time.Sleep(25 * time.Millisecond)
		_, _ = w.Write([]byte(`{"token":"ghs_shared_token","expires_at":"2099-01-01T00:00:00Z"}`))
	}))
	defer server.Close()
	client, err := NewClient(ClientConfig{
		TokenRequest: TokenRequest{AppID: "123", InstallationID: "42", PrivateKey: key, APIURL: server.URL},
	})
	if err != nil {
		t.Fatal(err)
	}

	const callers = 20
	tokens := make(chan string, callers)
	errs := make(chan error, callers)
	var group sync.WaitGroup
	for range callers {
		group.Add(1)
		go func() {
			defer group.Done()
			token, err := client.Token(context.Background())
			if err != nil {
				errs <- err
				return
			}
			tokens <- token
		}()
	}
	group.Wait()
	close(tokens)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	for token := range tokens {
		if token != "ghs_shared_token" {
			t.Fatalf("token = %q", token)
		}
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("requests = %d, want 1", got)
	}
}

func TestClientWaitingCallerHonorsContext(t *testing.T) {
	key := testKey(t)
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		_, _ = w.Write([]byte(`{"token":"ghs_token","expires_at":"2099-01-01T00:00:00Z"}`))
	}))
	defer server.Close()
	client, err := NewClient(ClientConfig{
		TokenRequest: TokenRequest{AppID: "123", InstallationID: "42", PrivateKey: key, APIURL: server.URL},
	})
	if err != nil {
		t.Fatal(err)
	}
	firstDone := make(chan error, 1)
	go func() {
		_, err := client.Token(context.Background())
		firstDone <- err
	}()
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Token(ctx); err != context.Canceled {
		t.Fatalf("Token() error = %v, want context.Canceled", err)
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
}

func TestClientRefreshAndInvalidate(t *testing.T) {
	key := testKey(t)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		number := requests.Add(1)
		_, _ = w.Write([]byte(`{"token":"ghs_token_` + strconv.FormatInt(int64(number), 10) + `","expires_at":"2099-01-01T00:00:00Z"}`))
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
	client.Invalidate()
	second, err := client.Token(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first == second || requests.Load() != 2 {
		t.Fatalf("tokens = %q, %q; requests = %d", first, second, requests.Load())
	}
	if _, err := client.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 3 {
		t.Fatalf("requests = %d, want 3", requests.Load())
	}
}

func TestCreateInstallationTokenErrors(t *testing.T) {
	key := testKey(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "permission denied", http.StatusForbidden)
	}))
	defer server.Close()
	_, err := CreateInstallationToken(context.Background(), TokenRequest{
		AppID: "123", InstallationID: "42", PrivateKey: key, APIURL: server.URL,
	})
	if err == nil || !strings.Contains(err.Error(), "403") || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("CreateInstallationToken() error = %v", err)
	}
}

func TestCreateInstallationTokenRejectsInvalidAPIURL(t *testing.T) {
	key := testKey(t)
	for _, apiURL := range []string{"not a URL", "://missing-scheme"} {
		t.Run(apiURL, func(t *testing.T) {
			_, err := CreateInstallationToken(context.Background(), TokenRequest{
				AppID: "123", InstallationID: "42", PrivateKey: key, APIURL: apiURL,
			})
			if err == nil || !strings.Contains(err.Error(), "invalid API URL") {
				t.Fatalf("CreateInstallationToken() error = %v", err)
			}
		})
	}
}

func TestClientRejectsMalformedExpiration(t *testing.T) {
	key := testKey(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"token":"ghs_token","expires_at":"not-a-timestamp"}`))
	}))
	defer server.Close()
	client, err := NewClient(ClientConfig{
		TokenRequest: TokenRequest{AppID: "123", InstallationID: "42", PrivateKey: key, APIURL: server.URL},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Token(context.Background()); err == nil || !strings.Contains(err.Error(), "parse token expiration") {
		t.Fatalf("Token() error = %v", err)
	}
}

func TestCreateInstallationTokenHonorsContextCancellation(t *testing.T) {
	key := testKey(t)
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errs := make(chan error, 1)
	go func() {
		_, err := CreateInstallationToken(ctx, TokenRequest{
			AppID: "123", InstallationID: "42", PrivateKey: key, APIURL: server.URL,
		})
		errs <- err
	}()
	<-started
	cancel()
	if err := <-errs; err == nil || !strings.Contains(err.Error(), "request installation token") {
		t.Fatalf("CreateInstallationToken() error = %v", err)
	}
}

func TestCreateInstallationTokenUsesCustomHTTPClient(t *testing.T) {
	key := testKey(t)
	var requests atomic.Int32
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			requests.Add(1)
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Body:       io.NopCloser(strings.NewReader(`{"token":"ghs_custom_client_token"}`)),
				Header:     make(http.Header),
				Request:    r,
			}, nil
		}),
	}
	token, err := CreateInstallationToken(context.Background(), TokenRequest{
		AppID: "123", InstallationID: "42", PrivateKey: key,
		APIURL: "https://example.test", HTTPClient: client,
	})
	if err != nil {
		t.Fatal(err)
	}
	if token != "ghs_custom_client_token" || requests.Load() != 1 {
		t.Fatalf("token = %q, requests = %d", token, requests.Load())
	}
}

func TestClientRefreshesExpiredToken(t *testing.T) {
	key := testKey(t)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		number := requests.Add(1)
		expiresAt := "2000-01-01T00:00:00Z"
		if number > 1 {
			expiresAt = "2099-01-01T00:00:00Z"
		}
		_, _ = w.Write([]byte(`{"token":"ghs_token","expires_at":"` + expiresAt + `"}`))
	}))
	defer server.Close()
	client, err := NewClient(ClientConfig{
		TokenRequest:  TokenRequest{AppID: "123", InstallationID: "42", PrivateKey: key, APIURL: server.URL},
		RefreshBefore: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Token(context.Background()); err == nil {
		t.Fatal("Token() accepted expired token")
	}
	if token, err := client.Token(context.Background()); err != nil || token != "ghs_token" {
		t.Fatalf("Token() = %q, %v", token, err)
	}
	if requests.Load() != 2 {
		t.Fatalf("requests = %d, want 2", requests.Load())
	}
}

func TestNewClientValidation(t *testing.T) {
	key := testKey(t)
	tests := []struct {
		name   string
		config ClientConfig
		want   string
	}{
		{"missing app ID", ClientConfig{TokenRequest: TokenRequest{InstallationID: "42", PrivateKey: key}}, "app ID"},
		{"missing installation ID", ClientConfig{TokenRequest: TokenRequest{AppID: "123", PrivateKey: key}}, "installation ID"},
		{"missing private key", ClientConfig{TokenRequest: TokenRequest{AppID: "123", InstallationID: "42"}}, "private key"},
		{"negative refresh window", ClientConfig{TokenRequest: TokenRequest{AppID: "123", InstallationID: "42", PrivateKey: key}, RefreshBefore: -time.Second}, "refresh-before"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewClient(test.config)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("NewClient() error = %v, want %q", err, test.want)
			}
		})
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
	key := testKey(t)
	pemKey := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	loaded, err := ParsePrivateKey(string(pemKey))
	if err != nil || loaded.N.Cmp(key.N) != 0 {
		t.Fatalf("ParsePrivateKey() failed: %v", err)
	}
}

func TestParsePrivateKeyPKCS8AndFile(t *testing.T) {
	key := testKey(t)
	pemKey := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: mustMarshalPKCS8(t, key)})
	file := t.TempDir() + "/private-key.pem"
	if err := os.WriteFile(file, pemKey, 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := ParsePrivateKey(file)
	if err != nil || loaded.N.Cmp(key.N) != 0 {
		t.Fatalf("ParsePrivateKey() failed: %v", err)
	}
}

func TestParsePrivateKeyErrors(t *testing.T) {
	for _, input := range []string{"not pem", t.TempDir() + "/missing.pem"} {
		if _, err := ParsePrivateKey(input); err == nil {
			t.Fatalf("ParsePrivateKey(%q) succeeded", input)
		}
	}
}

func testKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func mustMarshalPKCS8(t *testing.T, key *rsa.PrivateKey) []byte {
	t.Helper()
	data, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
