package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func testManager() *JWTManager {
	return NewJWTManager("test-secret-key-for-jwt-tests-0123456789abcdef", 900, 604800)
}

// signWithClaims manually signs a token with the given registered claims using
// the same HMAC secret and method the JWTManager uses, so tests can forge
// tokens with wrong issuer/audience values.
func signWithClaims(t *testing.T, secret string, rc jwt.RegisteredClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, &Claims{
		UserID:           1,
		Username:         "admin",
		Role:             "admin",
		Scope:            "parent",
		RegisteredClaims: rc,
	})
	s, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("SignedString: %v", err)
	}
	return s
}

func TestValidateTokenAcceptsValidToken(t *testing.T) {
	m := testManager()
	for _, scope := range []string{"parent", "child"} {
		token, err := m.GenerateAccessToken(&Claims{
			UserID:    1,
			Username:  "alice",
			Role:      "admin",
			Scope:     scope,
			AccountID: 7,
		})
		if err != nil {
			t.Fatalf("GenerateAccessToken (%s scope): %v", scope, err)
		}

		claims, err := m.ValidateToken(token)
		if err != nil {
			t.Fatalf("ValidateToken rejected a properly-issued %s-scope token: %v", scope, err)
		}
		if claims.Scope != scope {
			t.Errorf("ValidateToken returned scope %q, want %q", claims.Scope, scope)
		}
	}
}

func TestValidateTokenRejectsWrongAudience(t *testing.T) {
	m := testManager()
	secret := string(m.secret)

	token := signWithClaims(t, secret, jwt.RegisteredClaims{
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		Issuer:    JWTIssuer,
		Audience:  jwt.ClaimStrings{"other-audience"},
	})

	if _, err := m.ValidateToken(token); err == nil {
		t.Error("ValidateToken accepted a token with the wrong audience")
	}
}

func TestValidateTokenRejectsMissingAudience(t *testing.T) {
	m := testManager()
	secret := string(m.secret)

	token := signWithClaims(t, secret, jwt.RegisteredClaims{
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		Issuer:    JWTIssuer,
	})

	if _, err := m.ValidateToken(token); err == nil {
		t.Error("ValidateToken accepted a token with no audience claim")
	}
}

func TestValidateTokenRejectsWrongIssuer(t *testing.T) {
	m := testManager()
	secret := string(m.secret)

	token := signWithClaims(t, secret, jwt.RegisteredClaims{
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		Issuer:    "attacker-issuer",
		Audience:  jwt.ClaimStrings{JWTAudience},
	})

	if _, err := m.ValidateToken(token); err == nil {
		t.Error("ValidateToken accepted a token with the wrong issuer")
	}
}

func TestHashAndCheckPassword(t *testing.T) {
	const password = "correct-horse-battery-staple"

	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash == "" || hash == password {
		t.Fatalf("HashPassword returned unusable value %q", hash)
	}

	if !CheckPassword(hash, password) {
		t.Error("CheckPassword failed for the correct password")
	}
	if CheckPassword(hash, "wrong-password") {
		t.Error("CheckPassword accepted an incorrect password")
	}
	if CheckPassword(hash, "") {
		t.Error("CheckPassword accepted an empty password")
	}
}

func TestHashPasswordUniqueSalt(t *testing.T) {
	const password = "same-password"

	h1, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword #1: %v", err)
	}
	h2, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword #2: %v", err)
	}
	if h1 == h2 {
		t.Error("bcrypt should use a random salt; hashes must differ")
	}
}

func TestCheckPasswordTimingSafe(t *testing.T) {
	hash, err := HashPassword("secret")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	if !CheckPasswordTimingSafe(hash, "secret") {
		t.Error("CheckPasswordTimingSafe rejected the correct password")
	}
	if CheckPasswordTimingSafe(hash, "nope") {
		t.Error("CheckPasswordTimingSafe accepted an incorrect password")
	}
	// Empty hash must fall back to the dummy comparison without panicking and
	// must still reject the password.
	if CheckPasswordTimingSafe("", "anything") {
		t.Error("CheckPasswordTimingSafe accepted a password against an empty hash")
	}
}
