package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientTLSVerification(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer server.Close()
	trusted, err := clientTLSConfig(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
	if err != nil {
		t.Fatal(err)
	}
	wrongName := trusted.Clone()
	wrongName.ServerName = "wrong.example.invalid"
	for _, tc := range []struct {
		name    string
		config  *tls.Config
		wantErr bool
	}{
		{"trusted", trusted, false},
		{"untrusted", &tls.Config{RootCAs: x509.NewCertPool(), MinVersion: tls.VersionTLS12}, true},
		{"wrong hostname", wrongName, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transport := &http.Transport{TLSClientConfig: tc.config}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport, Timeout: time.Second}
			response, err := client.Get(server.URL)
			if response != nil {
				if closeErr := response.Body.Close(); closeErr != nil {
					t.Error(closeErr)
				}
			}
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, want failure = %v", err, tc.wantErr)
			}
		})
	}
}
func TestClientTLSRejectsInvalidTrust(t *testing.T) {
	if _, err := clientTLSConfig([]byte("not a certificate")); err == nil {
		t.Fatal("invalid certificate accepted")
	}
}
