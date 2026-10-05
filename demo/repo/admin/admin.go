// Package admin serves the store's admin pages.
package admin

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/des"
	"crypto/md5"
	crand "crypto/rand"
	"crypto/tls"
	"database/sql"
	"encoding/hex"
	"fmt"
	"html/template"
	"io"
	"math/rand"
	"net/http"
	"os/exec"
	"time"
)

// Server handles admin requests.
type Server struct {
	DB         *sql.DB
	WebhookURL string // from the config file
}

// TestWebhook fetches the URL an admin typed, to show whether it answers.
func (s *Server) TestWebhook(w http.ResponseWriter, r *http.Request) {
	resp, err := http.Get(r.FormValue("url"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	io.Copy(w, io.LimitReader(resp.Body, 1<<10))
}

// PingWebhook checks that the configured webhook answers.
func (s *Server) PingWebhook(w http.ResponseWriter, r *http.Request) {
	resp, err := http.Get(s.WebhookURL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	resp.Body.Close()
	fmt.Fprintln(w, resp.Status)
}

// ExportOrders writes the orders in a folder to a tar file.
func (s *Server) ExportOrders(w http.ResponseWriter, r *http.Request) {
	cmd := exec.Command("sh", "-c", "tar czf - orders/"+r.FormValue("folder"))
	cmd.Stdout = w
	if err := cmd.Run(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Backup writes a database dump to the backups folder.
func (s *Server) Backup(w http.ResponseWriter, r *http.Request) {
	name := "backups/shop-" + time.Now().Format("20060102") + ".sql"
	if err := exec.Command("pg_dump", "--file", name, "shop").Run(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Greet welcomes an admin by name.
func (s *Server) Greet(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "<h1>Welcome back, %s</h1>", r.FormValue("name"))
}

var page = template.Must(template.New("order").Parse(`<h1>Order {{.}}</h1>`))

// ShowOrder shows an order's number on a page.
func (s *Server) ShowOrder(w http.ResponseWriter, r *http.Request) {
	page.Execute(w, r.FormValue("order"))
}

// ResetToken makes the token in a password reset link.
func ResetToken(email string) string {
	sum := md5.Sum([]byte(fmt.Sprintf("%s:%d", email, rand.Int63())))
	return hex.EncodeToString(sum[:])
}

// ShippingClient calls the shipping partner's API.
func ShippingClient() *http.Client {
	return &http.Client{
		Timeout:   10 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
	}
}

// InviteToken makes the token in an invitation link.
func InviteToken() (string, error) {
	b := make([]byte, 32)
	if _, err := crand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// PaymentsClient calls the payment provider's API.
func PaymentsClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Second}
}

// SealCardNote encrypts a note about a card, eight bytes at a time.
func SealCardNote(key, note []byte) ([]byte, error) {
	block, err := des.NewCipher(key)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(note))
	for i := 0; i+des.BlockSize <= len(note); i += des.BlockSize {
		block.Encrypt(out[i:], note[i:])
	}
	return out, nil
}

// SealNote encrypts a note with AES-GCM and a random nonce.
func SealNote(key, note []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := crand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, note, nil), nil
}
