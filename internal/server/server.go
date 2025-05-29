package server

import (
	"errors"
	"net/http"

	"github.com/10664kls/automatic-finance-api/internal/auth"
	"github.com/10664kls/automatic-finance-api/internal/currency"
	"github.com/10664kls/automatic-finance-api/internal/income"
	"github.com/labstack/echo/v4"
	edPb "google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	rpcStatus "google.golang.org/grpc/status"
)

type Server struct {
	auth     *auth.Auth
	currency *currency.Service
	income   *income.Service
}

func NewServer(auth *auth.Auth, currency *currency.Service, income *income.Service) (*Server, error) {
	if auth == nil {
		return nil, errors.New("auth service is nil")
	}
	if currency == nil {
		return nil, errors.New("currency service is nil")
	}
	if income == nil {
		return nil, errors.New("income service is nil")
	}

	return &Server{
		auth:     auth,
		currency: currency,
		income:   income,
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
	v1.POST("/auth/profile/change-password", s.changeMyPassword, mws...)
	v1.PATCH("/auth/profile/change-display-name", s.changeMyDisplayName, mws...)

	v1.POST("/auth/users", s.createUser, mws...)
	v1.GET("/auth/users/:id", s.getUserByID, mws...)
	v1.GET("/auth/users", s.listUsers, mws...)
	v1.POST("/auth/users/:id/reset-password", s.resetUserPasswordByAdmin, mws...)
	v1.POST("/auth/users/:id/disable", s.disableUser, mws...)
	v1.POST("/auth/users/:id/enable", s.enableUser, mws...)
	v1.POST("/auth/users/:id/terminate", s.terminateUser, mws...)

	v1.POST("/currencies", s.createCurrency, mws...)
	v1.GET("/currencies/:id", s.getCurrencyByID, mws...)
	v1.GET("/currencies", s.listCurrencies, mws...)
	v1.PATCH("/currencies/:id", s.updateCurrencyExchangeRate, mws...)

	v1.POST("/files/statements", s.uploadStatement, mws...)
	v1.GET("/files/statements/:name", s.downloadStatement, mws...)

	v1.POST("/incomes/calculations", s.calculateIncome, mws...)
	v1.GET("/incomes/calculations", s.listIncomeCalculations, mws...)
	v1.GET("/incomes/calculations/:number", s.getIncomeCalculationByNumber, mws...)
	v1.PUT("/incomes/calculations/:number", s.recalculateIncome, mws...)

	v1.GET("/incomes/wordlists", s.listIncomeWordlists, mws...)

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

func (s *Server) createUser(c echo.Context) error {
	req := new(auth.CreateUserReq)
	if err := c.Bind(req); err != nil {
		return badJSON()
	}

	user, err := s.auth.CreateUser(c.Request().Context(), req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, echo.Map{
		"user": user,
	})
}

func (s *Server) getUserByID(c echo.Context) error {
	user, err := s.auth.GetUserByID(c.Request().Context(), c.Param("id"))
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, echo.Map{
		"user": user,
	})
}

func (s *Server) listUsers(c echo.Context) error {
	req := new(auth.UserQuery)
	if err := c.Bind(req); err != nil {
		return badJSON()
	}

	users, err := s.auth.ListUsers(c.Request().Context(), req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, users)
}

func (s *Server) changeMyPassword(c echo.Context) error {
	req := new(auth.ChangeMyPasswordReq)
	if err := c.Bind(req); err != nil {
		return badJSON()
	}

	if err := s.auth.ChangeMyPassword(c.Request().Context(), req); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, echo.Map{
		"status":  "SUCCESS",
		"code":    http.StatusOK,
		"message": "Your password has been changed successfully.",
	})
}

func (s *Server) resetUserPasswordByAdmin(c echo.Context) error {
	req := new(auth.ResetUserPasswordByAdminReq)
	if err := c.Bind(req); err != nil {
		return badJSON()
	}

	if err := s.auth.ResetUserPasswordByAdmin(c.Request().Context(), req); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, echo.Map{
		"status":  "SUCCESS",
		"code":    http.StatusOK,
		"message": "The user's password has been reset successfully.",
	})
}

func (s *Server) changeMyDisplayName(c echo.Context) error {
	req := new(auth.ChangeDisplayNameReq)
	if err := c.Bind(req); err != nil {
		return badJSON()
	}

	user, err := s.auth.ChangeMyDisplayName(c.Request().Context(), req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, echo.Map{
		"profile": user,
	})
}

