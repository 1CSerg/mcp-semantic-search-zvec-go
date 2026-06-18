package auth

import "net/http"

// AuthMiddleware validates bearer tokens for incoming requests.
func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")
		if token == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type Session struct {
	UserID string
	Token  string
}

func (s *Session) Valid() bool {
	return s.Token != "" && s.UserID != ""
}
