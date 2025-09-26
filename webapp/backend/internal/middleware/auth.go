package middleware

import (
	"context"
	"log"
	"net/http"
	"fmt"
	"backend/internal/repository"
)

type contextKey string

const userContextKey contextKey = "user"

var Session_cache = make(map[string]int)

func UserAuthMiddleware(sessionRepo *repository.SessionRepository) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie("session_id")
			if err != nil {
				log.Printf("Error retrieving session cookie: %v", err)
				http.Error(w, "Unauthorized: No session cookie", http.StatusUnauthorized)
				return
			}
			sessionID := cookie.Value

			// cache sessions
			if uid, ok :=  Session_cache[sessionID]; ok {
				fmt.Println("can find user in cache!\n")
				// do nothing
				ctx := context.WithValue(r.Context(), userContextKey, uid)
				next.ServeHTTP(w, r.WithContext(ctx))
			} else {
				userID, err := sessionRepo.FindUserBySessionID(sessionID)
				if err != nil {
					log.Printf("Error finding user by session ID: %v", err)
					http.Error(w, "Unauthorized: Invalid session", http.StatusUnauthorized)
					return
				}
				Session_cache[sessionID] = userID
				ctx := context.WithValue(r.Context(), userContextKey, userID)
				next.ServeHTTP(w, r.WithContext(ctx))
			}

		})
	}
}

func RobotAuthMiddleware(validAPIKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			apiKey := r.Header.Get("X-API-KEY")

			if apiKey == "" || apiKey != validAPIKey {
				http.Error(w, "Forbidden: Invalid or missing API key", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// コンテキストからユーザー情報を取得
// ユーザ情報はUserAuthMiddleware
func GetUserFromContext(ctx context.Context) (int, bool) {
	userID, ok := ctx.Value(userContextKey).(int)
	return userID, ok
}
