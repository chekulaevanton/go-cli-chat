package crypter

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
)

type Crypter struct {
}

func New() *Crypter {
	return &Crypter{}
}

func (c *Crypter) GenKeys() (string, string, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", err
	}

	pub, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return "", "", err
	}

	pr := x509.MarshalPKCS1PrivateKey(key)

	pubRem := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pub})
	prRem := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: pr})

	return string(pubRem), string(prRem), nil
}

func (c *Crypter) Encrypt(data string, pubKey string) (string, error) {
	decodedPubKey, _ := pem.Decode([]byte(pubKey))
	if decodedPubKey == nil {
		return "", errors.New("failed to decode public key")
	}

	publicKey, err := x509.ParsePKIXPublicKey(decodedPubKey.Bytes)
	if err != nil {
		return "", err
	}

	ciphertext, err := rsa.EncryptPKCS1v15(rand.Reader, publicKey.(*rsa.PublicKey), []byte(data))
	if err != nil {
		return "", err
	}

	encryptedData := base64.StdEncoding.EncodeToString(ciphertext)
	return encryptedData, nil
}

// Decrypt расшифровывает текстовые данные с использованием закрытого ключа и декодирует base64.
func (c *Crypter) Decrypt(data string, prKey string) (string, error) {
	decodedPrKey, _ := pem.Decode([]byte(prKey))
	if decodedPrKey == nil {
		return "", errors.New("failed to decode private key")
	}

	privateKey, err := x509.ParsePKCS1PrivateKey(decodedPrKey.Bytes)
	if err != nil {
		return "", err
	}

	decodedData, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return "", err
	}

	plaintext, err := rsa.DecryptPKCS1v15(rand.Reader, privateKey, decodedData)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}
