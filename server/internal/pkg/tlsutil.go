package pkg

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"time"
)

// CertFingerprint 计算 DER 证书的 SHA256 指纹 hex(Agent Pinning 与 /server-cert 端点)。
func CertFingerprint(der []byte) string {
	h := sha256.Sum256(der)
	return hex.EncodeToString(h[:])
}

// GenerateSelfSigned 生成自签证书(ECDSA P-256, 10 年)用于无证书场景。
// 返回 PEM 编码的 cert/key 与指纹; 指纹随首次启动持久化到数据目录, 重启保持不变。
func GenerateSelfSigned(hosts []string) (certPEM, keyPEM []byte, fingerprint string, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, "", fmt.Errorf("gen key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, "", err
	}
	tmpl := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "XProbe Server"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	for _, h := range hosts {
		if h != "" {
			if ip := net.ParseIP(h); ip != nil {
				tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
			} else {
				tmpl.DNSNames = append(tmpl.DNSNames, h)
			}
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, "", fmt.Errorf("create cert: %w", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, "", err
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM, CertFingerprint(der), nil
}

// LoadCertFingerprint 从 PEM 证书文件内容提取指纹(启动时读取已配置证书)。
func LoadCertFingerprint(certPEM []byte) (string, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return "", errors.New("tls: no CERTIFICATE block in pem")
	}
	return CertFingerprint(block.Bytes), nil
}