func (s *Server) enableUser(c echo.Context) error {
	user, err := s.auth.EnableUser(c.Request().Context(), c.Param("id"))
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, echo.Map{
		"user": user,
	})
}

func (s *Server) terminateUser(c echo.Context) error {
	user, err := s.auth.TerminateUser(c.Request().Context(), c.Param("id"))
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, echo.Map{
		"user": user,
	})
}

func (s *Server) disableUser(c echo.Context) error {
	user, err := s.auth.DisableUser(c.Request().Context(), c.Param("id"))
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, echo.Map{
		"user": user,
	})
}

func (s *Server) createCurrency(c echo.Context) error {
	req := new(currency.CreateReq)
	if err := c.Bind(req); err != nil {
		return badJSON()
	}

	currency, err := s.currency.CreateCurrency(c.Request().Context(), req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, echo.Map{
		"currency": currency,
	})
}

func (s *Server) updateCurrencyExchangeRate(c echo.Context) error {
	req := new(currency.ExchangeRateReq)
	if err := c.Bind(req); err != nil {
		return badJSON()
	}

	currency, err := s.currency.UpdateExchangeRate(c.Request().Context(), req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, echo.Map{
		"currency": currency,
	})
}

func (s *Server) listCurrencies(c echo.Context) error {
	req := new(currency.Query)
	if err := c.Bind(req); err != nil {
		return badJSON()
	}

	currencies, err := s.currency.ListCurrencies(c.Request().Context(), req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, currencies)
}

func (s *Server) getCurrencyByID(c echo.Context) error {
	currency, err := s.currency.GetCurrencyByID(c.Request().Context(), c.Param("id"))
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, echo.Map{
		"currency": currency,
	})
}

func (s *Server) uploadStatement(c echo.Context) error {
	f, err := c.FormFile("file")
	if errors.Is(err, http.ErrMissingFile) {
		st, _ := status.New(codes.InvalidArgument, "File must not be empty.").
			WithDetails(&edPb.BadRequest{
				FieldViolations: []*edPb.BadRequest_FieldViolation{
					{
						Field:       "file",
						Description: "File must not be empty.",
					},
				},
			})
		return st.Err()
	}
	if err != nil {
		return err
	}

	src, err := f.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	ctx := c.Request().Context()
	sf, err := s.income.UploadStatement(ctx, &income.FileStatementReq{
		ReadSeeker:   src,
		OriginalName: f.Filename,
	})
	if err != nil {
		return err
	}

	sf.PublicURL = s.income.SignedURL(ctx, sf)

	return c.JSON(http.StatusOK, echo.Map{
		"metadata": sf,
	})
}

func (s *Server) downloadStatement(c echo.Context) error {
	name, signature := c.Param("name"), c.QueryParam("signature")
	f, err := s.income.GetStatement(c.Request().Context(), name, signature)
	if err != nil {
		return err
	}
	return c.Inline(f.Location, f.Name)
}

func (s *Server) calculateIncome(c echo.Context) error {
	req := new(income.CalculateReq)
	if err := c.Bind(req); err != nil {
		return badJSON()
	}

	calculation, err := s.income.CalculateIncome(c.Request().Context(), req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, echo.Map{
		"calculation": calculation,
	})
}

func (s *Server) listIncomeCalculations(c echo.Context) error {
	req := new(income.CalculationQuery)
	if err := c.Bind(req); err != nil {
		return badJSON()
	}

	calculations, err := s.income.ListCalculations(c.Request().Context(), req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, calculations)
}

func (s *Server) getIncomeCalculationByNumber(c echo.Context) error {
	calculation, err := s.income.GetCalculationByNumber(c.Request().Context(), c.Param("number"))
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, echo.Map{
		"calculation": calculation,
	})
}

func (s *Server) recalculateIncome(c echo.Context) error {
	req := new(income.RecalculateReq)
	if err := c.Bind(req); err != nil {
		return badJSON()
	}

	salaries, err := s.income.ReCalculateIncome(c.Request().Context(), req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, salaries)
}

func (s *Server) listIncomeWordlists(c echo.Context) error {
	req := new(income.WordlistQuery)
	if err := c.Bind(req); err != nil {
		return badJSON()
	}

	wordlists, err := s.income.ListWordlists(c.Request().Context(), req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, wordlists)
}
