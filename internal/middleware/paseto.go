package middleware

import (
	"errors"
	"time"

	"aidanwoods.dev/go-paseto"
	"github.com/labstack/echo/v4"
	stdmw "github.com/labstack/echo/v4/middleware"
	"google.golang.org/grpc/codes"
	rpcStatus "google.golang.org/grpc/status"
)

type pasetoExtractor func(echo.Context) (string, error)

func pasetoFromHeader(header string, authScheme string) pasetoExtractor {
	return func(ctx echo.Context) (string, error) {
		auth := ctx.Request().Header.Get(header)
		ln := len(authScheme)
		if len(auth) > ln+1 && auth[:ln] == authScheme {
			return auth[ln+1:], nil
		}

		return "", errors.New("missing or malformed paseto token")
	}
}

type PASETOConfig struct {
	Skipper      stdmw.Skipper
	ErrorHandler func(echo.Context, error) error
	SymmetricKey paseto.V4SymmetricKey
	Implicit     []byte
	Rules        []paseto.Rule
	ContextKey   string
}

func PASETO(config PASETOConfig) echo.MiddlewareFunc {
	if config.Skipper == nil {
		config.Skipper = stdmw.DefaultSkipper
	}

	if config.ContextKey == "" {
		config.ContextKey = "token"
	}

	extractor := pasetoFromHeader(echo.HeaderAuthorization, "Bearer")

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if config.Skipper(c) {
				return next(c)
			}

			tainted, err := extractor(c)
			if err != nil {
				if config.ErrorHandler != nil {
					return config.ErrorHandler(c, err)
				}

				return rpcStatus.Error(codes.Unauthenticated, "Your provided token is not valid. Please provide a valid token")
			}

			rules := append(config.Rules, paseto.NotExpired(), paseto.ValidAt(time.Now()))
			parser := paseto.MakeParser(rules)
			token, err := parser.ParseV4Local(config.SymmetricKey, tainted, config.Implicit)
			if err != nil {
				if config.ErrorHandler != nil {
					return config.ErrorHandler(c, err)
				}

				return rpcStatus.Error(codes.Unauthenticated, "Your provided token is not valid. Please provide a valid token")
			}

			c.Set(config.ContextKey, token)
			return next(c)
		}
	}
}
