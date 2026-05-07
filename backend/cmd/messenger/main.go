package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
)

type config struct {
	mode         string
	host         string
	port         string
	appSecret    string
	postgresURL  string
	redisURL     string
	natsURL      string
	natsStream   string
	natsSubject  string
	readinessMax time.Duration
}

type message struct {
	ID        int64     `json:"id"`
	Author    string    `json:"author"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
}

type server struct {
	cfg      config
	db       *pgxpool.Pool
	redis    *redis.Client
	natsConn *nats.Conn
	js       nats.JetStreamContext
	upgrader websocket.Upgrader
}

func main() {
	healthcheck := flag.Bool("healthcheck", false, "run one local health check and exit")
	flag.Parse()

	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	if *healthcheck {
		runHealthcheck(cfg)
		return
	}

	ctx := context.Background()
	app, err := connectWithRetries(ctx, cfg)
	if err != nil {
		log.Fatalf("startup failed: %v", err)
	}
	defer app.close()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", app.handleHealth)
	mux.HandleFunc("/api/messages", app.handleMessages)
	mux.HandleFunc("/ws", app.handleWebSocket)

	addr := cfg.host + ":" + cfg.port
	log.Printf("messenger backend listening on %s", addr)
	if err := http.ListenAndServe(addr, withRequestLog(mux)); err != nil {
		log.Fatal(err)
	}
}

func loadConfig() (config, error) {
	mode := getenv("APP_MODE", "production")
	secret := os.Getenv("APP_SECRET")
	if mode == "production" && (len(secret) < 16 || strings.Contains(secret, "change-me")) {
		return config{}, errors.New("APP_SECRET must be configured as a production secret")
	}

	pgHost := getenv("POSTGRES_HOST", "postgres")
	pgPort := getenv("POSTGRES_PORT", "5432")
	pgDB := getenv("POSTGRES_DB", "messenger")
	pgUser := getenv("POSTGRES_USER", "messenger")
	pgPassword := os.Getenv("POSTGRES_PASSWORD")
	if pgPassword == "" {
		return config{}, errors.New("POSTGRES_PASSWORD must be configured as a runtime secret")
	}

	return config{
		mode:         mode,
		host:         getenv("APP_HOST", "0.0.0.0"),
		port:         getenv("APP_PORT", "8081"),
		appSecret:    secret,
		postgresURL:  fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", pgUser, pgPassword, pgHost, pgPort, pgDB),
		redisURL:     getenv("REDIS_URL", "redis://redis:6379/0"),
		natsURL:      getenv("NATS_URL", "nats://nats:4222"),
		natsStream:   getenv("NATS_STREAM", "MESSAGES"),
		natsSubject:  getenv("NATS_SUBJECT", "messages.created"),
		readinessMax: 60 * time.Second,
	}, nil
}

func connectWithRetries(ctx context.Context, cfg config) (*server, error) {
	deadline := time.Now().Add(cfg.readinessMax)
	var lastErr error
	for time.Now().Before(deadline) {
		app, err := connectOnce(ctx, cfg)
		if err == nil {
			return app, nil
		}
		lastErr = err
		log.Printf("waiting for dependencies: %v", err)
		time.Sleep(2 * time.Second)
	}
	return nil, lastErr
}

func connectOnce(ctx context.Context, cfg config) (*server, error) {
	db, err := pgxpool.New(ctx, cfg.postgresURL)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if err := migrate(ctx, db); err != nil {
		db.Close()
		return nil, err
	}

	redisOpts, err := redis.ParseURL(cfg.redisURL)
	if err != nil {
		db.Close()
		return nil, err
	}
	redisClient := redis.NewClient(redisOpts)
	if err := redisClient.Ping(ctx).Err(); err != nil {
		db.Close()
		_ = redisClient.Close()
		return nil, err
	}

	nc, err := nats.Connect(cfg.natsURL, nats.Name("tlfpaas messenger backend"))
	if err != nil {
		db.Close()
		_ = redisClient.Close()
		return nil, err
	}
	js, err := nc.JetStream()
	if err != nil {
		db.Close()
		_ = redisClient.Close()
		nc.Close()
		return nil, err
	}
	if err := ensureStream(js, cfg); err != nil {
		db.Close()
		_ = redisClient.Close()
		nc.Close()
		return nil, err
	}

	return &server{
		cfg:      cfg,
		db:       db,
		redis:    redisClient,
		natsConn: nc,
		js:       js,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(_ *http.Request) bool {
				return true
			},
		},
	}, nil
}

func migrate(ctx context.Context, db *pgxpool.Pool) error {
	_, err := db.Exec(ctx, `
CREATE TABLE IF NOT EXISTS messages (
	id BIGSERIAL PRIMARY KEY,
	author TEXT NOT NULL,
	body TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS messages_created_at_idx ON messages(created_at DESC);
`)
	return err
}

func ensureStream(js nats.JetStreamContext, cfg config) error {
	_, err := js.StreamInfo(cfg.natsStream)
	if err == nil {
		return nil
	}
	_, err = js.AddStream(&nats.StreamConfig{
		Name:     cfg.natsStream,
		Subjects: []string{cfg.natsSubject},
		Storage:  nats.FileStorage,
	})
	return err
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	status := map[string]string{"status": "ok"}
	if err := s.db.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "error", "dependency": "postgres"})
		return
	}
	if err := s.redis.Ping(ctx).Err(); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "error", "dependency": "redis"})
		return
	}
	if !s.natsConn.IsConnected() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "error", "dependency": "nats"})
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *server) handleMessages(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listMessages(w, r)
	case http.MethodPost:
		s.createMessage(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (s *server) listMessages(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	rows, err := s.db.Query(ctx, `
SELECT id, author, body, created_at
FROM messages
ORDER BY id DESC
LIMIT 50`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "query failed"})
		return
	}
	defer rows.Close()

	items := make([]message, 0)
	for rows.Next() {
		var item message
		if err := rows.Scan(&item.ID, &item.Author, &item.Text, &item.CreatedAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "scan failed"})
			return
		}
		items = append(items, item)
	}
	reverse(items)
	writeJSON(w, http.StatusOK, map[string]any{"messages": items})
}

func (s *server) createMessage(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Author string `json:"author"`
		Text   string `json:"text"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	input.Author = strings.TrimSpace(input.Author)
	input.Text = strings.TrimSpace(input.Text)
	if input.Author == "" {
		input.Author = "Anonymous"
	}
	if len(input.Author) > 80 || input.Text == "" || len(input.Text) > 1000 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "author or text is invalid"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var item message
	err := s.db.QueryRow(ctx, `
INSERT INTO messages(author, body)
VALUES ($1, $2)
RETURNING id, author, body, created_at`, input.Author, input.Text).Scan(&item.ID, &item.Author, &item.Text, &item.CreatedAt)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "insert failed"})
		return
	}

	payload, _ := json.Marshal(item)
	_ = s.redis.Set(ctx, "latest_message", payload, time.Hour).Err()
	if _, err := s.js.Publish(s.cfg.natsSubject, payload); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "publish failed"})
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	var writeMu sync.Mutex
	sub, err := s.natsConn.Subscribe(s.cfg.natsSubject, func(msg *nats.Msg) {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = conn.WriteMessage(websocket.TextMessage, msg.Data)
	})
	if err != nil {
		_ = conn.WriteJSON(map[string]string{"error": "subscription failed"})
		return
	}
	defer sub.Unsubscribe()

	_ = conn.WriteJSON(map[string]string{"type": "ready"})
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}

func (s *server) close() {
	s.db.Close()
	_ = s.redis.Close()
	s.natsConn.Close()
}

func runHealthcheck(cfg config) {
	url := "http://127.0.0.1:" + cfg.port + "/api/health"
	client := http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Fatalf("healthcheck failed: %s", resp.Status)
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func withRequestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
	})
}

func getenv(key string, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func reverse(items []message) {
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
}
