package http

import (
	"context"
	"net/http"
	"strings"

	"minicloud/internal/auth"
)

type ctxKey int

const userIDKey ctxKey = 1

func withUserID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, userIDKey, id)
}

func userIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(userIDKey).(string)
	return id
}

func requireAuth(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if !strings.HasPrefix(header, "Bearer ") {
				writeError(w, http.StatusUnauthorized, "log in first")
				return
			}
			id, err := auth.ParseUserID(secret, strings.TrimPrefix(header, "Bearer "))
			if err != nil {
				writeError(w, http.StatusUnauthorized, "session expired, log in again")
				return
			}
			next.ServeHTTP(w, r.WithContext(withUserID(r.Context(), id)))
		})
	}
}
