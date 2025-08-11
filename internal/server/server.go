package server

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/10664kls/automatic-finance-api/internal/auth"
	"github.com/10664kls/automatic-finance-api/internal/cib"
	"github.com/10664kls/automatic-finance-api/internal/currency"
	"github.com/10664kls/automatic-finance-api/internal/income"
	"github.com/10664kls/automatic-finance-api/internal/selfemployed"
	"github.com/10664kls/automatic-finance-api/internal/statement"
	"github.com/labstack/echo/v4"
	edPb "google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	rpcStatus "google.golang.org/grpc/status"
)

type Server struct {
	auth         *auth.Auth
	currency     *currency.Service
	statement    *statement.Service
	income       *income.Service
	selfemployed *selfemployed.Service
	cib          *cib.Service
}

func NewServer(auth *auth.Auth, currency *currency.Service, income *income.Service, statement *statement.Service, cib *cib.Service, selfemployed *selfemployed.Service) (*Server, error) {
	if auth == nil {
		return nil, errors.New("auth service is nil")
	}
	if currency == nil {
		return nil, errors.New("currency service is nil")
	}
	if income == nil {
		return nil, errors.New("income service is nil")
	}
	if cib == nil {
		return nil, errors.New("cib service is nil")
	}
	if statement == nil {
		return nil, errors.New("statement service is nil")
	}
	if selfemployed == nil {
		return nil, errors.New("selfemployed service is nil")
	}

	return &Server{
		auth:         auth,
		currency:     currency,
		income:       income,
		statement:    statement,
		cib:          cib,
		selfemployed: selfemployed,
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
	v1.POST("/files/cib", s.uploadCIB, mws...)
	v1.GET("/files/cib/:name", s.downloadCIB, mws...)

	v1.POST("/incomes/calculations", s.calculateIncome, mws...)
	v1.GET("/incomes/calculations", s.listIncomeCalculations, mws...)
	v1.GET("/incomes/calculations/:number", s.getIncomeCalculationByNumber, mws...)
	v1.PUT("/incomes/calculations/:number", s.recalculateIncome, mws...)
	v1.POST("/incomes/calculations/:number/complete", s.completeIncomeCalculation, mws...)
	v1.POST("/incomes/calculations/:number/transactions", s.listIncomeTransactionsByNumber, mws...)
	v1.GET("/incomes/calculations/:number/transactions/:billNumber", s.getIncomeTransactionByBillNumber, mws...)
	v1.GET("/incomes/calculations/:number/export-to-excel", s.exportIncomeCalculationToExcelByNumber, mws...)
	v1.GET("/incomes/calculations/export-to-excel", s.exportIncomeCalculationsToExcel, mws...)

	v1.GET("/incomes/wordlists", s.listIncomeWordlists, mws...)
	v1.GET("/incomes/wordlists/:id", s.getIncomeWordlistByID, mws...)
	v1.POST("/incomes/wordlists", s.createIncomeWordlist, mws...)
	v1.PUT("/incomes/wordlists/:id", s.updateIncomeWordlist, mws...)

	v1.GET("/cib/calculations", s.listCIBCalculations, mws...)
	v1.GET("/cib/calculations/:number", s.getCIBCalculationByNumber, mws...)
	v1.POST("/cib/calculations", s.calculateCIB, mws...)
	v1.GET("/cib/calculations/:number/export-to-excel", s.exportCIBCalculationToExcelByNumber, mws...)
	v1.GET("/cib/calculations/export-to-excel", s.exportCIBCalculationsToExcel, mws...)

	v1.POST("/selfemployed/calculations", s.calculateSelfEmployedIncome, mws...)
	v1.GET("/selfemployed/calculations", s.listSelfEmployedIncomeCalculations, mws...)
	v1.GET("/selfemployed/calculations/:number", s.getSelfEmployedIncomeCalculationByNumber, mws...)
	v1.PUT("/selfemployed/calculations/:number", s.recalculateSelfEmployedIncome, mws...)
	v1.PATCH("/selfemployed/calculations/:number/complete", s.completeSelfEmployedIncomeCalculationByNumber, mws...)
	v1.POST("/selfemployed/calculations/:number/transactions", s.listSelfEmployedIncomeTransactions, mws...)
	v1.GET("/selfemployed/calculations/:number/transactions/:billNumber", s.getSelfEmployedIncomeTransactionByBillNumber, mws...)

	v1.GET("/selfemployed/wordlists", s.listSelfEmployedWordlists, mws...)
	v1.GET("/selfemployed/wordlists/:id", s.getSelfEmployedWordlistByID, mws...)
	v1.POST("/selfemployed/wordlists", s.createSelfEmployedWordlist, mws...)
	v1.PUT("/selfemployed/wordlists/:id", s.updateSelfEmployedWordlist, mws...)

	v1.GET("/selfemployed/businesses", s.listSelfEmployedBusinesses, mws...)
	v1.GET("/selfemployed/businesses/:id", s.getSelfEmployedBusinessByID, mws...)
	v1.POST("/selfemployed/businesses", s.createSelfEmployedBusiness, mws...)
	v1.PUT("/selfemployed/businesses/:id", s.updateSelfEmployedBusiness, mws...)

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
	sf, err := s.statement.UploadStatement(ctx, &statement.StatementFileReq{
		ReadSeeker:   src,
		OriginalName: f.Filename,
	})
	if err != nil {
		return err
	}

	sf.PublicURL = s.statement.SignedURL(ctx, sf)

	return c.JSON(http.StatusOK, echo.Map{
		"metadata": sf,
	})
}

func (s *Server) downloadStatement(c echo.Context) error {
	name, signature := c.Param("name"), c.QueryParam("signature")
	f, err := s.statement.GetStatement(c.Request().Context(), name, signature)
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

func (s *Server) listIncomeTransactionsByNumber(c echo.Context) error {
	req := new(income.TransactionReq)
	if err := c.Bind(req); err != nil {
		return badJSON()
	}

	transactions, err := s.income.ListIncomeTransactionsByNumber(c.Request().Context(), req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, transactions)
}

func (s *Server) getIncomeTransactionByBillNumber(c echo.Context) error {
	req := new(income.GetTransactionReq)
	if err := c.Bind(req); err != nil {
		return badJSON()
	}

	transaction, err := s.income.GetIncomeTransactionByBillNumber(c.Request().Context(), req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, echo.Map{
		"transaction": transaction,
	})
}

func (s *Server) getIncomeWordlistByID(c echo.Context) error {
	req := new(income.WordlistReq)
	if err := c.Bind(req); err != nil {
		return badJSON()
	}

	wordlist, err := s.income.GetWordlistByID(c.Request().Context(), req.ID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, echo.Map{
		"wordlist": wordlist,
	})
}

func (s *Server) createIncomeWordlist(c echo.Context) error {
	req := new(income.WordlistReq)
	if err := c.Bind(req); err != nil {
		return badJSON()
	}

	wordlist, err := s.income.CreateWordlist(c.Request().Context(), req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, echo.Map{
		"wordlist": wordlist,
	})
}

func (s *Server) updateIncomeWordlist(c echo.Context) error {
	req := new(income.WordlistReq)
	if err := c.Bind(req); err != nil {
		return badJSON()
	}

	wordlist, err := s.income.UpdateWordlist(c.Request().Context(), req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, echo.Map{
		"wordlist": wordlist,
	})
}

func (s *Server) completeIncomeCalculation(c echo.Context) error {
	calculation, err := s.income.CompleteCalculation(c.Request().Context(), c.Param("number"))
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, echo.Map{
		"calculation": calculation,
	})
}

func (s *Server) exportIncomeCalculationToExcelByNumber(c echo.Context) error {
	buf, err := s.income.ExportCalculationToExcelByNumber(c.Request().Context(), c.Param("number"))
	if err != nil {
		return err
	}

	c.Response().Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Response().Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="Income_calculation_%s.xlsx"`, c.Param("number")))

	return c.Blob(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", buf.Bytes())
}

func (s *Server) exportIncomeCalculationsToExcel(c echo.Context) error {
	req := new(income.BatchGetCalculationsQuery)
	if err := c.Bind(req); err != nil {
		return badJSON()
	}

	buf, err := s.income.ExportCalculationsToExcel(c.Request().Context(), req)
	if err != nil {
		return err
	}

	c.Response().Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Response().Header().Set("Content-Disposition", `attachment; filename="Income_calculations.xlsx"`)

	return c.Blob(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", buf.Bytes())
}

func (s *Server) calculateCIB(c echo.Context) error {
	req := new(cib.CalculateReq)
	if err := c.Bind(req); err != nil {
		return badJSON()
	}

	calculation, err := s.cib.CalculateCIB(c.Request().Context(), req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, echo.Map{
		"calculation": calculation,
	})
}

func (s *Server) getCIBCalculationByNumber(c echo.Context) error {
	calculation, err := s.cib.GetCalculationByNumber(c.Request().Context(), c.Param("number"))
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, echo.Map{
		"calculation": calculation,
	})
}

func (s *Server) listCIBCalculations(c echo.Context) error {
	req := new(cib.CalculationQuery)
	if err := c.Bind(req); err != nil {
		return badJSON()
	}

	calculations, err := s.cib.ListCalculations(c.Request().Context(), req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, calculations)
}

func (s *Server) uploadCIB(c echo.Context) error {
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
	sf, err := s.cib.UploadCIB(ctx, &cib.CIBFileReq{
		ReadSeeker:   src,
		OriginalName: f.Filename,
	})
	if err != nil {
		return err
	}

	sf.PublicURL = s.cib.SignedURL(ctx, sf)

	return c.JSON(http.StatusOK, echo.Map{
		"metadata": sf,
	})
}

func (s *Server) downloadCIB(c echo.Context) error {
	name, signature := c.Param("name"), c.QueryParam("signature")
	f, err := s.cib.GetCIBFile(c.Request().Context(), name, signature)
	if err != nil {
		return err
	}
	return c.Inline(f.Location, f.Name)
}

func (s *Server) exportCIBCalculationToExcelByNumber(c echo.Context) error {
	buf, err := s.cib.ExportCalculationToExcelByNumber(c.Request().Context(), c.Param("number"))
	if err != nil {
		return err
	}

	c.Response().Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Response().Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="CIB_calculation_%s.xlsx"`, c.Param("number")))

	return c.Blob(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", buf.Bytes())
}

