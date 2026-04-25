package auth

import (
	"context"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// ContextKey тип для ключей контекста.
type ContextKey string

const (
	// ContextKeyUser ключ для хранения claims пользователя в контексте.
	ContextKeyUser ContextKey = "user_claims"
)

// AuthInterceptor возвращает unary interceptor для проверки JWT и ролей.
func AuthInterceptor(jwtSecret []byte, skipMethods map[string]bool) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// Пропускаем методы, не требующие аутентификации
		if skipMethods[info.FullMethod] {
			return handler(ctx, req)
		}

		// Извлекаем токен из метаданных
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "missing metadata")
		}

		authHeaders := md.Get("authorization")
		if len(authHeaders) == 0 {
			return nil, status.Error(codes.Unauthenticated, "missing authorization header")
		}

		tokenStr := extractBearerToken(authHeaders[0])
		if tokenStr == "" {
			return nil, status.Error(codes.Unauthenticated, "invalid authorization header format")
		}

		// Валидируем токен
		claims, err := ValidateToken(tokenStr, jwtSecret)
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, "invalid token")
		}

		// Проверяем роли (базовая проверка)
		if !hasRequiredRoleForMethod(info.FullMethod, claims.Roles) {
			return nil, status.Error(codes.PermissionDenied, "insufficient privileges")
		}

		// Добавляем claims в контекст
		ctx = context.WithValue(ctx, ContextKeyUser, claims)
		return handler(ctx, req)
	}
}

// StreamAuthInterceptor возвращает stream interceptor для проверки JWT и ролей.
func StreamAuthInterceptor(jwtSecret []byte, skipMethods map[string]bool) grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		// Пропускаем методы, не требующие аутентификации
		if skipMethods[info.FullMethod] {
			return handler(srv, ss)
		}

		// Извлекаем токен из метаданных
		md, ok := metadata.FromIncomingContext(ss.Context())
		if !ok {
			return status.Error(codes.Unauthenticated, "missing metadata")
		}

		authHeaders := md.Get("authorization")
		if len(authHeaders) == 0 {
			return status.Error(codes.Unauthenticated, "missing authorization header")
		}

		tokenStr := extractBearerToken(authHeaders[0])
		if tokenStr == "" {
			return status.Error(codes.Unauthenticated, "invalid authorization header format")
		}

		// Валидируем токен
		claims, err := ValidateToken(tokenStr, jwtSecret)
		if err != nil {
			return status.Error(codes.Unauthenticated, "invalid token")
		}

		// Проверяем роли
		if !hasRequiredRoleForMethod(info.FullMethod, claims.Roles) {
			return status.Error(codes.PermissionDenied, "insufficient privileges")
		}

		// Создаём новый контекст с claims и оборачиваем stream
		ctx := context.WithValue(ss.Context(), ContextKeyUser, claims)
		wrappedStream := &wrappedServerStream{ServerStream: ss, ctx: ctx}
		return handler(srv, wrappedStream)
	}
}

// wrappedServerStream обёртка для grpc.ServerStream с изменённым контекстом.
type wrappedServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (w *wrappedServerStream) Context() context.Context {
	return w.ctx
}

// hasRequiredRoleForMethod определяет, достаточно ли прав у пользователя для вызова метода.
func hasRequiredRoleForMethod(fullMethod string, userRoles []string) bool {
	// Маппинг методов на минимально необходимые роли
	// Формат fullMethod: "/scoriadb.ScoriaDB/Get"
	var requiredRoles []string

	switch {
	case strings.HasSuffix(fullMethod, "Get"):
		requiredRoles = []string{RoleReadOnly, RoleReadWrite, RoleAdmin}
	case strings.HasSuffix(fullMethod, "Put"), strings.HasSuffix(fullMethod, "Delete"):
		requiredRoles = []string{RoleReadWrite, RoleAdmin}
	case strings.HasSuffix(fullMethod, "Scan"):
		requiredRoles = []string{RoleReadOnly, RoleReadWrite, RoleAdmin}
	case strings.HasSuffix(fullMethod, "BeginTxn"), strings.HasSuffix(fullMethod, "CommitTxn"), strings.HasSuffix(fullMethod, "RollbackTxn"):
		requiredRoles = []string{RoleReadWrite, RoleAdmin}
	case strings.HasSuffix(fullMethod, "CreateUser"):
		requiredRoles = []string{RoleAdmin}
	case strings.HasSuffix(fullMethod, "Authenticate"):
		// Аутентификация доступна всем (но этот метод должен быть в skipMethods)
		return true
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

// GetClaimsFromContext извлекает claims из контекста gRPC.
func GetClaimsFromContext(ctx context.Context) (*Claims, bool) {
	val := ctx.Value(ContextKeyUser)
	if val == nil {
		return nil, false
	}
	claims, ok := val.(*Claims)
	return claims, ok
}