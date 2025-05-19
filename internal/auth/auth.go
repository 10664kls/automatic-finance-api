package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/mail"
	"time"

	"aidanwoods.dev/go-paseto"
	"github.com/10664kls/automatic-finance-api/internal/gen"
	"github.com/10664kls/automatic-finance-api/internal/pager"
	sq "github.com/Masterminds/squirrel"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
	edPb "google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	rpcStatus "google.golang.org/grpc/status"
)

// ErrUserNotFound is returned when a user is not found in the database.
var ErrUserNotFound = errors.New("user not found")

type Auth struct {
	db   *sql.DB
	aKey paseto.V4SymmetricKey
	rKey paseto.V4SymmetricKey
	zlog *zap.Logger
}

func New(_ context.Context, db *sql.DB, zlog *zap.Logger, aKey, rKey paseto.V4SymmetricKey) (*Auth, error) {
	if db == nil {
		return nil, errors.New("db is nil")
	}
	if zlog == nil {
		return nil, errors.New("logger is nil")
	}

	return &Auth{
		db:   db,
		aKey: aKey,
		rKey: rKey,
		zlog: zlog,
	}, nil
}

type ListUsersResult struct {
	Users         []*User `json:"users"`
	NextPageToken string  `json:"nextPageToken"`
}

func (s *Auth) ListUsers(ctx context.Context, in *UserQuery) (*ListUsersResult, error) {
	claims := ClaimsFromContext(ctx)

	zlog := s.zlog.With(
		zap.String("Method", "ListUsers"),
		zap.String("Username", claims.Username),
	)

	if !claims.IsAdmin {
		return nil, rpcStatus.Error(codes.PermissionDenied, "You are not allowed to this resource or (it may not exist)")
	}

	users, err := listUsers(ctx, s.db, in)
	if err != nil {
		zlog.Error("failed to list users", zap.Error(err))
		return nil, err
	}

	var pageToken string
	if l := len(users); l > 0 && l == int(pager.Size(in.PageSize)) {
		last := users[l-1]
		pageToken = pager.EncodeCursor(&pager.Cursor{
			ID:   last.ID,
			Time: last.CreatedAt,
		})
	}

	return &ListUsersResult{
		Users:         users,
		NextPageToken: pageToken,
	}, nil
}

func (s *Auth) GetUserByID(ctx context.Context, id string) (*User, error) {
	claims := ClaimsFromContext(ctx)

	zlog := s.zlog.With(
		zap.String("Method", "GetUserByID"),
		zap.String("Username", claims.Username),
		zap.String("userId", id),
	)

	if !claims.IsAdmin {
		return nil, rpcStatus.Error(codes.PermissionDenied, "You are not allowed to this resource or (it may not exist)")
	}

	user, err := getUser(ctx, s.db, &UserQuery{
		ID: id,
	})
	if errors.Is(err, ErrUserNotFound) {
		return nil, rpcStatus.Error(codes.PermissionDenied, "You are not allowed to access this resource or (it may not exist)")
	}
	if err != nil {
		zlog.Error("failed to get user", zap.Error(err))
		return nil, err
	}

	return user, nil
}

func (s *Auth) CreateUser(ctx context.Context, in *CreateUserReq) (*User, error) {
	claims := ClaimsFromContext(ctx)
	claims.IsAdmin = true
	claims.Username = "System"

	zlog := s.zlog.With(
		zap.String("Method", "CreateUser"),
		zap.String("Username", claims.Username),
	)

	if !claims.IsAdmin {
		return nil, rpcStatus.Error(codes.PermissionDenied, "You are not allowed to this resource or (it may not exist)")
	}

	if err := in.Validate(); err != nil {
		return nil, err
	}

	user := newUser(claims.Username, in)
	exists, err := isEmailAlreadyExists(ctx, s.db, user.ID, user.Email)
	if err != nil {
		zlog.Error("failed to check if email exists", zap.Error(err))
		return nil, err
	}
	if exists {
		return nil, rpcStatus.Error(codes.AlreadyExists, "The user with this email already exists")
	}

	if err := createUser(ctx, s.db, user); err != nil {
		zlog.Error("failed to create user", zap.Error(err))
		return nil, err
	}

	return user, nil
}

