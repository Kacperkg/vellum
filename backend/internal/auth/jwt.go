package auth

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type Claims struct {
	UserID      uuid.UUID  `json:"user_id"`
	AuthVersion int64      `json:"auth_version"`
	SessionID   *uuid.UUID `json:"session_id,omitempty"`
	jwt.RegisteredClaims
}

func GenerateToken(userID uuid.UUID, secret string, expiry time.Duration, version ...int64) (string, error) {
	var authVersion int64
	if len(version) > 0 {
		authVersion = version[0]
	}
	return generateToken(userID, secret, expiry, authVersion, nil)
}

// GenerateSessionToken binds newly issued access credentials to one refresh family.
func GenerateSessionToken(userID uuid.UUID, secret string, expiry time.Duration, version int64, sessionID uuid.UUID) (string, error) {
	return generateToken(userID, secret, expiry, version, &sessionID)
}

func generateToken(userID uuid.UUID, secret string, expiry time.Duration, authVersion int64, sessionID *uuid.UUID) (string, error) {
	claims := &Claims{
		UserID:      userID,
		AuthVersion: authVersion,
		SessionID:   sessionID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(expiry)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

func ValidateToken(tokenString string, secret string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(
		tokenString,
		&Claims{},
		func(token *jwt.Token) (interface{}, error) {
			return []byte(secret), nil
		}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired())

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	} else {
		return nil, jwt.ErrTokenInvalidClaims
	}
}
