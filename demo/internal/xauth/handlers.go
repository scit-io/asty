package xauth

import (
	"context"
	"crypto/hmac"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"asty/demo/middleware"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/nats-io/nats.go/micro"
	"github.com/rs/zerolog"
)

type Handlers struct {
	kv  jetstream.KeyValue
	cfg Config
	log zerolog.Logger
}

func NewHandlers(kv jetstream.KeyValue, cfg Config, log zerolog.Logger) *Handlers {
	return &Handlers{kv: kv, cfg: cfg, log: log}
}

func (h *Handlers) Login(req micro.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.Unmarshal(req.Data(), &body); err != nil {
		_ = req.Error("400", "invalid json", nil)
		return
	}

	userOK := hmac.Equal([]byte(body.Username), []byte(h.cfg.Username))
	passOK := hmac.Equal([]byte(body.Password), []byte(h.cfg.Password))
	if !userOK || !passOK {
		_ = req.Error("401", "invalid credentials", nil)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	accessCookie, refreshCookie, err := h.issueTokenCookies(ctx)
	if err != nil {
		h.log.Error().Err(err).Msg("Login: issue tokens failed")
		_ = req.Error("500", "failed to issue tokens", nil)
		return
	}

	h.log.Info().Str("user", h.cfg.Username).Msg("login ok")
	_ = req.RespondJSON(map[string]string{"status": "ok"}, withCookies(accessCookie, refreshCookie))
}

func (h *Handlers) Refresh(req micro.Request) {
	c, code, errMsg := middleware.VerifyCookieToken(req.Headers().Get("Cookie"), "refresh_token", h.cfg.RefreshSecret)
	if code != 0 {
		_ = req.Error(strconv.Itoa(code), errMsg, nil)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	entry, err := h.kv.Get(ctx, c.Jti)
	if err != nil {
		if err == jetstream.ErrKeyNotFound {
			_ = req.Error("401", "refresh token revoked", nil)
			return
		}
		h.log.Error().Err(err).Str("jti", c.Jti).Msg("Refresh: KV get error")
		_ = req.Error("500", "internal error", nil)
		return
	}
	if string(entry.Value()) == "revoked" {
		_ = req.Error("401", "refresh token revoked", nil)
		return
	}

	accessCookie, refreshCookie, err := h.issueTokenCookies(ctx)
	if err != nil {
		h.log.Error().Err(err).Msg("Refresh: issue tokens failed")
		_ = req.Error("500", "failed to issue tokens", nil)
		return
	}

	if _, err := h.kv.Put(ctx, c.Jti, []byte("revoked")); err != nil {
		h.log.Warn().Err(err).Str("jti", c.Jti).Msg("Refresh: revoke old JTI failed")
	}

	h.log.Info().Str("user", c.Sub).Msg("refresh ok")
	_ = req.RespondJSON(map[string]string{"status": "ok"}, withCookies(accessCookie, refreshCookie))
}

func (h *Handlers) Logout(req micro.Request) {
	// Best-effort revoke — runs even on an expired-but-valid signature,
	// so VerifyJWT (no Exp check) is the right primitive here, not
	// VerifyCookieToken.
	if rawRefresh := middleware.CookieValue(req.Headers().Get("Cookie"), "refresh_token"); rawRefresh != "" {
		if c, err := middleware.VerifyJWT(rawRefresh, h.cfg.RefreshSecret); err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if _, err := h.kv.Put(ctx, c.Jti, []byte("revoked")); err != nil {
				h.log.Warn().Err(err).Str("jti", c.Jti).Msg("Logout: revoke failed")
			}
		}
	}

	clearAccess := buildSetCookie("access_token", "", h.cfg.CookieDomain, -1, h.cfg.CookieSecure, h.cfg.CookieSameSite)
	clearRefresh := buildSetCookie("refresh_token", "", h.cfg.CookieDomain, -1, h.cfg.CookieSecure, h.cfg.CookieSameSite)

	h.log.Info().Msg("logout")
	_ = req.RespondJSON(map[string]string{"status": "ok"}, withCookies(clearAccess, clearRefresh))
}

func (h *Handlers) Me(req micro.Request) {
	c, code, errMsg := middleware.VerifyCookieToken(req.Headers().Get("Cookie"), "access_token", h.cfg.AccessSecret)
	if code != 0 {
		_ = req.Error(strconv.Itoa(code), errMsg, nil)
		return
	}
	_ = req.RespondJSON(map[string]any{"sub": c.Sub, "exp": c.Exp, "iat": c.Iat})
}

// withCookies returns a micro RespondOpt that adds Set-Cookie headers
// to the response. Set-Cookie is the only header in our replies that
// must carry multiple values, hence the dedicated helper.
func withCookies(cookies ...string) micro.RespondOpt {
	return micro.WithHeaders(micro.Headers{"Set-Cookie": cookies})
}

func (h *Handlers) issueTokenCookies(ctx context.Context) (string, string, error) {
	now := time.Now()

	accessJTI, err := middleware.NewJTI()
	if err != nil {
		return "", "", fmt.Errorf("jti(access): %w", err)
	}
	refreshJTI, err := middleware.NewJTI()
	if err != nil {
		return "", "", fmt.Errorf("jti(refresh): %w", err)
	}

	access, err := middleware.SignJWT(middleware.Claims{
		Sub: h.cfg.Username, Exp: now.Add(h.cfg.AccessTTL).Unix(),
		Jti: accessJTI, Iat: now.Unix(),
	}, h.cfg.AccessSecret)
	if err != nil {
		return "", "", fmt.Errorf("sign access: %w", err)
	}

	refresh, err := middleware.SignJWT(middleware.Claims{
		Sub: h.cfg.Username, Exp: now.Add(h.cfg.RefreshTTL).Unix(),
		Jti: refreshJTI, Iat: now.Unix(),
	}, h.cfg.RefreshSecret)
	if err != nil {
		return "", "", fmt.Errorf("sign refresh: %w", err)
	}

	expBytes := []byte(fmt.Sprintf("%d", now.Add(h.cfg.RefreshTTL).Unix()))
	if _, err := h.kv.Put(ctx, refreshJTI, expBytes); err != nil {
		return "", "", fmt.Errorf("kv put refresh jti: %w", err)
	}

	accessCookie := buildSetCookie("access_token", access, h.cfg.CookieDomain,
		int(h.cfg.AccessTTL.Seconds()), h.cfg.CookieSecure, h.cfg.CookieSameSite)
	refreshCookie := buildSetCookie("refresh_token", refresh, h.cfg.CookieDomain,
		int(h.cfg.RefreshTTL.Seconds()), h.cfg.CookieSecure, h.cfg.CookieSameSite)

	return accessCookie, refreshCookie, nil
}

func buildSetCookie(name, value, domain string, maxAge int, secure bool, sameSite http.SameSite) string {
	c := &http.Cookie{
		Name: name, Value: value, Path: "/", Domain: domain,
		MaxAge: maxAge, HttpOnly: true, Secure: secure, SameSite: sameSite,
	}
	return c.String()
}