func (s *Auth) ChangeMyPassword(ctx context.Context, in *ChangeMyPasswordReq) error {
	claims := ClaimsFromContext(ctx)

	zlog := s.zlog.With(
		zap.String("Method", "ChangeMyPassword"),
		zap.String("Username", claims.Username),
	)

	if err := in.Validate(); err != nil {
		return err
	}

	user, err := getUser(ctx, s.db, &UserQuery{
		ID: claims.ID,
	})
	if errors.Is(err, ErrUserNotFound) {
		return rpcStatus.Error(codes.PermissionDenied, "You are not allowed to access this resource or (it may not exist)")
	}
	if err != nil {
		zlog.Error("failed to get user", zap.Error(err))
		return err
	}

	if passed := user.ComparePassword(in.CurrentPassword); !passed {
		return rpcStatus.Error(codes.FailedPrecondition, "Your current password not valid. Please check your current password and try again.")
	}

	user.changePassword(claims.Username, in.NewPassword)
	if err := updateUser(ctx, s.db, user); err != nil {
		zlog.Error("failed to update user", zap.Error(err))
		return err
	}

	return nil
}

func (s *Auth) ResetUserPasswordByAdmin(ctx context.Context, in *ResetUserPasswordByAdminReq) error {
	claims := ClaimsFromContext(ctx)

	zlog := s.zlog.With(
		zap.String("Method", "ResetUserPasswordByAdmin"),
		zap.String("Username", claims.Username),
		zap.String("userId", in.UserID),
	)

	if !claims.IsAdmin {
		return rpcStatus.Error(codes.PermissionDenied, "You are not allowed to this resource or (it may not exist)")
	}

	if err := in.Validate(); err != nil {
		return err
	}

	user, err := getUser(ctx, s.db, &UserQuery{
		ID: in.UserID,
	})
	if errors.Is(err, ErrUserNotFound) {
		return rpcStatus.Error(codes.PermissionDenied, "You are not allowed to access this resource or (it may not exist)")
	}
	if err != nil {
		zlog.Error("failed to get user", zap.Error(err))
		return err
	}

	user.changePassword(claims.Username, in.Password)
	if err := updateUser(ctx, s.db, user); err != nil {
		zlog.Error("failed to update user", zap.Error(err))
		return err
	}

	return nil
}

func (s *Auth) ChangeMyDisplayName(ctx context.Context, in *ChangeDisplayNameReq) (*User, error) {
	claims := ClaimsFromContext(ctx)

	zlog := s.zlog.With(
		zap.String("Method", "ChangeMyDisplayName"),
		zap.String("Username", claims.Username),
	)

	if err := in.Validate(); err != nil {
		return nil, err
	}

	user, err := getUser(ctx, s.db, &UserQuery{
		ID: claims.ID,
	})
	if errors.Is(err, ErrUserNotFound) {
		return nil, rpcStatus.Error(codes.PermissionDenied, "You are not allowed to access this resource or (it may not exist)")
	}
	if err != nil {
		zlog.Error("failed to get user", zap.Error(err))
		return nil, err
	}

	user.changeDisplayName(claims.Username, in.DisplayName)
	if err := updateUser(ctx, s.db, user); err != nil {
		zlog.Error("failed to update user", zap.Error(err))
		return nil, err
	}

	return user, nil
}

func (s *Auth) Profile(ctx context.Context) (*User, error) {
	claims := ClaimsFromContext(ctx)

	s.zlog.With(
		zap.String("Method", "Profile"),
		zap.String("Username", claims.Username),
	)

	user, err := getUser(ctx, s.db, &UserQuery{
		ID: claims.ID,
	})
	if errors.Is(err, ErrUserNotFound) {
		return nil, rpcStatus.Error(codes.PermissionDenied, "You are not allowed to access this resource or (it may not exist)")
	}
	if err != nil {
		s.zlog.Error("failed to get user", zap.Error(err))
		return nil, err
	}

	return user, nil
}

func (s *Auth) genToken(u *User) (*Token, error) {
	now := time.Now()

	t := paseto.NewToken()
	t.SetSubject(u.Username)
	t.SetIssuedAt(now)
	t.SetNotBefore(now)
	t.SetExpiration(now.Add(time.Hour))
	t.SetFooter([]byte(now.Format(time.RFC3339)))

	if err := t.Set("profile", u.toClaims()); err != nil {
		return nil, fmt.Errorf("failed to set claims: %w", err)
	}

	accessToken := t.V4Encrypt(s.aKey, nil)

	t.SetExpiration(now.Add(time.Hour * 24 * 7))
	refreshToken := t.V4Encrypt(s.rKey, nil)

	return &Token{
		Access:  accessToken,
		Refresh: refreshToken,
	}, nil
}

type LoginReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (r *LoginReq) Validate() error {
	violations := make([]*edPb.BadRequest_FieldViolation, 0)

	if r.Email == "" {
		violations = append(violations, &edPb.BadRequest_FieldViolation{
			Field:       "email",
			Description: "Email must not be empty",
		})
	}

	if _, err := mail.ParseAddress(r.Email); err != nil {
		violations = append(violations, &edPb.BadRequest_FieldViolation{
			Field:       "email",
			Description: "Email must be a valid email address",
		})
	}

	if r.Password == "" {
		violations = append(violations, &edPb.BadRequest_FieldViolation{
			Field:       "password",
			Description: "Password must not be empty",
		})
	}

	if len(violations) > 0 {
		s, _ := rpcStatus.New(
			codes.InvalidArgument,
			"Credentials are not valid or incomplete. Please check the errors and try again, see details for more information.",
		).WithDetails(&edPb.BadRequest{
			FieldViolations: violations,
		})

		return s.Err()
	}
	return nil
}

func (s *Auth) DisableUser(ctx context.Context, id string) (*User, error) {
	claims := ClaimsFromContext(ctx)

	zlog := s.zlog.With(
		zap.String("Method", "DisableUser"),
		zap.String("Username", claims.Username),
		zap.String("userId", id),
	)

	if !claims.IsAdmin {
		return nil, rpcStatus.Error(codes.PermissionDenied, "You are not allowed to this resource or (it may not exist)")
	}

	user, err := getUser(ctx, s.db, &UserQuery{
		ID: id,
	})
	if errors.Is(err, ErrUserNotFound) {
		return nil, rpcStatus.Error(codes.PermissionDenied, "You are not allowed to access this resource or (it may not exist)")
	}
	if err != nil {
		zlog.Error("failed to get user", zap.Error(err))
		return nil, err
	}

	if err := user.Disable(claims.Username); err != nil {
		return nil, err
	}
	if err := updateUser(ctx, s.db, user); err != nil {
		zlog.Error("failed to update user", zap.Error(err))
		return nil, err
	}

	return user, nil
}

func (s *Auth) EnableUser(ctx context.Context, id string) (*User, error) {
	claims := ClaimsFromContext(ctx)

	zlog := s.zlog.With(
		zap.String("Method", "EnableUser"),
		zap.String("Username", claims.Username),
		zap.String("userId", id),
	)

	if !claims.IsAdmin {
		return nil, rpcStatus.Error(codes.PermissionDenied, "You are not allowed to this resource or (it may not exist)")
	}

	user, err := getUser(ctx, s.db, &UserQuery{
		ID: id,
	})
	if errors.Is(err, ErrUserNotFound) {
		return nil, rpcStatus.Error(codes.PermissionDenied, "You are not allowed to access this resource or (it may not exist)")
	}
	if err != nil {
		zlog.Error("failed to get user", zap.Error(err))
		return nil, err
	}

	if err := user.Enable(claims.Username); err != nil {
		return nil, err
	}
	if err := updateUser(ctx, s.db, user); err != nil {
		zlog.Error("failed to update user", zap.Error(err))
		return nil, err
	}

	return user, nil
}

func (s *Auth) TerminateUser(ctx context.Context, id string) (*User, error) {
	claims := ClaimsFromContext(ctx)

	zlog := s.zlog.With(
		zap.String("Method", "TerminateUser"),
		zap.String("Username", claims.Username),
		zap.String("userId", id),
	)

	if !claims.IsAdmin {
		return nil, rpcStatus.Error(codes.PermissionDenied, "You are not allowed to this resource or (it may not exist)")
	}

	user, err := getUser(ctx, s.db, &UserQuery{
		ID: id,
	})
	if errors.Is(err, ErrUserNotFound) {
		return nil, rpcStatus.Error(codes.PermissionDenied, "You are not allowed to access this resource or (it may not exist)")
	}
	if err != nil {
		zlog.Error("failed to get user", zap.Error(err))
		return nil, err
	}

	if err := user.Close(claims.Username); err != nil {
		return nil, err
	}

	if err := updateUser(ctx, s.db, user); err != nil {
		zlog.Error("failed to update user", zap.Error(err))
		return nil, err
	}

	return user, nil
}

