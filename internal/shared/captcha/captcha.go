package captcha

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"time"
)

type Config struct {
	Length int
	Expiry time.Duration
}

func DefaultConfig() Config {
	return Config{
		Length: 5,
		Expiry: 5 * time.Minute,
	}
}

type Generator struct {
	Config Config
	DB     *sql.DB
}

func New(db *sql.DB, cfg Config) *Generator {
	return &Generator{Config: cfg, DB: db}
}

type Challenge struct {
	Type   string `json:"type"`
	Prompt string `json:"prompt"`
}

type Result struct {
	SessionID string    `json:"session_id"`
	Challenge *Challenge `json:"challenge"`
}

func (g *Generator) Generate() (*Result, error) {
	challenge, answer := g.generateChallenge()

	sessionID := generateSessionID()
	_, err := g.DB.Exec(
		`INSERT INTO captcha_sessions (session_id, answer, expires_at) VALUES (?, ?, datetime('now', '+5 minutes'))`,
		sessionID, answer,
	)
	if err != nil {
		return nil, err
	}

	return &Result{
		SessionID: sessionID,
		Challenge: challenge,
	}, nil
}

func (g *Generator) Verify(sessionID, answer string) bool {
	var id int
	var storedAnswer string
	err := g.DB.QueryRow(
		`SELECT id, answer FROM captcha_sessions WHERE session_id = ? AND used = 0 AND expires_at > datetime('now')`,
		sessionID,
	).Scan(&id, &storedAnswer)
	if err != nil {
		return false
	}

	if strings.TrimSpace(answer) != strings.TrimSpace(storedAnswer) {
		return false
	}

	g.DB.Exec("UPDATE captcha_sessions SET used = 1 WHERE id = ?", id)
	return true
}

func (g *Generator) Cleanup() {
	g.DB.Exec("DELETE FROM captcha_sessions WHERE expires_at < datetime('now')")
}

var operators = []string{"+", "-"}

func (g *Generator) generateChallenge() (*Challenge, string) {
	mode := randInt(3)
	switch mode {
	case 0:
		return g.mathChallenge()
	default:
		return g.mathChallenge()
	}
}

func (g *Generator) mathChallenge() (*Challenge, string) {
	a := randInt(10)
	b := randInt(10)
	op := operators[randInt(len(operators))]

	var answer int
	switch op {
	case "+":
		answer = a + b
	case "-":
		if a < b {
			a, b = b, a
		}
		answer = a - b
	}

	prompt := fmt.Sprintf("What is %d %s %d?", a, op, b)
	return &Challenge{Type: "math", Prompt: prompt}, fmt.Sprintf("%d", answer)
}

func generateSessionID() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func randInt(max int) int {
	if max <= 0 {
		return 0
	}
	n, _ := rand.Int(rand.Reader, big.NewInt(int64(max)))
	return int(n.Int64())
}
