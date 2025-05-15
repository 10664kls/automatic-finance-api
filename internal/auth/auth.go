package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"aidanwoods.dev/go-paseto"
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
		return nil, rpcStatus.Error(codes.PermissionDenied, "You are not allowed to access this profile or (it may not exist)")
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
	Username string `json:"username"`
	Password string `json:"password"`
}

func (r *LoginReq) Validate() error {
	violations := make([]*edPb.BadRequest_FieldViolation, 0)

	if r.Username == "" {
		violations = append(violations, &edPb.BadRequest_FieldViolation{
			Field:       "username",
			Description: "Username must not be empty",
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

func (s *Auth) Login(ctx context.Context, in *LoginReq) (*Token, error) {
	zlog := s.zlog.With(
		zap.String("Method", "Login"),
		zap.String("username", in.Username),
	)

	if err := in.Validate(); err != nil {
		return nil, err
	}

	user, err := getUser(ctx, s.db, &UserQuery{
		Username: in.Username,
	})
	if errors.Is(err, ErrUserNotFound) {
		return nil, rpcStatus.Error(codes.Unauthenticated, "Your credentials not valid. Please check your username and password and try again.")
	}
	if err != nil {
		zlog.Error("failed to get user", zap.Error(err))
		return nil, err
	}

	if passed := user.ComparePassword(in.Password); !passed {
		return nil, rpcStatus.Error(codes.Unauthenticated, "Your credentials not valid. Please check your username and password and try again.")
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
			"password_hashed",
			"created_by",
			"updated_by",
			"created_at",
			"updated_at",
		).
		From(`[user]`).
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
	if in.ID == "" {
		return nil, ErrUserNotFound
	}

	users, err := listUsers(ctx, db, in)
	if err != nil {
		return nil, err
	}
	if len(users) == 0 {
		return nil, ErrUserNotFound
	}

	return users[0], nil
}