func (s *Auth) Login(ctx context.Context, in *LoginReq) (*Token, error) {
	zlog := s.zlog.With(
		zap.String("Method", "Login"),
		zap.String("email", in.Email),
	)

	if err := in.Validate(); err != nil {
		return nil, err
	}

	user, err := getUser(ctx, s.db, &UserQuery{
		Email: in.Email,
	})
	if errors.Is(err, ErrUserNotFound) {
		return nil, rpcStatus.Error(codes.Unauthenticated, "Your credentials not valid. Please check your email and password and try again.")
	}
	if err != nil {
		zlog.Error("failed to get user", zap.Error(err))
		return nil, err
	}

	if passed := user.ComparePassword(in.Password); !passed {
		return nil, rpcStatus.Error(codes.Unauthenticated, "Your credentials not valid. Please check your email and password and try again.")
	}

	token, err := s.genToken(user)
	if err != nil {
		zlog.Error("failed to generate token", zap.Error(err))
		return nil, err
	}

	return token, nil
}

type NewTokenReq struct {
	Token string `json:"token"`
}

func (s *Auth) RefreshToken(ctx context.Context, in *NewTokenReq) (*Token, error) {
	zlog := s.zlog.With(
		zap.String("Method", "RefreshToken"),
		zap.Any("req", in),
	)

	if in.Token == "" {
		return nil, rpcStatus.Error(codes.Unauthenticated, "Your credentials not valid. Please check your token and try again.")
	}

	rules := []paseto.Rule{
		paseto.NotExpired(),
		paseto.ValidAt(time.Now()),
	}

	parser := paseto.MakeParser(rules)
	t, err := parser.ParseV4Local(s.rKey, in.Token, nil)
	if err != nil {
		return nil, rpcStatus.Error(codes.Unauthenticated, "Your credentials not valid. Please check your token and try again.")
	}

	claims := new(Claims)
	if err := t.Get("profile", claims); err != nil {
		return nil, rpcStatus.Error(codes.Unauthenticated, "Your credentials not valid. Please check your token and try again.")
	}

	user, err := getUser(ctx, s.db, &UserQuery{
		ID: claims.ID,
	})
	if errors.Is(err, ErrUserNotFound) {
		return nil, rpcStatus.Error(codes.Unauthenticated, "Your credentials not valid. Please check your token and try again.")
	}
	if err != nil {
		zlog.Error("failed to get user", zap.Error(err))
		return nil, err
	}
	if !user.IsEnabled() {
		return nil, rpcStatus.Error(codes.Unauthenticated, "Your credentials not valid. Please check your token and try again.")
	}

	token, err := s.genToken(user)
	if err != nil {
		zlog.Error("failed to generate token", zap.Error(err))
		return nil, err
	}

	return token, nil
}

type Token struct {
	Access  string `json:"accessToken"`
	Refresh string `json:"refreshToken"`
}

