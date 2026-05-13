package xauth

import (
	"context"
	"crypto/hmac"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"asty/demo/utils"

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
		utils.Error(req, 400, "invalid json")
		return
	}

	userOK := hmac.Equal([]byte(body.Username), []byte(h.cfg.Username))
	passOK := hmac.Equal([]byte(body.Password), []byte(h.cfg.Password))
	if !userOK || !passOK {
		utils.Error(req, 401, "invalid credentials")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	accessCookie, refreshCookie, err := h.issueTokenCookies(ctx)
	if err != nil {
		h.log.Error().Err(err).Msg("Login: issue tokens failed")
		utils.Error(req, 500, "failed to issue tokens")
		return
	}

	h.log.Info().Str("user", h.cfg.Username).Msg("login ok")
	utils.JSON(req, 200, map[string]string{"status": "ok"}, accessCookie, refreshCookie)
}

func (h *Handlers) Refresh(req micro.Request) {
	rawRefresh := getCookie(req, "refresh_token")
	if rawRefresh == "" {
		utils.Error(req, 401, "refresh token missing")
		return
	}

	c, err := VerifyJWT(rawRefresh, h.cfg.RefreshSecret)
	if err != nil {
		utils.Error(req, 401, "invalid refresh token")
		return
	}
	if time.Now().Unix() > c.Exp+int64(JWTClockSkew.Seconds()) {
		utils.Error(req, 401, "refresh token expired")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	entry, err := h.kv.Get(ctx, c.Jti)
	if err != nil {
		if err == jetstream.ErrKeyNotFound {
			utils.Error(req, 401, "refresh token revoked")
			return
		}
		h.log.Error().Err(err).Str("jti", c.Jti).Msg("Refresh: KV get error")
		utils.Error(req, 500, "internal error")
		return
	}
	if string(entry.Value()) == "revoked" {
		utils.Error(req, 401, "refresh token revoked")
		return
	}

	accessCookie, refreshCookie, err := h.issueTokenCookies(ctx)
	if err != nil {
		h.log.Error().Err(err).Msg("Refresh: issue tokens failed")
		utils.Error(req, 500, "failed to issue tokens")
		return
	}

	if _, err := h.kv.Put(ctx, c.Jti, []byte("revoked")); err != nil {
		h.log.Warn().Err(err).Str("jti", c.Jti).Msg("Refresh: revoke old JTI failed")
	}

	h.log.Info().Str("user", c.Sub).Msg("refresh ok")
	utils.JSON(req, 200, map[string]string{"status": "ok"}, accessCookie, refreshCookie)
}

func (h *Handlers) Logout(req micro.Request) {
	if rawRefresh := getCookie(req, "refresh_token"); rawRefresh != "" {
		if c, err := VerifyJWT(rawRefresh, h.cfg.RefreshSecret); err == nil {
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
	utils.JSON(req, 200, map[string]string{"status": "ok"}, clearAccess, clearRefresh)
}

func (h *Handlers) Me(req micro.Request) {
	rawAccess := getCookie(req, "access_token")
	if rawAccess == "" {
		utils.Error(req, 401, "access token missing")
		return
	}

	c, err := VerifyJWT(rawAccess, h.cfg.AccessSecret)
	if err != nil {
		utils.Error(req, 401, "invalid access token")
		return
	}
	if time.Now().Unix() > c.Exp+int64(JWTClockSkew.Seconds()) {
		utils.Error(req, 401, "access token expired")
		return
	}

	data, _ := json.Marshal(map[string]any{"sub": c.Sub, "exp": c.Exp, "iat": c.Iat})
	req.Respond(data, micro.WithHeaders(micro.Headers{"Status": []string{"200"}}))
}

func (h *Handlers) issueTokenCookies(ctx context.Context) (string, string, error) {
	now := time.Now()

	accessJTI, err := NewJTI()
	if err != nil {
		return "", "", fmt.Errorf("jti(access): %w", err)
	}
	refreshJTI, err := NewJTI()
	if err != nil {
		return "", "", fmt.Errorf("jti(refresh): %w", err)
	}

	access, err := SignJWT(Claims{
		Sub: h.cfg.Username, Exp: now.Add(h.cfg.AccessTTL).Unix(),
		Jti: accessJTI, Iat: now.Unix(),
	}, h.cfg.AccessSecret)
	if err != nil {
		return "", "", fmt.Errorf("sign access: %w", err)
	}

	refresh, err := SignJWT(Claims{
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

func getCookie(req micro.Request, name string) string {
	raw := req.Headers().Get("Cookie")
	if raw == "" {
		return ""
	}
	r := &http.Request{Header: http.Header{"Cookie": []string{raw}}}
	c, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return c.Value
}

func buildSetCookie(name, value, domain string, maxAge int, secure bool, sameSite http.SameSite) string {
	c := &http.Cookie{
		Name: name, Value: value, Path: "/", Domain: domain,
		MaxAge: maxAge, HttpOnly: true, Secure: secure, SameSite: sameSite,
	}
	return c.String()
}
