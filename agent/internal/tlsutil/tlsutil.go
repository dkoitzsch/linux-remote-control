// Package tlsutil generates and caches a self-signed TLS certificate so
// rc-agentd can serve HTTPS on the LAN without any external CA. The browser
// will show a one-time trust warning the first time it connects to the
// agent, which is expected for a self-hosted, LAN-only certificate.
package tlsutil

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// EnsureCert loads a cert/key pair from certPath/keyPath, generating and
// persisting a new self-signed one (valid 10 years, covering localhost, the
// machine's hostname and its current LAN IPs) if either file is missing.
func EnsureCert(certPath, keyPath string) (tls.Certificate, error) {
	if _, err := os.Stat(certPath); err == nil {
		if _, err := os.Stat(keyPath); err == nil {
			return tls.LoadX509KeyPair(certPath, keyPath)
		}
	}

	cert, key, err := generateSelfSigned()
	if err != nil {
		return tls.Certificate{}, err
	}
	if err := os.MkdirAll(filepath.Dir(certPath), 0755); err != nil {
		return tls.Certificate{}, err
	}
	if err := os.WriteFile(certPath, cert, 0644); err != nil {
		return tls.Certificate{}, err
	}
	if err := os.WriteFile(keyPath, key, 0600); err != nil {
		return tls.Certificate{}, err
	}
	return tls.X509KeyPair(cert, key)
}

func generateSelfSigned() (certPEM, keyPEM []byte, err error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, err
	}

	hostname, _ := os.Hostname()
	dnsNames := []string{"localhost"}
	if hostname != "" {
		dnsNames = append(dnsNames, hostname, hostname+".local")
	}
	ips := []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				ips = append(ips, ipnet.IP)
			}
		}
	}

	tmpl := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "linux-remote-control agent", Organization: []string{"linux-remote-control"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:                  true,
		BasicConstraintsValid: true,
		DNSNames:              dnsNames,
		IPAddresses:           ips,
	}

	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		return nil, nil, err
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

	keyBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return nil, nil, err
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})

	return certPEM, keyPEM, nil
}
