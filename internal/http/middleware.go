package http

import (
	"context"
	"net/http"
	"strings"

	"minicloud/internal/auth"
	"minicloud/internal/catalog"
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

func requireApproved(cat *catalog.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, err := cat.UserByID(r.Context(), userIDFrom(r.Context()))
			if err != nil {
				writeError(w, http.StatusUnauthorized, "session expired, log in again")
				return
			}
			if !user.Approved {
				writeError(w, http.StatusForbidden, "this account is waiting for the owner to approve it")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func requireAdmin(cat *catalog.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, err := cat.UserByID(r.Context(), userIDFrom(r.Context()))
			if err != nil {
				writeError(w, http.StatusUnauthorized, "session expired, log in again")
				return
			}
			if !user.IsAdmin {
				writeError(w, http.StatusForbidden, "only the owner can do that")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
