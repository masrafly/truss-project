package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// healthcheckCommand adalah argumen yang mengubah binary menjadi pemeriksa kesehatan:
//
//	/app/api healthcheck
//
// Dipakai oleh Docker HEALTHCHECK. Image runtime (distroless) tidak punya curl atau shell,
// jadi binary itu sendiri yang menjadi klien HTTP-nya. Hanya membaca APP_PORT; tidak
// memerlukan JWT_SECRET atau database.
const healthcheckCommand = "healthcheck"

// runHealthcheck memanggil GET {baseURL}/healthz dan mengembalikan exit code proses:
// 0 jika 200 OK, 1 untuk selain itu.
func runHealthcheck(client *http.Client, baseURL string) int {
	resp, err := client.Get(baseURL + "/healthz")
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck gagal:", err)
		return 1
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<10))

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "healthcheck gagal: status", resp.StatusCode)
		return 1
	}
	return 0
}

// healthcheckFromEnv membentuk URL dari APP_PORT (default 8080) dan menjalankan pemeriksaan.
func healthcheckFromEnv() int {
	port := os.Getenv("APP_PORT")
	if port == "" {
		port = "8080"
	}
	return runHealthcheck(&http.Client{Timeout: 3 * time.Second}, "http://127.0.0.1:"+port)
}
