package server

import (
	"errors"
	"net/http"

	"github.com/10664kls/automatic-finance-api/internal/auth"
	"github.com/labstack/echo/v4"
	edPb "google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	rpcStatus "google.golang.org/grpc/status"
)

type Server struct {
	auth *auth.Auth
}

func NewServer(auth *auth.Auth) (*Server, error) {
	if auth == nil {
		return nil, errors.New("auth is nil")
	}

	return &Server{
		auth: auth,
	}, nil
}

func (s *Server) Install(e *echo.Echo, mws ...echo.MiddlewareFunc) error {
	if e == nil {
		return errors.New("echo is nil")
	}

	v1 := e.Group("/v1")

	v1.POST("/auth/login", s.login)
	v1.POST("/auth/token", s.refreshToken)
	v1.GET("/auth/profile", s.profile, mws...)

	return nil
}

func (s *Server) profile(c echo.Context) error {
	profile, err := s.auth.Profile(c.Request().Context())
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, echo.Map{
		"profile": profile,
	})
}

func badJSON() error {
	s, _ := rpcStatus.New(codes.InvalidArgument, "Request body must be a valid JSON.").
		WithDetails(&edPb.ErrorInfo{
			Reason: "BINDING_ERROR",
			Domain: "http",
		})

	return s.Err()
}

func badParam() error {
	s, _ := rpcStatus.New(codes.InvalidArgument, "Request parameters must be a valid type.").
		WithDetails(&edPb.ErrorInfo{
			Reason: "BINDING_ERROR",
			Domain: "http",
		})

	return s.Err()
}

func (s *Server) login(c echo.Context) error {
	req := new(auth.LoginReq)
	if err := c.Bind(req); err != nil {
		return badJSON()
	}

	token, err := s.auth.Login(c.Request().Context(), req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, token)
}

func (s *Server) refreshToken(c echo.Context) error {
	req := new(auth.NewTokenReq)
	if err := c.Bind(req); err != nil {
		return badJSON()
	}

	token, err := s.auth.RefreshToken(c.Request().Context(), req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, token)
}
