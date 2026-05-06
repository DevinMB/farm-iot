package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

const hubIDKey contextKey = "hub_id"

// HubJWTMiddleware validates hub JWTs (claims: hub_id + type:"hub").
func HubJWTMiddleware(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				http.Error(w, `{"error":"missing authorization header"}`, http.StatusUnauthorized)
				return
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				http.Error(w, `{"error":"invalid authorization header format"}`, http.StatusUnauthorized)
				return
			}

			token, err := jwt.Parse(parts[1], func(t *jwt.Token) (interface{}, error) {
				if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, jwt.ErrSignatureInvalid
				}
				return []byte(secret), nil
			}, jwt.WithValidMethods([]string{"HS256"}))

			if err != nil || !token.Valid {
				http.Error(w, `{"error":"invalid or expired token"}`, http.StatusUnauthorized)
				return
			}

			claims, ok := token.Claims.(jwt.MapClaims)
			if !ok {
				http.Error(w, `{"error":"invalid token claims"}`, http.StatusUnauthorized)
				return
			}

			if claims["type"] != "hub" {
				http.Error(w, `{"error":"not a hub token"}`, http.StatusUnauthorized)
				return
			}

			hubID, ok := claims["hub_id"].(string)
			if !ok || hubID == "" {
				http.Error(w, `{"error":"invalid token: missing hub_id"}`, http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), hubIDKey, hubID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// HubIDFromContext extracts the hub_id from the context.
func HubIDFromContext(ctx context.Context) (string, bool) {
	hubID, ok := ctx.Value(hubIDKey).(string)
	return hubID, ok && hubID != ""
}
