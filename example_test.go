package githubapptoken_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"

	githubapptoken "github.com/tongbpm/github-app-token"
)

func ExampleParsePrivateKey() {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}

	pemKey := keyBytes(key)
	parsed, err := githubapptoken.ParsePrivateKey(pemKey)
	if err != nil {
		panic(err)
	}
	_ = parsed
}

func ExampleCreateInstallationToken() {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"token":"ghs_example_token"}`))
	}))
	defer server.Close()

	token, err := githubapptoken.CreateInstallationToken(context.Background(), githubapptoken.TokenRequest{
		AppID:          "123456",
		InstallationID: "789012",
		PrivateKey:     key,
		APIURL:         server.URL,
	})
	if err != nil {
		panic(err)
	}
	_ = token
}

func ExampleNewClient() {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}

	client, err := githubapptoken.NewClient(githubapptoken.ClientConfig{
		TokenRequest: githubapptoken.TokenRequest{
			AppID:          "123456",
			InstallationID: "789012",
			PrivateKey:     key,
		},
	})
	if err != nil {
		panic(err)
	}
	_ = client
}

func keyBytes(key *rsa.PrivateKey) string {
	return string(pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	}))
}
