/*
Copyright © 2025 Brian Tong
*/
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
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const jwtLifetime = 9 * time.Minute
const defaultRefreshBefore = time.Minute

// TokenRequest contains the credentials and endpoint used to request an
// installation access token.
type TokenRequest struct {
	AppID          string
	InstallationID string
	PrivateKey     *rsa.PrivateKey
	APIURL         string
	Timeout        time.Duration
	HTTPClient     *http.Client
}

type tokenResponse struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
}

// Client caches an installation token in memory and refreshes it before it
// expires. A Client should be created per app installation.
type Client struct {
	request       TokenRequest
	refreshBefore time.Duration

	mu         sync.Mutex
	token      string
	expiresAt  time.Time
	refreshing chan struct{}
}

// ClientConfig configures a cached installation-token client.
type ClientConfig struct {
	TokenRequest
	RefreshBefore time.Duration
}

// NewClient creates a client that reuses tokens until they approach expiry.
func NewClient(config ClientConfig) (*Client, error) {
	if config.RefreshBefore < 0 {
		return nil, fmt.Errorf("refresh-before must not be negative")
	}
	if config.RefreshBefore == 0 {
		config.RefreshBefore = defaultRefreshBefore
	}
	if config.AppID == "" || config.InstallationID == "" {
		return nil, fmt.Errorf("app ID and installation ID are required")
	}
	if config.PrivateKey == nil {
		return nil, fmt.Errorf("private key is required")
	}
	return &Client{
		request:       config.TokenRequest,
		refreshBefore: config.RefreshBefore,
	}, nil
}

// Token returns a cached token or obtains a new one. Concurrent callers
// share one refresh request.
func (c *Client) Token(ctx context.Context) (string, error) {
	for {
		c.mu.Lock()
		if c.token != "" && time.Now().Add(c.refreshBefore).Before(c.expiresAt) {
			token := c.token
			c.mu.Unlock()
			return token, nil
		}
		if c.refreshing != nil {
			refreshing := c.refreshing
			c.mu.Unlock()
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-refreshing:
			}
			continue
		}
		refreshing := make(chan struct{})
		c.refreshing = refreshing
		c.mu.Unlock()

		result, err := requestInstallationToken(ctx, c.request)
		if err == nil && result.expiresAt.IsZero() {
			err = fmt.Errorf("GitHub response did not contain token expiration")
		}
		if err == nil && !time.Now().Add(c.refreshBefore).Before(result.expiresAt) {
			err = fmt.Errorf("GitHub response token expires too soon")
		}

		c.mu.Lock()
		if err == nil {
			c.token = result.Token
			c.expiresAt = result.expiresAt
		}
		c.refreshing = nil
		close(refreshing)
		c.mu.Unlock()
		if err != nil {
			return "", err
		}
		return result.Token, nil
	}
}

// Refresh invalidates the cached token and obtains a fresh one.
func (c *Client) Refresh(ctx context.Context) (string, error) {
	c.Invalidate()
	return c.Token(ctx)
}

// Invalidate removes the cached token. The next Token call will refresh it.
func (c *Client) Invalidate() {
	c.mu.Lock()
	c.token = ""
	c.expiresAt = time.Time{}
	c.mu.Unlock()
}

// ParsePrivateKey parses an RSA private key from PEM contents or a file path.
func ParsePrivateKey(input string) (*rsa.PrivateKey, error) {
	data := []byte(input)
	if !strings.Contains(input, "-----BEGIN") {
		var err error
		data, err = os.ReadFile(input)
		if err != nil {
			return nil, fmt.Errorf("read private key: %w", err)
		}
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("private key is not valid PEM")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("private key must be RSA")
	}
	return rsaKey, nil
}

// CreateInstallationToken requests a short-lived GitHub App installation
// access token.
func CreateInstallationToken(ctx context.Context, request TokenRequest) (string, error) {
	result, err := requestInstallationToken(ctx, request)
	if err != nil {
		return "", err
	}
	return result.Token, nil
}

type installationTokenResult struct {
	Token     string
	expiresAt time.Time
}

func requestInstallationToken(ctx context.Context, request TokenRequest) (installationTokenResult, error) {
	if request.AppID == "" || request.InstallationID == "" {
		return installationTokenResult{}, fmt.Errorf("app ID and installation ID are required")
	}
	if request.PrivateKey == nil {
		return installationTokenResult{}, fmt.Errorf("private key is required")
	}
	baseURL := strings.TrimRight(request.APIURL, "/")
	if baseURL == "" {
		baseURL = "https://api.github.com"
	}
	parsedURL, err := url.Parse(baseURL)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		return installationTokenResult{}, fmt.Errorf("invalid API URL %q", request.APIURL)
	}

	jwt, err := createJWT(request.AppID, request.PrivateKey, time.Now())
	if err != nil {
		return installationTokenResult{}, fmt.Errorf("create app JWT: %w", err)
	}
	endpoint := baseURL + "/app/installations/" + url.PathEscape(request.InstallationID) + "/access_tokens"
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return installationTokenResult{}, fmt.Errorf("create token request: %w", err)
	}
	httpRequest.Header.Set("Accept", "application/vnd.github+json")
	httpRequest.Header.Set("Authorization", "Bearer "+jwt)
	httpRequest.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	client := request.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: request.Timeout}
	}
	response, err := client.Do(httpRequest)
	if err != nil {
		return installationTokenResult{}, fmt.Errorf("request installation token: %w", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return installationTokenResult{}, fmt.Errorf("read GitHub response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return installationTokenResult{}, fmt.Errorf("GitHub returned %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	var result tokenResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return installationTokenResult{}, fmt.Errorf("decode GitHub response: %w", err)
	}
	if result.Token == "" {
		return installationTokenResult{}, fmt.Errorf("GitHub response did not contain an installation token")
	}
	var expiresAt time.Time
	if result.ExpiresAt != "" {
		expiresAt, err = time.Parse(time.RFC3339, result.ExpiresAt)
		if err != nil {
			return installationTokenResult{}, fmt.Errorf("parse token expiration: %w", err)
		}
	}
	return installationTokenResult{Token: result.Token, expiresAt: expiresAt}, nil
}

func createJWT(appID string, key *rsa.PrivateKey, now time.Time) (string, error) {
	issuedAt := now.Add(-time.Minute).Unix()
	claims := map[string]any{"iat": issuedAt, "exp": now.Add(jwtLifetime).Unix(), "iss": appID}
	header := map[string]string{"alg": "RS256", "typ": "JWT"}
	encode := func(value any) (string, error) {
		data, err := json.Marshal(value)
		if err != nil {
			return "", err
		}
		return base64.RawURLEncoding.EncodeToString(data), nil
	}
	encodedHeader, err := encode(header)
	if err != nil {
		return "", err
	}
	encodedClaims, err := encode(claims)
	if err != nil {
		return "", err
	}
	signingInput := encodedHeader + "." + encodedClaims
	digest := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}