func (s *Server) exportCIBCalculationsToExcel(c echo.Context) error {
	req := new(cib.BatchGetCalculationsQuery)
	if err := c.Bind(req); err != nil {
		return badJSON()
	}

	buf, err := s.cib.ExportCalculationsToExcel(c.Request().Context(), req)
	if err != nil {
		return err
	}

	c.Response().Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Response().Header().Set("Content-Disposition", `attachment; filename="CIB_calculations.xlsx"`)

	return c.Blob(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", buf.Bytes())
}

func (s *Server) calculateSelfEmployedIncome(c echo.Context) error {
	req := new(selfemployed.CalculateReq)
	if err := c.Bind(req); err != nil {
		return badJSON()
	}

	calculation, err := s.selfemployed.CalculateIncome(c.Request().Context(), req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, echo.Map{
		"calculation": calculation,
	})
}

func (s *Server) getSelfEmployedIncomeCalculationByNumber(c echo.Context) error {
	calculation, err := s.selfemployed.GetCalculationByNumber(c.Request().Context(), c.Param("number"))
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, echo.Map{
		"calculation": calculation,
	})
}

func (s *Server) listSelfEmployedIncomeCalculations(c echo.Context) error {
	req := new(selfemployed.CalculationQuery)
	if err := c.Bind(req); err != nil {
		return badParam()
	}

	calculations, err := s.selfemployed.ListCalculations(c.Request().Context(), req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, calculations)
}

