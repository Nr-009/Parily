package auth

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

const cookieName = "parily_token"

type Claims struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
	jwt.RegisteredClaims
}

// IssueToken signs a JWT and sets it as an httpOnly cookie.
//
// httpOnly = true  → JavaScript cannot read it (blocks XSS token theft).
// Secure   = false → set to true in production (requires HTTPS).
// SameSite = Lax   → cookie is sent on top-level navigation but not on
//
//	cross-site sub-requests (good CSRF default).
func IssueToken(c *gin.Context, userID, email, secret string, expiryHours int) error {
	expiry := time.Now().Add(time.Duration(expiryHours) * time.Hour)

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{
		UserID: userID,
		Email:  email,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiry),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	})

	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		return fmt.Errorf("sign token: %w", err)
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     cookieName,
		Value:    signed,
		Expires:  expiry,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

// ParseToken reads the cookie and validates the JWT signature + expiry.
// Returns the decoded claims on success.
func ParseToken(c *gin.Context, secret string) (*Claims, error) {
	raw, err := c.Cookie(cookieName)
	if err != nil {
		return nil, errors.New("no auth cookie")
	}
	token, err := jwt.ParseWithClaims(raw, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, fmt.Errorf("invalid token: %w", err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token claims")
	}
	return claims, nil
}

// ClearToken deletes the auth cookie — used on logout.
func ClearToken(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}
