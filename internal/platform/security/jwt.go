package security

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const jwtIssuer = "truss"

// JWTManager menerbitkan dan memvalidasi token. Secret disuntikkan dari
// config, bukan dibaca langsung dari environment.
type JWTManager struct {
	secret []byte
	ttl    time.Duration
}

func NewJWTManager(secret string, ttl time.Duration) (*JWTManager, error) {
	if secret == "" {
		return nil, errors.New("JWT_SECRET belum dikonfigurasi")
	}
	return &JWTManager{secret: []byte(secret), ttl: ttl}, nil
}

func (m *JWTManager) Issue(userID string) (string, time.Duration, error) {
	now := time.Now()

	claims := jwt.RegisteredClaims{
		Subject:   userID,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(m.ttl)),
		Issuer:    jwtIssuer,
	}

	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
	if err != nil {
		return "", 0, err
	}
	return token, m.ttl, nil
}

func (m *JWTManager) Validate(tokenString string) (string, error) {
	claims := &jwt.RegisteredClaims{}

	token, err := jwt.ParseWithClaims(
		tokenString,
		claims,
		func(t *jwt.Token) (any, error) {
			if t.Method != jwt.SigningMethodHS256 {
				return nil, errors.New("algoritma token tidak valid")
			}
			return m.secret, nil
		},
		jwt.WithIssuer(jwtIssuer),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return "", err
	}
	if !token.Valid || claims.Subject == "" {
		return "", errors.New("token tidak valid")
	}
	return claims.Subject, nil
}