type Claims struct {
	IsAdmin     bool   `json:"isAdmin"`
	ID          string `json:"id"`
	Email       string `json:"email"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
}

type ctxKey int

const claimKey ctxKey = iota

// ClaimsFromContext retrieves the claims from the context.
// If no claims are found, it returns an empty Claims struct.
func ClaimsFromContext(ctx context.Context) *Claims {
	claims, ok := ctx.Value(claimKey).(*Claims)
	if !ok {
		return &Claims{}
	}
	return claims
}

// ContextWithClaims returns a new context with the claims set.
func ContextWithClaims(ctx context.Context, claims *Claims) context.Context {
	return context.WithValue(ctx, claimKey, claims)
}

type User struct {
	IsAdmin        bool `json:"isAdmin"`
	hashedPassword []byte
	createdBy      string
	updatedBy      string
	ID             string    `json:"id"`
	Email          string    `json:"email"`
	Username       string    `json:"username"`
	DisplayName    string    `json:"displayName"`
	Status         status    `json:"status"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

func (u User) toClaims() *Claims {
	return &Claims{
		IsAdmin:     u.IsAdmin,
		ID:          u.ID,
		Email:       u.Email,
		Username:    u.Username,
		DisplayName: u.DisplayName,
	}
}

func (u *User) ComparePassword(password string) bool {
	return bcrypt.CompareHashAndPassword(u.hashedPassword, []byte(password)) == nil
}

func (u *User) SetPassword(password string) error {
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	u.hashedPassword = hashedPassword
	return nil
}

func (u *User) Disable(by string) error {
	if u.Status == StatusDisabled {
		return nil
	}

	if u.Status == StatusClosed {
		return rpcStatus.Error(codes.FailedPrecondition, "The user status is closed. You can not disable this user.")
	}

	u.Status = StatusDisabled
	u.updatedBy = by
	u.UpdatedAt = time.Now()

	return nil
}

func (u *User) Enable(by string) error {
	if u.Status == StatusEnabled {
		return nil
	}

	if u.Status == StatusClosed {
		return rpcStatus.Error(codes.FailedPrecondition, "The user status is closed. You can not enable this user.")
	}

	u.Status = StatusEnabled
	u.updatedBy = by
	u.UpdatedAt = time.Now()

	return nil
}

func (u *User) Close(by string) error {
	if u.Status == StatusClosed {
		return nil
	}

	u.Status = StatusClosed
	u.updatedBy = by
	u.UpdatedAt = time.Now()

	return nil
}

func (u *User) changePassword(by string, password string) {
	u.SetPassword(password)
	u.updatedBy = by
	u.UpdatedAt = time.Now()
}

func (u *User) changeDisplayName(by string, displayName string) {
	u.DisplayName = displayName
	u.updatedBy = u.createdBy
	u.UpdatedAt = time.Now()
}

func (u User) IsEnabled() bool {
	return u.Status == StatusEnabled
}

type UserQuery struct {
	ID            string    `json:"id" param:"id" query:"id"`
	Email         string    `json:"email"  query:"email"`
	Username      string    `json:"username"  query:"username"`
	Status        string    `json:"status"  query:"status"`
	PageToken     string    `json:"pageToken"  query:"pageToken"`
	PageSize      uint64    `json:"pageSize"  query:"pageSize"`
	CreatedAfter  time.Time `json:"createdAfter"  query:"createdAfter"`
	CreatedBefore time.Time `json:"createdBefore"  query:"createdBefore"`
}

func (q *UserQuery) ToSql() (string, []any, error) {
	and := sq.And{}

	if q.ID != "" {
		and = append(and, sq.Eq{"id": q.ID})
	}

	if q.Email != "" {
		and = append(and, sq.Eq{"email": q.Email})
	}

	if q.Username != "" {
		and = append(and, sq.Eq{"username": q.Username})
	}

	if q.Status != "" {
		and = append(and, sq.Eq{"status": q.Status})
	}

	if !q.CreatedAfter.IsZero() {
		and = append(and, sq.GtOrEq{"created_at": q.CreatedAfter})
	}

	if !q.CreatedBefore.IsZero() {
		and = append(and, sq.LtOrEq{"created_at": q.CreatedBefore})
	}

	if q.PageToken != "" {
		cursor, err := pager.DecodeCursor(q.PageToken)
		if err == nil {
			and = append(and, sq.Lt{"created_at": cursor.Time})
		}
	}

	return and.ToSql()
}

func listUsers(ctx context.Context, db *sql.DB, in *UserQuery) ([]*User, error) {
	id := fmt.Sprintf("TOP %d id", pager.Size(in.PageSize))
	pred, args, err := in.ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	q, args := sq.
		Select(
			id,
			"email",
			"username",
			"display_name",
			"status",
			"is_admin",
			"password_hash",
			"created_by",
			"updated_by",
			"created_at",
			"updated_at",
		).
		From(`"user"`).
		Where(pred, args...).
		PlaceholderFormat(sq.AtP).
		OrderBy("created_at DESC").
		MustSql()

	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query for listing users: %w", err)
	}
	defer rows.Close()

	users := make([]*User, 0)
	for rows.Next() {
		var u User
		err := rows.Scan(
			&u.ID,
			&u.Email,
			&u.Username,
			&u.DisplayName,
			&u.Status,
			&u.IsAdmin,
			&u.hashedPassword,
			&u.createdBy,
			&u.updatedBy,
			&u.CreatedAt,
			&u.UpdatedAt,
		)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		if err != nil {
			return nil, err
		}

		users = append(users, &u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read rows for listing users: %w", err)
	}

	return users, nil
}

func getUser(ctx context.Context, db *sql.DB, in *UserQuery) (*User, error) {
	in.PageSize = 1

	users, err := listUsers(ctx, db, in)
	if err != nil {
		return nil, err
	}
	if len(users) == 0 {
		return nil, ErrUserNotFound
	}

	return users[0], nil
}

func createUser(ctx context.Context, db *sql.DB, in *User) error {
	q, args := sq.Insert(`"user"`).
		Columns(
			"id",
			"email",
			"username",
			"display_name",
			"status",
			"is_admin",
			"password_hash",
			"created_by",
			"updated_by",
			"created_at",
			"updated_at",
		).
		Values(
			in.ID,
			in.Email,
			in.Username,
			in.DisplayName,
			in.Status,
			in.IsAdmin,
			in.hashedPassword,
			in.createdBy,
			in.updatedBy,
			in.CreatedAt,
			in.UpdatedAt,
		).
		PlaceholderFormat(sq.AtP).
		MustSql()

	_, err := db.ExecContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("failed to create user: %w", err)
	}

	return nil
}

