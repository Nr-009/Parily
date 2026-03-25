package auth

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
	"parily.dev/app/internal/config"
	pg "parily.dev/app/internal/postgres"
)

type Handler struct {
	db  *pgxpool.Pool
	cfg *config.Config
	log *zap.Logger
}

func NewHandler(db *pgxpool.Pool, cfg *config.Config, log *zap.Logger) *Handler {
	return &Handler{db: db, cfg: cfg, log: log}
}

func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.POST("/register", h.Register)
	rg.POST("/login", h.Login)
	rg.POST("/logout", h.Logout)
	rg.GET("/google", h.GoogleLogin)
	rg.GET("/callback", h.GoogleCallback)
}

// ── Register ──────────────────────────────────────────────────────────────────
// POST /auth/register
// Body:    { "email", "name", "password" }
// Returns: { "user": { "id", "email", "name", "created_at" } }

type registerRequest struct {
	Email    string `json:"email"    binding:"required,email"`
	Name     string `json:"name"     binding:"required,min=2,max=255"`
	Password string `json:"password" binding:"required,min=8"`
}

func (h *Handler) Register(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	req.Name = strings.TrimSpace(req.Name)

	// Reject duplicate emails before trying to insert
	existing, err := pg.GetUserByEmail(c.Request.Context(), h.db, req.Email)
	if err != nil && err != pgx.ErrNoRows {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}
	if existing != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "email already registered"})
		return
	}

	hashed, err := HashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not hash password"})
		return
	}

	user, err := pg.CreateUser(c.Request.Context(), h.db, req.Email, req.Name, hashed)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create user"})
		return
	}

	if err := IssueToken(c, user.ID, user.Email, h.cfg.JWTSecret, h.cfg.JWTExpiryHours); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not issue token"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"user": gin.H{
			"id":         user.ID,
			"email":      user.Email,
			"name":       user.Name,
			"created_at": user.CreatedAt.Format(time.RFC3339),
		},
	})
}

// ── Login ─────────────────────────────────────────────────────────────────────
// POST /auth/login
// Body:    { "email", "password" }
// Returns: { "user": { "id", "email", "name", "created_at" } }
//
// "email not found" and "wrong password" return the same error intentionally —
// leaking which one failed lets attackers enumerate valid emails.

type loginRequest struct {
	Email    string `json:"email"    binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

func (h *Handler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	req.Email = strings.ToLower(strings.TrimSpace(req.Email))

	user, err := pg.GetUserByEmail(c.Request.Context(), h.db, req.Email)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid email or password"})
		return
	}

	if err := CheckPassword(req.Password, user.PasswordHash); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid email or password"})
		return
	}

	if err := IssueToken(c, user.ID, user.Email, h.cfg.JWTSecret, h.cfg.JWTExpiryHours); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not issue token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"user": gin.H{
			"id":         user.ID,
			"email":      user.Email,
			"name":       user.Name,
			"created_at": user.CreatedAt.Format(time.RFC3339),
		},
	})
}

// ── Logout ────────────────────────────────────────────────────────────────────
// POST /auth/logout — clears the JWT cookie.

func (h *Handler) Logout(c *gin.Context) {
	ClearToken(c)
	c.JSON(http.StatusOK, gin.H{"message": "logged out"})
}

func (h *Handler) Me(c *gin.Context) {
	userID := c.GetString("userID")

	user, err := pg.GetUserByID(c.Request.Context(), h.db, userID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"user": gin.H{
			"id":    user.ID,
			"email": user.Email,
			"name":  user.Name,
		},
	})
}

const stateCookieName = "oauth_state"

// GoogleLogin handles GET /auth/google
// Generates a random state string, stores it in a cookie,
// then redirects the browser to Google's consent screen.
func (h *Handler) GoogleLogin(c *gin.Context) {
	cfg := newOAuthConfig(
		h.cfg.GoogleClientID,
		h.cfg.GoogleClientSecret,
		h.cfg.GoogleRedirectURL,
	)

	state, err := generateState()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not generate state"})
		return
	}

	http.SetCookie(c.Writer, &http.Cookie{
		Name:     stateCookieName,
		Value:    state,
		Path:     "/",
		MaxAge:   300, // 5 minutes — enough time to complete OAuth flow
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	// Redirect browser to Google consent screen
	c.Redirect(http.StatusTemporaryRedirect, buildAuthURL(cfg, state))
}

// GoogleCallback handles GET /auth/callback
// Google redirects here after user approves.
// Verifies state, exchanges code for user info, upserts user, issues JWT.
func (h *Handler) GoogleCallback(c *gin.Context) {
	// 1. Verify state matches what we sent — CSRF protection
	stateCookie, err := c.Cookie(stateCookieName)
	if err != nil || stateCookie != c.Query("state") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid state"})
		return
	}

	// Clear the state cookie
	http.SetCookie(c.Writer, &http.Cookie{
		Name:   stateCookieName,
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})

	// 2. Exchange code for Google user info (server-to-server)
	code := c.Query("code")
	if code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing code"})
		return
	}

	cfg := newOAuthConfig(
		h.cfg.GoogleClientID,
		h.cfg.GoogleClientSecret,
		h.cfg.GoogleRedirectURL,
	)

	googleUser, err := exchangeCode(c.Request.Context(), cfg, code)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not exchange code"})
		return
	}

	// 3. Upsert user in PostgreSQL
	user, err := pg.UpsertGoogleUser(
		c.Request.Context(),
		h.db,
		googleUser.ID,
		googleUser.Email,
		googleUser.Name,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create user"})
		return
	}

	// 4. Issue JWT cookie — same as email/password login
	if err := IssueToken(c, user.ID, user.Email, h.cfg.JWTSecret, h.cfg.JWTExpiryHours); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not issue token"})
		return
	}
	// 5. Redirect to frontend dashboard
	c.Redirect(http.StatusTemporaryRedirect, "http://localhost:5173/dashboard")
}
