package main

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"
)

// tlsConfig: the certificates trusted besides the ones of the system (ca_file), or none checked (insecure)
func (c *Config) tlsConfig() (*tls.Config, error) {
	if c == nil {
		return nil, nil
	}
	if c.Insecure {
		return &tls.Config{InsecureSkipVerify: true}, nil //nolint:gosec // asked for, local development
	}
	if c.CAFile == "" {
		return nil, nil
	}
	data, err := os.ReadFile(expandPath(c.CAFile))
	if err != nil {
		return nil, fmt.Errorf("ca_file: %w", err)
	}
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(data) {
		return nil, fmt.Errorf("ca_file %s: no PEM certificate in it", c.CAFile)
	}
	return &tls.Config{RootCAs: pool}, nil
}

// httpClient with the tls settings of the config (validated before: an error leaves the defaults)
func httpClient(c *Config, timeout time.Duration) *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if tc, err := c.tlsConfig(); err == nil && tc != nil {
		tr.TLSClientConfig = tc
	}
	return &http.Client{Timeout: timeout, Transport: tr}
}

// isCertError: the server certificate is not trusted (self-signed, unknown authority, other host name)
func isCertError(err error) bool {
	var ua x509.UnknownAuthorityError
	var host x509.HostnameError
	var inv x509.CertificateInvalidError
	var verif *tls.CertificateVerificationError
	return errors.As(err, &ua) || errors.As(err, &host) || errors.As(err, &inv) || errors.As(err, &verif)
}

const certHint = "the certificate is not trusted (self-signed?): artcoded config set ca_file /path/to/ca.pem\n  or, for local development only: artcoded config set insecure true"

// askCertTrust: during the setup, how to trust a certificate the system refuses. false: cancelled.
func askCertTrust(cfg *Config, cause error) (bool, error) {
	fmt.Fprintln(stderr, yellow("! ")+"the certificate is not trusted: "+cause.Error())
	if !isInteractive() {
		return false, nil
	}
	opts := []string{
		"use the certificate (authority) file of my setup (pem)",
		"don't check certificates (local development only)",
		"cancel",
	}
	for {
		i, err := choose("How to trust it?", opts)
		if err != nil || i == 2 {
			return false, err
		}
		if i == 1 {
			cfg.Insecure, cfg.CAFile = true, ""
			warn("certificates are not checked (insecure: true in the config)")
			return true, nil
		}
		p, err := askRequired("Path of the pem file (e.g. ~/.local/share/mkcert/rootCA.pem)", cfg.CAFile)
		if err != nil {
			return false, err
		}
		cfg.CAFile, cfg.Insecure = p, false
		if _, err := cfg.tlsConfig(); err != nil {
			fmt.Fprintln(stderr, red("  "+err.Error()))
			continue
		}
		return true, nil
	}
}
