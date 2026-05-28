package xhttp

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/nats-io/nats.go/micro"
	"github.com/rs/zerolog"
)

type Item struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Value     string    `json:"value"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Handlers struct {
	db    *sql.DB
	cache *cache
	cfg   Config
	log   zerolog.Logger
}

func NewHandlers(kv jetstream.KeyValue, db *sql.DB, cfg Config, log zerolog.Logger) *Handlers {
	return &Handlers{
		db:    db,
		cache: newCache(kv, log),
		cfg:   cfg,
		log:   log,
	}
}

func (h *Handlers) Create(req micro.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var body struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(req.Data(), &body); err != nil {
		_ = req.Error("400", "invalid json", nil)
		return
	}
	if body.Name == "" {
		_ = req.Error("400", "name required", nil)
		return
	}

	var it Item
	err := h.db.QueryRowContext(ctx, `
		INSERT INTO xhttp (name, value)
		VALUES ($1, $2)
		RETURNING id, name, value, created_at, updated_at`,
		body.Name, body.Value,
	).Scan(&it.ID, &it.Name, &it.Value, &it.CreatedAt, &it.UpdatedAt)
	if err != nil {
		_ = req.Error("500", "db error", nil)
		return
	}

	h.cache.Invalidate(ctx, "list")
	_ = req.RespondJSON(it)
}

func (h *Handlers) Get(req micro.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var body struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(req.Data(), &body); err != nil {
		_ = req.Error("400", "invalid json", nil)
		return
	}

	cacheKey := fmt.Sprintf("item:%d", body.ID)
	if cached := h.cache.Get(ctx, cacheKey); len(cached) > 0 {
		var it Item
		if json.Unmarshal(cached, &it) == nil {
			_ = req.RespondJSON(it)
			return
		}
	}

	var it Item
	err := h.db.QueryRowContext(ctx, `
		SELECT id, name, value, created_at, updated_at
		FROM xhttp WHERE id = $1`, body.ID,
	).Scan(&it.ID, &it.Name, &it.Value, &it.CreatedAt, &it.UpdatedAt)
	if err == sql.ErrNoRows {
		_ = req.Error("404", "not found", nil)
		return
	}
	if err != nil {
		_ = req.Error("500", "db error", nil)
		return
	}

	if encoded, err := json.Marshal(it); err == nil {
		h.cache.Put(ctx, cacheKey, encoded)
	}
	_ = req.RespondJSON(it)
}

func (h *Handlers) List(req micro.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if cached := h.cache.Get(ctx, "list"); len(cached) > 0 {
		var items []Item
		if json.Unmarshal(cached, &items) == nil {
			_ = req.RespondJSON(items)
			return
		}
	}

	rows, err := h.db.QueryContext(ctx, `
		SELECT id, name, value, created_at, updated_at
		FROM xhttp ORDER BY id`)
	if err != nil {
		_ = req.Error("500", "db error", nil)
		return
	}
	defer rows.Close()

	items := make([]Item, 0)
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.ID, &it.Name, &it.Value, &it.CreatedAt, &it.UpdatedAt); err != nil {
			_ = req.Error("500", "db scan error", nil)
			return
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		_ = req.Error("500", "db error", nil)
		return
	}

	if encoded, err := json.Marshal(items); err == nil {
		h.cache.Put(ctx, "list", encoded)
	}
	_ = req.RespondJSON(items)
}

func (h *Handlers) Update(req micro.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var body struct {
		ID    int64  `json:"id"`
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(req.Data(), &body); err != nil {
		_ = req.Error("400", "invalid json", nil)
		return
	}
	if body.Name == "" {
		_ = req.Error("400", "name required", nil)
		return
	}

	var it Item
	err := h.db.QueryRowContext(ctx, `
		UPDATE xhttp SET name = $1, value = $2, updated_at = NOW()
		WHERE id = $3
		RETURNING id, name, value, created_at, updated_at`,
		body.Name, body.Value, body.ID,
	).Scan(&it.ID, &it.Name, &it.Value, &it.CreatedAt, &it.UpdatedAt)
	if err == sql.ErrNoRows {
		_ = req.Error("404", "not found", nil)
		return
	}
	if err != nil {
		_ = req.Error("500", "db error", nil)
		return
	}

	h.cache.Invalidate(ctx, fmt.Sprintf("item:%d", body.ID), "list")
	_ = req.RespondJSON(it)
}

func (h *Handlers) Delete(req micro.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var body struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(req.Data(), &body); err != nil {
		_ = req.Error("400", "invalid json", nil)
		return
	}

	res, err := h.db.ExecContext(ctx, `DELETE FROM xhttp WHERE id = $1`, body.ID)
	if err != nil {
		_ = req.Error("500", "db error", nil)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		_ = req.Error("404", "not found", nil)
		return
	}

	h.cache.Invalidate(ctx, fmt.Sprintf("item:%d", body.ID), "list")
	_ = req.Respond(nil)
}
