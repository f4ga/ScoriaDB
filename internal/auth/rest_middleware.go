// Copyright 2026 Ekaterina Godulyan
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package auth

import (
	"context"
	"log"
	"net/http"
	"strings"
)

// HTTPContextKey тип для ключей контекста HTTP.
type HTTPContextKey string

const (
	// HTTPContextKeyUser ключ для хранения claims пользователя в HTTP контексте.
	HTTPContextKeyUser HTTPContextKey = "user_claims"
)

// AuthMiddleware возвращает middleware для проверки JWT и ролей в HTTP‑запросах.
func AuthMiddleware(jwtSecret []byte, skipPaths map[string]bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Пропускаем пути, не требующие аутентификации
			if skipPaths[r.URL.Path] {
				next.ServeHTTP(w, r)
				return
			}

			// Извлекаем токен из заголовка Authorization
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				writeAuthError(w, http.StatusUnauthorized, "missing authorization header")
				return
			}

			tokenStr := extractBearerToken(authHeader)
			if tokenStr == "" {
				writeAuthError(w, http.StatusUnauthorized, "invalid authorization header format")
				return
			}

			// Валидируем токен
			claims, err := ValidateToken(tokenStr, jwtSecret)
			if err != nil {
				writeAuthError(w, http.StatusUnauthorized, "invalid token")
				return
			}

			// Проверяем роли для данного пути
			if !hasRequiredRoleForPath(r.URL.Path, r.Method, claims.Roles) {
				writeAuthError(w, http.StatusForbidden, "insufficient privileges")
				return
			}

			// Добавляем claims в контекст запроса
			ctx := context.WithValue(r.Context(), HTTPContextKeyUser, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetClaimsFromHTTPRequest извлекает claims из контекста HTTP запроса.
func GetClaimsFromHTTPRequest(r *http.Request) (*Claims, bool) {
	val := r.Context().Value(HTTPContextKeyUser)
	if val == nil {
		return nil, false
	}
	claims, ok := val.(*Claims)
	return claims, ok
}

// writeAuthError записывает JSON‑ответ с ошибкой аутентификации/авторизации.
func writeAuthError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	jsonBody := `{"code":"` + getErrorCode(status) + `","message":"` + message + `"}`
	if _, err := w.Write([]byte(jsonBody)); err != nil {
		log.Printf("failed to write error response: %v", err)
	}
}

// getErrorCode возвращает строковый код ошибки по HTTP статусу.
func getErrorCode(status int) string {
	switch status {
	case http.StatusUnauthorized:
		return "UNAUTHORIZED"
	case http.StatusForbidden:
		return "FORBIDDEN"
	default:
		return "AUTH_ERROR"
	}
}

// hasRequiredRoleForPath определяет, достаточно ли прав у пользователя для доступа к пути.
func hasRequiredRoleForPath(path, method string, userRoles []string) bool {
	// Маппинг путей и методов на минимально необходимые роли
	var requiredRoles []string

	// REST API пути
	switch {
	case strings.HasPrefix(path, "/api/v1/kv/"):
		switch method {
		case http.MethodGet:
			requiredRoles = []string{RoleReadOnly, RoleReadWrite, RoleAdmin}
		case http.MethodPut, http.MethodDelete:
			requiredRoles = []string{RoleReadWrite, RoleAdmin}
		case http.MethodPost:
			if path == "/api/v1/kv/scan" {
				requiredRoles = []string{RoleReadOnly, RoleReadWrite, RoleAdmin}
			} else {
				requiredRoles = []string{RoleReadWrite, RoleAdmin}
			}
		default:
			requiredRoles = []string{RoleAdmin}
		}
	case path == "/api/v1/auth/login":
		// Логин доступен всем
		return true
	case strings.HasPrefix(path, "/api/v1/admin/"):
		// Все админские пути требуют роли admin
		requiredRoles = []string{RoleAdmin}
	default:
		// По умолчанию требуем admin
		requiredRoles = []string{RoleAdmin}
	}

	// Проверяем, есть ли у пользователя хотя бы одна из требуемых ролей
	for _, userRole := range userRoles {
		for _, required := range requiredRoles {
			if userRole == required {
				return true
			}
		}
	}
	return false
}
