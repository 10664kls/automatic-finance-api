package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"

	"os"
	"os/signal"
	"syscall"
	"time"

	"aidanwoods.dev/go-paseto"
	httpPb "github.com/10664kls/automatic-finance-api/genproto/go/http/v1"
	"github.com/10664kls/automatic-finance-api/internal/auth"
	"github.com/10664kls/automatic-finance-api/internal/cib"
	"github.com/10664kls/automatic-finance-api/internal/currency"
	"github.com/10664kls/automatic-finance-api/internal/income"
	"github.com/10664kls/automatic-finance-api/internal/middleware"
	"github.com/10664kls/automatic-finance-api/internal/selfemployed"
	"github.com/10664kls/automatic-finance-api/internal/server"
	"github.com/10664kls/automatic-finance-api/internal/statement"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/labstack/echo/v4"
	stdmw "github.com/labstack/echo/v4/middleware"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"google.golang.org/genproto/googleapis/rpc/code"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"

	_ "github.com/denisenkom/go-mssqldb"
)

func main() {
	if err := run(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	zlog, err := newLogger()
	if err != nil {
		return fmt.Errorf("failed to create logger: %w", err)
	}
	defer zlog.Sync()
	zap.ReplaceGlobals(zlog)
	zlog.Info("Logger replaced in globals")
	zlog.Info("Logger initialized")

	db, err := sql.Open(
		"sqlserver",
		fmt.Sprintf("sqlserver://%s:%s@%s:%s?database=%s&TrustServerCertificate=true",
			os.Getenv("DB_USER"),
			os.Getenv("DB_PASSWORD"),
			os.Getenv("DB_HOST"),
			os.Getenv("DB_PORT"),
			os.Getenv("DB_NAME"),
		),
	)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}
	zlog.Info("Database connection established")

	aKey := must(paseto.V4SymmetricKeyFromHex(os.Getenv("PASETO_ACCESS_KEY")))
	rKey := must(paseto.V4SymmetricKeyFromHex(os.Getenv("PASETO_REFRESH_KEY")))

	// Initialize the auth service
	authSvc, err := auth.New(ctx, db, zlog, aKey, rKey)
	if err != nil {
		return fmt.Errorf("failed to create auth service: %w", err)
	}
	zlog.Info("Auth service initialized")

	// Initialize the currency service
	currencySvc, err := currency.NewService(ctx, db, zlog)
	if err != nil {
		return fmt.Errorf("failed to create currency service: %w", err)
	}
	zlog.Info("Currency service initialized")

	// Initialize the statement service
	statementSvc, err := statement.NewService(ctx, db, zlog)
	if err != nil {
		return fmt.Errorf("failed to create statement service: %w", err)
	}
	zlog.Info("Statement service initialized")

	// Initialize the income service
	incomeSvc, err := income.NewService(ctx, db, currencySvc, statementSvc, zlog)
	if err != nil {
		return fmt.Errorf("failed to create income service: %w", err)
	}
	zlog.Info("Income service initialized")

	cibService, err := cib.NewService(ctx, db, currencySvc, zlog, os.Getenv("PDF_EXTRACTOR_URL"))
	if err != nil {
		return fmt.Errorf("failed to create cib service: %w", err)
	}
	zlog.Info("CIB service initialized")

	selfemployedSvc, err := selfemployed.NewService(ctx, db, statementSvc, currencySvc, zlog)
	if err != nil {
		return fmt.Errorf("failed to create selfemployed service: %w", err)
	}
	zlog.Info("Selfemployed service initialized")

	e := echo.New()
	e.HideBanner = true
	e.HTTPErrorHandler = httpErr
	e.Use(httpLogger(zlog))
	e.Use(stdMws()...)

	mdw := []echo.MiddlewareFunc{
		middleware.PASETO(middleware.PASETOConfig{
			SymmetricKey: aKey,
		},
		),
		middleware.SetContextClaimsFromToken,
	}

	serve := must(server.NewServer(authSvc, currencySvc, incomeSvc, statementSvc, cibService, selfemployedSvc))
	if err := serve.Install(e, mdw...); err != nil {
		return fmt.Errorf("failed to install auth service: %w", err)
	}

	errCh := make(chan error)
	go func() {
		errCh <- e.Start(fmt.Sprintf(":%s", getEnv("PORT", "8890")))
	}()

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, os.Kill, syscall.SIGTERM)
	defer stop()

	select {
	case <-ctx.Done():
		zlog.Info("Received shutdown signal, shutting down server...")

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		zlog.Info("Waiting for server to shut down...")
		if err := e.Shutdown(ctx); err != nil {
			zlog.Error("Error shutting down server", zap.Error(err))
			return err
		}
		zlog.Info("Server shut down gracefully")

	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			zlog.Error("Error starting server", zap.Error(err))
			return err
		}
	}

	return nil
}