func (s *Server) recalculateSelfEmployedIncome(c echo.Context) error {
	req := new(selfemployed.RecalculateReq)
	if err := c.Bind(req); err != nil {
		return badJSON()
	}

	calculation, err := s.selfemployed.ReCalculateIncome(c.Request().Context(), req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, echo.Map{
		"calculation": calculation,
	})
}

func (s *Server) completeSelfEmployedIncomeCalculationByNumber(c echo.Context) error {
	calculation, err := s.selfemployed.CompleteCalculation(c.Request().Context(), c.Param("number"))
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, echo.Map{
		"calculation": calculation,
	})
}

func (s *Server) listSelfEmployedIncomeTransactions(c echo.Context) error {
	req := new(selfemployed.TransactionQuery)
	if err := c.Bind(req); err != nil {
		return badJSON()
	}

	transactions, err := s.selfemployed.ListIncomeTransactionsByNumber(c.Request().Context(), req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, transactions)
}

func (s *Server) getSelfEmployedIncomeTransactionByBillNumber(c echo.Context) error {
	req := new(selfemployed.GetTransactionQuery)
	if err := c.Bind(req); err != nil {
		return badParam()
	}

	transaction, err := s.selfemployed.GetIncomeTransactionByBillNumber(c.Request().Context(), req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, echo.Map{
		"transaction": transaction,
	})
}

