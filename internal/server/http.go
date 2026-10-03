package server

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	khttp "github.com/go-kratos/kratos/v3/transport/http"
	"io"
	"io/fs"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"texas-holdem/internal/service"
	"time"
)

// Address 是默认监听地址，保留为兼容常量；实际地址由 Options 决定。
const Address = "127.0.0.1:8081"
const (
	DefaultHost    = "127.0.0.1"
	DefaultPort    = 8081
	defaultTimeout = 10 * time.Second
)
const maxBody = 16 << 10
const maxHeaders = 8 << 10

// Options 描述监听地址与超时。零值等价于历史默认 127.0.0.1:8081 / 10s。
type Options struct {
	Host    string
	Port    int
	Timeout time.Duration
}

func (o Options) normalized() Options {
	if o.Host == "" {
		o.Host = DefaultHost
	}
	if o.Port == 0 {
		o.Port = DefaultPort
	}
	if o.Timeout <= 0 {
		o.Timeout = defaultTimeout
	}
	return o
}

func (o Options) address() string { return net.JoinHostPort(o.Host, strconv.Itoa(o.Port)) }

func optionsFrom(opts []Options) Options {
	if len(opts) == 0 {
		return Options{}.normalized()
	}
	return opts[0].normalized()
}

// allowedHosts 只放行配置端口；仅当配置为 loopback 时额外放行 127.0.0.1 / localhost 别名。
func allowedHosts(o Options) map[string]bool {
	port := strconv.Itoa(o.Port)
	hosts := map[string]bool{net.JoinHostPort(o.Host, port): true}
	if isLoopbackHost(o.Host) {
		hosts[net.JoinHostPort("127.0.0.1", port)] = true
		hosts[net.JoinHostPort("localhost", port)] = true
	}
	return hosts
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (o Options) loopback() bool { return isLoopbackHost(o.Host) }

const (
	playerCookieName = "poker_player"
	sessionBytes     = 32
)

func hasHumanSeat(c service.CreateGame) bool {
	for _, s := range c.Seats {
		if s.Kind == service.SeatHuman {
			return true
		}
	}
	return false
}

// newPlayerSession 生成 32 字节密码学随机凭据及其 SHA-256 摘要。
// 原始凭据只写入 HttpOnly Cookie；服务端仅保存摘要。
func newPlayerSession() (string, service.PlayerSessionHash, bool) {
	b := make([]byte, sessionBytes)
	if _, err := rand.Read(b); err != nil {
		return "", service.PlayerSessionHash{}, false
	}
	return hex.EncodeToString(b), service.PlayerSessionHash(sha256.Sum256(b)), true
}

func setPlayerCookie(w http.ResponseWriter, id, cred string) {
	http.SetCookie(w, &http.Cookie{
		Name:     playerCookieName,
		Value:    cred,
		Path:     "/api/v1/games/" + id,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}

// requestPlayerSession 读取唯一同名 Cookie，解码后取 SHA-256 摘要。
// 重复 Cookie、缺失、格式错误都返回 false，交由上层映射 403。
func requestPlayerSession(r *http.Request) (service.PlayerSessionHash, bool) {
	var found []string
	for _, c := range r.Cookies() {
		if c.Name == playerCookieName {
			found = append(found, c.Value)
		}
	}
	if len(found) != 1 {
		return service.PlayerSessionHash{}, false
	}
	b, err := hex.DecodeString(found[0])
	if err != nil || len(b) != sessionBytes {
		return service.PlayerSessionHash{}, false
	}
	return service.PlayerSessionHash(sha256.Sum256(b)), true
}

func New(m service.Manager, assets fs.FS, opts ...Options) *khttp.Server {
	o := optionsFrom(opts)
	s := khttp.NewServer(khttp.Address(o.address()), khttp.Timeout(o.Timeout))
	// 同步到嵌入的 http.Server.Addr，便于断言监听地址；Kratos 实际用 Options 地址绑定。
	s.Server.Addr = o.address()
	s.Server.ReadHeaderTimeout = 3 * time.Second
	s.Server.ReadTimeout = o.Timeout
	s.Server.WriteTimeout = o.Timeout
	s.Server.IdleTimeout = 30 * time.Second
	s.Server.MaxHeaderBytes = maxHeaders
	s.HandlePrefix("/", Handler(m, assets, opts...))
	return s
}
func Handler(m service.Manager, assets fs.FS, opts ...Options) http.Handler {
	o := optionsFrom(opts)
	allowed := allowedHosts(o)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		v, err := m.Health(r.Context())
		code := 200
		if err != nil {
			code = 503
		}
		write(w, code, Health(v))
	})
	mux.HandleFunc("GET /api/v1/models", func(w http.ResponseWriter, r *http.Request) {
		v, err := m.Models(r.Context())
		if err != nil {
			fail(w, err)
			return
		}
		write(w, 200, Models(v))
	})
	mux.HandleFunc("POST /api/v1/games", func(w http.ResponseWriter, r *http.Request) {
		var d CreateGameDTO
		if err := decode(w, r, &d); err != nil {
			fail(w, service.ErrInvalid)
			return
		}
		c, err := d.Input()
		if err != nil {
			fail(w, err)
			return
		}
		cred := ""
		if hasHumanSeat(c) {
			// 真人功能仅允许 loopback 监听；网络部署需另行设计 HTTPS 与权限体系。
			if !o.loopback() {
				fail(w, service.ErrHumanLocalOnly)
				return
			}
			var hash service.PlayerSessionHash
			var ok bool
			cred, hash, ok = newPlayerSession()
			if !ok {
				fail(w, service.ErrStorage)
				return
			}
			c.PlayerSessionHash = &hash
		}
		v, err := m.Create(r.Context(), c)
		if err != nil {
			fail(w, err)
			return
		}
		if cred != "" {
			setPlayerCookie(w, v.ID, cred)
		}
		write(w, 201, Game(v))
	})
	mux.HandleFunc("GET /api/v1/games/current", func(w http.ResponseWriter, r *http.Request) {
		v, err := m.Current(r.Context())
		if err != nil {
			fail(w, err)
			return
		}
		write(w, 200, Game(v))
	})
	mux.HandleFunc("GET /api/v1/games/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !validID(r.PathValue("id")) {
			fail(w, service.ErrInvalid)
			return
		}
		v, err := m.Get(r.Context(), r.PathValue("id"))
		if err != nil {
			fail(w, err)
			return
		}
		write(w, 200, Game(v))
	})
	mux.HandleFunc("GET /api/v1/games/{id}/player", func(w http.ResponseWriter, r *http.Request) {
		if !validID(r.PathValue("id")) {
			fail(w, service.ErrInvalid)
			return
		}
		if !o.loopback() {
			fail(w, service.ErrHumanLocalOnly)
			return
		}
		hash, ok := requestPlayerSession(r)
		if !ok {
			fail(w, service.ErrPlayerUnauthorized)
			return
		}
		v, err := m.GetPlayer(r.Context(), r.PathValue("id"), hash)
		if err != nil {
			fail(w, err)
			return
		}
		write(w, 200, PlayerGame(v))
	})
	mux.HandleFunc("POST /api/v1/games/{id}/action", func(w http.ResponseWriter, r *http.Request) {
		if !validID(r.PathValue("id")) {
			fail(w, service.ErrInvalid)
			return
		}
		if !o.loopback() {
			fail(w, service.ErrHumanLocalOnly)
			return
		}
		hash, ok := requestPlayerSession(r)
		if !ok {
			fail(w, service.ErrPlayerUnauthorized)
			return
		}
		var d HumanActionDTO
		if err := decode(w, r, &d); err != nil {
			fail(w, service.ErrInvalid)
			return
		}
		ha, err := d.HumanInput()
		if err != nil {
			fail(w, err)
			return
		}
		v, err := m.Act(r.Context(), r.PathValue("id"), hash, ha)
		if err != nil {
			fail(w, err)
			return
		}
		write(w, 200, PlayerGame(v))
	})
	mux.HandleFunc("POST /api/v1/games/{id}/commands", func(w http.ResponseWriter, r *http.Request) {
		if !validID(r.PathValue("id")) {
			fail(w, service.ErrInvalid)
			return
		}
		var d CommandDTO
		if err := decode(w, r, &d); err != nil {
			fail(w, service.ErrInvalid)
			return
		}
		if d.Command != "start" && d.Command != "pause" && d.Command != "resume" && d.Command != "stop" {
			fail(w, service.ErrInvalid)
			return
		}
		v, err := m.Control(r.Context(), r.PathValue("id"), d.Command)
		if err != nil {
			fail(w, err)
			return
		}
		write(w, 200, Game(v))
	})
	mux.HandleFunc("GET /api/v1/games/{id}/hands/{hand_no}", func(w http.ResponseWriter, r *http.Request) {
		n, err := strconv.ParseUint(r.PathValue("hand_no"), 10, 32)
		if err != nil || n == 0 || !validID(r.PathValue("id")) {
			fail(w, service.ErrInvalid)
			return
		}
		v, err := m.GetHand(r.Context(), r.PathValue("id"), uint32(n))
		if err != nil {
			fail(w, err)
			return
		}
		write(w, 200, Completed(v))
	})
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { fail(w, service.ErrNotFound) })
	mux.Handle("/", static(assets))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if !allowed[r.Host] {
			write(w, 403, ErrorDTO{Error: ErrorDetail{Code: "INVALID_ORIGIN", Message: "请求来源不允许"}})
			return
		}
		size := len(r.Host) + len(r.RequestURI)
		for k, vs := range r.Header {
			size += len(k)
			for _, v := range vs {
				size += len(v)
			}
		}
		if size > maxHeaders {
			write(w, 431, ErrorDTO{Error: ErrorDetail{Code: "HEADERS_TOO_LARGE", Message: "请求头过大"}})
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			origin := r.Header.Values("Origin")
			if len(origin) > 1 || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				forbiddenOrigin(w)
				return
			}
			if len(origin) == 1 {
				u, err := url.Parse(origin[0])
				if err != nil || u.Scheme != "http" || u.Host != r.Host || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
					forbiddenOrigin(w)
					return
				}
			}
		}
		mux.ServeHTTP(w, r)
	})
}
func forbiddenOrigin(w http.ResponseWriter) {
	write(w, 403, ErrorDTO{Error: ErrorDetail{Code: "INVALID_ORIGIN", Message: "请求来源不允许"}})
}
func validID(id string) bool {
	if len(id) != 36 {
		return false
	}
	for i, c := range id {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func write(w http.ResponseWriter, status int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		status = 500
		b = []byte(`{"error":{"code":"INTERNAL_ERROR","message":"服务内部错误"}}`)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if _, err = w.Write(append(b, '\n')); err != nil {
		log.Print("HTTP_RESPONSE_WRITE_FAILED")
		return
	}
}
func fail(w http.ResponseWriter, err error) {
	status, code, message := 500, "INTERNAL_ERROR", "服务内部错误"
	switch {
	case errors.Is(err, service.ErrInvalid):
		status, code, message = 400, "INVALID_INPUT", "请求参数不合法"
	case errors.Is(err, service.ErrForbidden):
		status, code, message = 403, "LIVE_NOT_AUTHORIZED", "服务未授权付费模型"
	case errors.Is(err, service.ErrNotFound):
		status, code, message = 404, "NOT_FOUND", "资源不存在"
	case errors.Is(err, service.ErrConflict):
		status, code, message = 409, "INVALID_STATE", "当前状态不允许该操作"
	case errors.Is(err, service.ErrStorage):
		status, code, message = 503, "STORAGE_UNAVAILABLE", "存储不可用"
	case errors.Is(err, service.ErrPlayerUnauthorized):
		status, code, message = 403, "PLAYER_NOT_AUTHORIZED", "真人会话无效"
	case errors.Is(err, service.ErrHumanLocalOnly):
		status, code, message = 403, "HUMAN_LOCAL_ONLY", "真人功能仅限本机"
	case errors.Is(err, service.ErrIllegalAction):
		status, code, message = 400, "ILLEGAL_ACTION", "当前局面不允许该动作"
	case errors.Is(err, service.ErrStaleAction):
		status, code, message = 409, "STALE_ACTION", "动作已过期"
	case errors.Is(err, service.ErrActionExpired):
		status, code, message = 409, "ACTION_EXPIRED", "等待已超时"
	}
	write(w, status, ErrorDTO{Error: ErrorDetail{Code: code, Message: message}})
}
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		return service.ErrInvalid
	}
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		return service.ErrInvalid
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(b, &fields); err != nil || fields == nil {
		return service.ErrInvalid
	}
	if raw, ok := fields["mode"]; ok && bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return service.ErrInvalid
	}
	// 先按 token 校验重复键和嵌套深度，再做严格 DTO 解码。
	d := json.NewDecoder(bytes.NewReader(b))
	if err = uniqueValue(d, 0); err != nil {
		return service.ErrInvalid
	}
	if _, err = d.Token(); err != io.EOF {
		return service.ErrInvalid
	}
	d = json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err = d.Decode(v); err != nil {
		return service.ErrInvalid
	}
	return nil
}
func uniqueValue(d *json.Decoder, depth int) error {
	if depth > 16 {
		return service.ErrInvalid
	}
	t, err := d.Token()
	if err != nil {
		return err
	}
	delim, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		keys := map[string]bool{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return err
			}
			s, ok := key.(string)
			if !ok || keys[s] {
				return service.ErrInvalid
			}
			keys[s] = true
			if err = uniqueValue(d, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for d.More() {
			if err = uniqueValue(d, depth+1); err != nil {
				return err
			}
		}
	default:
		return service.ErrInvalid
	}
	_, err = d.Token()
	return err
}
func static(assets fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if assets == nil {
			fail(w, service.ErrNotFound)
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			w.WriteHeader(405)
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		if !fs.ValidPath(path) {
			fail(w, service.ErrNotFound)
			return
		}
		b, err := fs.ReadFile(assets, path)
		if errors.Is(err, fs.ErrNotExist) && !strings.Contains(path, ".") {
			path = "index.html"
			b, err = fs.ReadFile(assets, path)
		}
		if err != nil {
			fail(w, service.ErrNotFound)
			return
		}
		contentType := "application/octet-stream"
		if i := strings.LastIndex(path, "."); i >= 0 {
			if value := mime.TypeByExtension(path[i:]); value != "" {
				contentType = value
			}
		}
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(200)
		if r.Method != "HEAD" {
			if _, err = w.Write(b); err != nil {
				log.Print("HTTP_RESPONSE_WRITE_FAILED")
				return
			}
		}
	})
}