func getEnv(key string, fallback string) string {
	value, exists := os.LookupEnv(key)
	if !exists {
		return fallback
	}
	return value
}

func httpLogger(zlog *zap.Logger) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			err := next(c)
			if err != nil {
				c.Error(err)
			}

			req := c.Request()
			res := c.Response()

			fields := []zapcore.Field{
				zap.String("remote_ip", c.RealIP()),
				zap.String("host", req.Host),
				zap.String("request", fmt.Sprintf("%s %s", req.Method, req.RequestURI)),
				zap.Int("status", res.Status),
				zap.String("user_agent", req.UserAgent()),
			}

			id := req.Header.Get(echo.HeaderXRequestID)
			if id != "" {
				fields = append(fields, zap.String("request_id", id))
			}

			n := res.Status
			switch {
			case n >= 500:
				zlog.
					With(zap.Error(err)).
					Error("HTTP Error", fields...)

			case n >= 400:
				zlog.
					With(zap.Error(err)).
					Warn("HTTP Error", fields...)

			case n >= 300:
				zlog.
					Info("Redirect", fields...)

			default:
				zlog.
					Info("HTTP Request", fields...)
			}

			return nil
		}
	}
}

func stdMws() []echo.MiddlewareFunc {
	return []echo.MiddlewareFunc{
		stdmw.RemoveTrailingSlash(),
		stdmw.Recover(),
		stdmw.CORSWithConfig((stdmw.CORSConfig{
			AllowOriginFunc: func(origin string) (bool, error) {
				return true, nil
			},
			AllowMethods: []string{
				http.MethodGet,
				http.MethodPost,
				http.MethodPut,
				http.MethodDelete,
				http.MethodOptions,
				http.MethodPatch,
				http.MethodDelete,
			},
			AllowCredentials: true,
			MaxAge:           3600,
		})),
		stdmw.Secure(),
		stdmw.RateLimiter(stdmw.NewRateLimiterMemoryStore(30)),
	}
}

func httpErr(err error, c echo.Context) {
	if s, ok := status.FromError(err); ok {
		he := httpStatusPbFromRPC(s)
		jsonb, _ := protojson.Marshal(he)
		c.JSONBlob(int(he.Error.Code), jsonb)
		return
	}

	if he, ok := err.(*echo.HTTPError); ok {
		var s *status.Status
		switch he.Code {
		case http.StatusNotFound, http.StatusMethodNotAllowed:
			s = status.New(codes.NotFound, "Not found!")

		case http.StatusTooManyRequests:
			s = status.New(codes.ResourceExhausted, "Too many requests!")

		case http.StatusInternalServerError:
			s = status.New(codes.Internal, "An internal server error occurred!")

		default:
			s = status.New(codes.Unknown, "An unknown error occurred!")
		}

		hpb := httpStatusPbFromRPC(s)
		jsonb, _ := protojson.Marshal(hpb)
		c.JSONBlob(int(hpb.Error.Code), jsonb)
		return
	}

	hpb := httpStatusPbFromRPC(status.New(codes.Internal, "An internal server error occurred!"))
	jsonb, _ := protojson.Marshal(hpb)
	c.JSONBlob(int(hpb.Error.Code), jsonb)
}

func httpStatusPbFromRPC(s *status.Status) *httpPb.Error {
	return &httpPb.Error{
		Error: &httpPb.Status{
			Code:    int32(runtime.HTTPStatusFromCode(s.Code())),
			Message: s.Message(),
			Status:  code.Code(s.Code()),
			Details: s.Proto().GetDetails(),
		},
	}
}

func newLogger() (*zap.Logger, error) {
	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "time",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		MessageKey:     "msg",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.CapitalColorLevelEncoder,
		EncodeTime:     zapcore.TimeEncoderOfLayout("02/01/2006 15:04:05 Z07:00"),
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	config := zap.Config{
		Level:            zap.NewAtomicLevelAt(zapcore.DebugLevel),
		Development:      false,
		Encoding:         "console",
		EncoderConfig:    encoderConfig,
		OutputPaths:      []string{"stdout"},
		ErrorOutputPaths: []string{"stderr"},
	}

	zlog, err := config.Build()
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	return zlog, nil
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
