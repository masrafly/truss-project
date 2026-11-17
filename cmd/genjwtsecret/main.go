// Command genjwtsecret membuat nilai JWT_SECRET acak yang kuat memakai crypto/rand
// (sumber acak kriptografis sistem operasi).
//
// Pemakaian:
//
//	go run ./cmd/genjwtsecret            # 48 byte acak -> 64 karakter base64
//	go run ./cmd/genjwtsecret -bytes 64  # lebih panjang
//	go run ./cmd/genjwtsecret -line      # cetak sebagai baris .env: JWT_SECRET=...
//
// Hasil hanya dicetak ke layar; tempel sendiri ke .env. Jangan bagikan atau commit.
package main

import (
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"io"
	"os"

	"truss-project/internal/config"
)

const defaultBytes = 48

func main() {
	n := flag.Int("bytes", defaultBytes, fmt.Sprintf("jumlah byte acak (minimal %d)", config.MinJWTSecretLength))
	line := flag.Bool("line", false, "cetak sebagai baris .env (JWT_SECRET=...)")
	flag.Parse()

	secret, err := generate(rand.Reader, *n)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gagal membuat secret:", err)
		os.Exit(1)
	}
	if *line {
		fmt.Println("JWT_SECRET=" + secret)
		return
	}
	fmt.Println(secret)
}

// generate membaca n byte dari r dan mengodekannya sebagai base64 URL-safe tanpa padding
// (hanya A-Z a-z 0-9 - _), sehingga aman ditempel ke .env tanpa tanda kutip.
func generate(r io.Reader, n int) (string, error) {
	if n < config.MinJWTSecretLength {
		return "", fmt.Errorf("-bytes minimal %d, dapat %d", config.MinJWTSecretLength, n)
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