func updateUser(ctx context.Context, db *sql.DB, in *User) error {
	q, args := sq.Update(`"user"`).
		Set("email", in.Email).
		Set("username", in.Username).
		Set("password_hash", in.hashedPassword).
		Set("display_name", in.DisplayName).
		Set("status", in.Status).
		Set("is_admin", in.IsAdmin).
		Set("updated_by", in.updatedBy).
		Set("updated_at", in.UpdatedAt).
		Where(
			sq.Eq{
				"id": in.ID,
			}).
		PlaceholderFormat(sq.AtP).
		MustSql()

	_, err := db.ExecContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("failed to update user: %w", err)
	}

	return nil
}

type CreateUserReq struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"displayName"`
}

func (r *CreateUserReq) Validate() error {
	violations := make([]*edPb.BadRequest_FieldViolation, 0)

	if r.Email == "" {
		violations = append(violations, &edPb.BadRequest_FieldViolation{
			Field:       "email",
			Description: "Email must not be empty",
		})
	}

	if _, err := mail.ParseAddress(r.Email); err != nil {
		violations = append(violations, &edPb.BadRequest_FieldViolation{
			Field:       "email",
			Description: "Email is not valid",
		})
	}

	if r.Password == "" {
		violations = append(violations, &edPb.BadRequest_FieldViolation{
			Field:       "password",
			Description: "Password must not be empty",
		})
	}

	if len(r.Password) < 8 {
		violations = append(violations, &edPb.BadRequest_FieldViolation{
			Field:       "password",
			Description: "Password must be at least 8 characters long",
		})
	}

	if len(r.Password) > 100 {
		violations = append(violations, &edPb.BadRequest_FieldViolation{
			Field:       "password",
			Description: "Password must be less than 100 characters long",
		})
	}

	if r.DisplayName == "" {
		violations = append(violations, &edPb.BadRequest_FieldViolation{
			Field:       "displayName",
			Description: "Display name must not be empty",
		})
	}

	if len(r.DisplayName) < 2 {
		violations = append(violations, &edPb.BadRequest_FieldViolation{
			Field:       "displayName",
			Description: "Display name must be at least 2 characters long",
		})
	}

	if len(r.DisplayName) > 140 {
		violations = append(violations, &edPb.BadRequest_FieldViolation{
			Field:       "displayName",
			Description: "Display name must be less than 140 characters long",
		})
	}

	if len(violations) > 0 {
		s, _ := rpcStatus.New(
			codes.InvalidArgument,
			"User is not valid or incomplete. Please check the errors and try again, see details for more information.",
		).WithDetails(&edPb.BadRequest{
			FieldViolations: violations,
		})

		return s.Err()
	}

	return nil
}

type ChangeDisplayNameReq struct {
	DisplayName string `json:"displayName"`
}

func (r *ChangeDisplayNameReq) Validate() error {
	violations := make([]*edPb.BadRequest_FieldViolation, 0)

	if r.DisplayName == "" {
		violations = append(violations, &edPb.BadRequest_FieldViolation{
			Field:       "displayName",
			Description: "Display name must not be empty",
		})
	}

	if len(r.DisplayName) < 2 {
		violations = append(violations, &edPb.BadRequest_FieldViolation{
			Field:       "displayName",
			Description: "Display name must be at least 2 characters long",
		})
	}

	if len(r.DisplayName) > 140 {
		violations = append(violations, &edPb.BadRequest_FieldViolation{
			Field:       "displayName",
			Description: "Display name must be less than 140 characters long",
		})
	}

	if len(violations) > 0 {
		s, _ := rpcStatus.New(
			codes.InvalidArgument,
			"Display name is not valid or incomplete. Please check the errors and try again, see details for more information.",
		).WithDetails(&edPb.BadRequest{
			FieldViolations: violations,
		})

		return s.Err()
	}

	return nil
}

type ChangeMyPasswordReq struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

func (r *ChangeMyPasswordReq) Validate() error {
	violations := make([]*edPb.BadRequest_FieldViolation, 0)

	if r.CurrentPassword == "" {
		violations = append(violations, &edPb.BadRequest_FieldViolation{
			Field:       "currentPassword",
			Description: "Current password must not be empty",
		})
	}

	if r.NewPassword == "" {
		violations = append(violations, &edPb.BadRequest_FieldViolation{
			Field:       "newPassword",
			Description: "New password must not be empty",
		})
	}

	if len(r.NewPassword) < 8 {
		violations = append(violations, &edPb.BadRequest_FieldViolation{
			Field:       "newPassword",
			Description: "New password must be at least 8 characters long",
		})
	}

	if len(r.NewPassword) > 100 {
		violations = append(violations, &edPb.BadRequest_FieldViolation{
			Field:       "newPassword",
			Description: "New password must be less than 100 characters long",
		})
	}

	if len(violations) > 0 {
		s, _ := rpcStatus.New(
			codes.InvalidArgument,
			"Change my password is not valid or incomplete. Please check the errors and try again, see details for more information.",
		).WithDetails(&edPb.BadRequest{
			FieldViolations: violations,
		})

		return s.Err()
	}

	return nil
}

type ResetUserPasswordByAdminReq struct {
	UserID   string `json:"userId" param:"id"`
	Password string `json:"password"`
}

func (r *ResetUserPasswordByAdminReq) Validate() error {
	violations := make([]*edPb.BadRequest_FieldViolation, 0)

	if len(r.UserID) != 12 {
		violations = append(violations, &edPb.BadRequest_FieldViolation{
			Field:       "userId",
			Description: "User ID must be a valid user ID",
		})
	}

	if r.Password == "" {
		violations = append(violations, &edPb.BadRequest_FieldViolation{
			Field:       "password",
			Description: "Password must not be empty",
		})
	}

	if len(r.Password) < 8 {
		violations = append(violations, &edPb.BadRequest_FieldViolation{
			Field:       "password",
			Description: "Password must be at least 8 characters long",
		})
	}

	if len(r.Password) > 100 {
		violations = append(violations, &edPb.BadRequest_FieldViolation{
			Field:       "password",
			Description: "Password must be less than 100 characters long",
		})
	}

	if len(violations) > 0 {
		s, _ := rpcStatus.New(
			codes.InvalidArgument,
			"Reset password is not valid or incomplete. Please check the errors and try again, see details for more information.",
		).WithDetails(&edPb.BadRequest{
			FieldViolations: violations,
		})

		return s.Err()
	}

	return nil
}

func newUser(createdBy string, in *CreateUserReq) *User {
	now := time.Now()
	user := &User{
		ID:          gen.ID(),
		Email:       in.Email,
		Username:    in.Email, // In this version, the username is the same as the email.
		DisplayName: in.DisplayName,
		createdBy:   createdBy,
		updatedBy:   createdBy,
		Status:      StatusEnabled,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	user.SetPassword(in.Password)
	return user
}

func isEmailAlreadyExists(ctx context.Context, db *sql.DB, id string, email string) (bool, error) {
	q, args := sq.Select("id").
		From(`"user"`).
		Where(
			sq.And{
				sq.NotEq{"id": id},
				sq.Eq{"email": email},
			}).
		PlaceholderFormat(sq.AtP).
		MustSql()

	var idx string
	row := db.QueryRowContext(ctx, q, args...)
	err := row.Scan(&idx)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to execute query if email exists: %w", err)
	}

	return idx != "", nil
}
