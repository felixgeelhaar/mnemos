package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"google.golang.org/grpc/credentials"
)

// buildServerTLS loads a tls.Config for the HTTP and gRPC servers.
// When clientCAPath is non-empty the config is mTLS — every client
// must present a cert signed by the supplied CA bundle. Empty
// clientCAPath produces a regular server-TLS config.
func buildServerTLS(certFile, keyFile, clientCAPath string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load keypair: %w", err)
	}
	cfg := &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}
	if clientCAPath != "" {
		caData, err := os.ReadFile(clientCAPath)
		if err != nil {
			return nil, fmt.Errorf("read client CA: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caData) {
			return nil, fmt.Errorf("client CA file %s contains no usable certs", clientCAPath)
		}
		cfg.ClientCAs = pool
		cfg.ClientAuth = tls.RequireAndVerifyClientCert
	}
	return cfg, nil
}

// buildServerCreds wraps the TLS config in gRPC TransportCredentials
// for use with grpc.Creds(...).
func buildServerCreds(certFile, keyFile, clientCAPath string) (credentials.TransportCredentials, error) {
	cfg, err := buildServerTLS(certFile, keyFile, clientCAPath)
	if err != nil {
		return nil, err
	}
	return credentials.NewTLS(cfg), nil
}