func (s *Server) listSelfEmployedWordlists(c echo.Context) error {
	req := new(selfemployed.WordlistQuery)
	if err := c.Bind(req); err != nil {
		return badParam()
	}

	wordlists, err := s.selfemployed.ListWordlists(c.Request().Context(), req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, wordlists)
}

func (s *Server) getSelfEmployedWordlistByID(c echo.Context) error {
	req := new(selfemployed.WordlistReq)
	if err := c.Bind(req); err != nil {
		return badParam()
	}

	wordlist, err := s.selfemployed.GetWordlistByID(c.Request().Context(), req.ID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, echo.Map{
		"wordlist": wordlist,
	})
}

func (s *Server) createSelfEmployedWordlist(c echo.Context) error {
	req := new(selfemployed.WordlistReq)
	if err := c.Bind(req); err != nil {
		return badJSON()
	}

	wordlist, err := s.selfemployed.CreateWordlist(c.Request().Context(), req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, echo.Map{
		"wordlist": wordlist,
	})
}

func (s *Server) updateSelfEmployedWordlist(c echo.Context) error {
	req := new(selfemployed.WordlistReq)
	if err := c.Bind(req); err != nil {
		return badJSON()
	}

	wordlist, err := s.selfemployed.UpdateWordlist(c.Request().Context(), req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, echo.Map{
		"wordlist": wordlist,
	})
}

func (s *Server) listSelfEmployedBusinesses(c echo.Context) error {
	req := new(selfemployed.BusinessQuery)
	if err := c.Bind(req); err != nil {
		return badParam()
	}

	businesses, err := s.selfemployed.ListBusinesses(c.Request().Context(), req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, businesses)
}

func (s *Server) getSelfEmployedBusinessByID(c echo.Context) error {
	req := new(selfemployed.BusinessReq)
	if err := c.Bind(req); err != nil {
		return badParam()
	}

	business, err := s.selfemployed.GetBusinessByID(c.Request().Context(), req.ID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, echo.Map{
		"business": business,
	})
}

func (s *Server) createSelfEmployedBusiness(c echo.Context) error {
	req := new(selfemployed.BusinessReq)
	if err := c.Bind(req); err != nil {
		return badJSON()
	}

	business, err := s.selfemployed.CreateBusiness(c.Request().Context(), req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, echo.Map{
		"business": business,
	})
}

func (s *Server) updateSelfEmployedBusiness(c echo.Context) error {
	req := new(selfemployed.BusinessReq)
	if err := c.Bind(req); err != nil {
		return badJSON()
	}

	business, err := s.selfemployed.UpdateBusiness(c.Request().Context(), req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, echo.Map{
		"business": business,
	})
}
