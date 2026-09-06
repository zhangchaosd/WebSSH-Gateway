package httpapi

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
	"webssh/internal/auth"
	"webssh/internal/config"
	"webssh/internal/security"
	"webssh/internal/sshgw"
	"webssh/internal/store"
)

//go:embed dist/*
var assets embed.FS

type contextKey string

const sessionKey contextKey = "session"

type Server struct {
	cfg         config.Config
	store       *store.Store
	cipher      *security.Cipher
	auth        *auth.Manager
	ssh         *sshgw.Gateway
	log         *slog.Logger
	mux         *http.ServeMux
	activeMu    sync.Mutex
	activeTotal int
	activeUsers map[int64]int
}
type apiError struct {
	Error struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		RequestID string `json:"request_id,omitempty"`
		Data      any    `json:"data,omitempty"`
	} `json:"error"`
}
type message struct {
	V         int    `json:"v"`
	Type      string `json:"type"`
	Cols      int    `json:"cols,omitempty"`
	Rows      int    `json:"rows,omitempty"`
	Encoding  string `json:"encoding,omitempty"`
	TS        int64  `json:"ts,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	Code      any    `json:"code,omitempty"`
	Message   string `json:"message,omitempty"`
	Data      any    `json:"data,omitempty"`
}

// streamCodec keeps incremental decoder state so multibyte GB18030 characters
// remain intact even when an SSH read splits them across packets.
type streamCodec struct {
	mu                    sync.Mutex
	name                  string
	encoder, decoder      transform.Transformer
	inPending, outPending []byte
}

func newStreamCodec(name string) *streamCodec { c := &streamCodec{}; c.set(name); return c }
func (c *streamCodec) set(name string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if name != "UTF-8" && name != "GB18030" {
		return false
	}
	c.name, c.inPending, c.outPending = name, nil, nil
	if name == "GB18030" {
		c.encoder = simplifiedchinese.GB18030.NewEncoder()
		c.decoder = simplifiedchinese.GB18030.NewDecoder()
	} else {
		c.encoder, c.decoder = nil, nil
	}
	return true
}
func (c *streamCodec) transcode(src []byte, encode bool) []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	var t transform.Transformer
	var pending *[]byte
	if encode {
		t, pending = c.encoder, &c.inPending
	} else {
		t, pending = c.decoder, &c.outPending
	}
	if t == nil {
		return append([]byte(nil), src...)
	}
	*pending = append(*pending, src...)
	dst := make([]byte, len(*pending)*4+16)
	nDst, nSrc, _ := t.Transform(dst, *pending, false)
	*pending = append((*pending)[:0], (*pending)[nSrc:]...)
	return append([]byte(nil), dst[:nDst]...)
}

func New(cfg config.Config, st *store.Store, cipher *security.Cipher, am *auth.Manager, gw *sshgw.Gateway, logger *slog.Logger) *Server {
	s := &Server{cfg: cfg, store: st, cipher: cipher, auth: am, ssh: gw, log: logger, mux: http.NewServeMux(), activeUsers: map[int64]int{}}
	s.routes()
	return s
}
func (s *Server) Handler() http.Handler { return s.securityHeaders(s.mux) }
func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/v1/healthz", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]any{"status": "ok"}) })
	s.mux.HandleFunc("GET /api/v1/readyz", func(w http.ResponseWriter, r *http.Request) {
		if e := s.store.Ready(r.Context()); e != nil {
			fail(w, 503, "NOT_READY", "database is not ready", nil)
			return
		}
		writeJSON(w, 200, map[string]any{"status": "ready"})
	})
	s.mux.HandleFunc("POST /api/v1/auth/login", s.login)
	s.mux.Handle("POST /api/v1/auth/logout", s.protect(http.HandlerFunc(s.logout)))
	s.mux.Handle("GET /api/v1/me", s.protect(http.HandlerFunc(s.me)))
	s.mux.Handle("PUT /api/v1/me/password", s.protect(http.HandlerFunc(s.passwordUpdate)))
	s.mux.Handle("GET /api/v1/hosts", s.protect(http.HandlerFunc(s.hostsList)))
	s.mux.Handle("POST /api/v1/hosts", s.protect(http.HandlerFunc(s.hostsCreate)))
	s.mux.Handle("GET /api/v1/hosts/{id}", s.protect(http.HandlerFunc(s.hostGet)))
	s.mux.Handle("PUT /api/v1/hosts/{id}", s.protect(http.HandlerFunc(s.hostUpdate)))
	s.mux.Handle("DELETE /api/v1/hosts/{id}", s.protect(http.HandlerFunc(s.hostDelete)))
	s.mux.Handle("POST /api/v1/hosts/{id}/copy", s.protect(http.HandlerFunc(s.hostCopy)))
	s.mux.Handle("POST /api/v1/hosts/{id}/verify-host-key", s.protect(http.HandlerFunc(s.hostVerify)))
	s.mux.Handle("GET /api/v1/credentials", s.protect(http.HandlerFunc(s.credentialsList)))
	s.mux.Handle("POST /api/v1/credentials", s.protect(http.HandlerFunc(s.credentialsCreate)))
	s.mux.Handle("PUT /api/v1/credentials/{id}", s.protect(http.HandlerFunc(s.credentialsUpdate)))
	s.mux.Handle("DELETE /api/v1/credentials/{id}", s.protect(http.HandlerFunc(s.credentialsDelete)))
	s.mux.Handle("GET /api/v1/groups", s.protect(http.HandlerFunc(s.groupsList)))
	s.mux.Handle("POST /api/v1/groups", s.protect(http.HandlerFunc(s.groupsCreate)))
	s.mux.Handle("DELETE /api/v1/groups/{id}", s.protect(http.HandlerFunc(s.groupsDelete)))
	s.mux.Handle("GET /api/v1/audit", s.protect(http.HandlerFunc(s.auditList)))
	s.mux.Handle("GET /api/v1/settings", s.protect(http.HandlerFunc(s.settingsGet)))
	s.mux.Handle("PUT /api/v1/settings", s.protect(http.HandlerFunc(s.settingsPut)))
	s.mux.Handle("GET /api/v1/terminal/{id}", s.protect(http.HandlerFunc(s.terminal)))
	dist, _ := fs.Sub(assets, "dist")
	files := http.FileServer(http.FS(dist))
	s.mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p != "" {
			if _, e := fs.Stat(dist, p); e == nil {
				files.ServeHTTP(w, r)
				return
			}
		}
		r.URL.Path = "/"
		files.ServeHTTP(w, r)
	})
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; connect-src 'self' ws: wss:; img-src 'self' data:; font-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		next.ServeHTTP(w, r)
	})
}
func (s *Server) protect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, ok := s.auth.Get(r)
		if !ok {
			fail(w, 401, "UNAUTHORIZED", "login required", nil)
			return
		}
		if !s.originAllowed(r) {
			fail(w, 403, "ORIGIN_REJECTED", "request origin is not allowed", nil)
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" && r.Header.Get("X-CSRF-Token") != sess.CSRF {
			fail(w, 403, "CSRF_REJECTED", "invalid CSRF token", nil)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionKey, sess)))
	})
}
func (s *Server) originAllowed(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return true
	}
	u, e := url.Parse(o)
	if e != nil {
		return false
	}
	if strings.EqualFold(u.Host, r.Host) {
		return true
	}
	for _, x := range s.cfg.AllowedOrigins {
		if strings.EqualFold(strings.TrimRight(x, "/"), strings.TrimRight(o, "/")) {
			return true
		}
	}
	return false
}
func current(r *http.Request) *auth.Session { return r.Context().Value(sessionKey).(*auth.Session) }
func clientIP(r *http.Request) string {
	h, _, e := net.SplitHostPort(r.RemoteAddr)
	if e == nil {
		return h
	}
	return r.RemoteAddr
}
func idParam(r *http.Request) (int64, error) { return strconv.ParseInt(r.PathValue("id"), 10, 64) }
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, code, msg string, data any) {
	e := apiError{}
	e.Error.Code = code
	e.Error.Message = msg
	e.Error.Data = data
	writeJSON(w, status, e)
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20))
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		fail(w, 400, "INVALID_REQUEST", "请求内容无效", nil)
		return false
	}
	return true
}
func requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	if current(r).User.Role != "admin" {
		fail(w, 403, "FORBIDDEN", "administrator access required", nil)
		return false
	}
	return true
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if !s.originAllowed(r) {
		fail(w, 403, "ORIGIN_REJECTED", "request origin is not allowed", nil)
		return
	}
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	key := clientIP(r) + ":" + strings.ToLower(in.Username)
	if ok, wait := s.auth.AllowLogin(key); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		fail(w, 429, "LOGIN_RATE_LIMITED", "登录尝试过多，请稍后重试", nil)
		return
	}
	u, e := s.store.UserByUsername(in.Username)
	if e != nil || !u.Enabled || !auth.CheckPassword(u.PasswordHash, in.Password) {
		s.auth.FailedLogin(key)
		s.store.Audit(nil, "login_failed", "user", nil, map[string]any{"username": in.Username, "client_ip": clientIP(r)})
		time.Sleep(250 * time.Millisecond)
		fail(w, 401, "INVALID_CREDENTIALS", "用户名或密码错误", nil)
		return
	}
	s.auth.ClearAttempts(key)
	sess := s.auth.Login(w, u)
	s.store.TouchLogin(u.ID)
	s.store.Audit(&u.ID, "login", "user", &u.ID, map[string]any{"client_ip": clientIP(r)})
	writeJSON(w, 200, map[string]any{"user": u, "csrf_token": sess.CSRF})
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	u := current(r).User
	s.auth.Logout(w, r)
	s.store.Audit(&u.ID, "logout", "user", &u.ID, map[string]any{})
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	x := current(r)
	writeJSON(w, 200, map[string]any{"user": x.User, "csrf_token": x.CSRF, "version": "1.0.0"})
}
func (s *Server) passwordUpdate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
	}
	if !decode(w, r, &in) {
		return
	}
	u := current(r).User
	fresh, e := s.store.UserByID(u.ID)
	if e != nil || !auth.CheckPassword(fresh.PasswordHash, in.Current) {
		fail(w, 400, "CURRENT_PASSWORD_INVALID", "当前密码不正确", nil)
		return
	}
	if len(in.New) < 12 {
		fail(w, 400, "PASSWORD_TOO_SHORT", "新密码至少需要 12 个字符", nil)
		return
	}
	hash, e := auth.HashPassword(in.New)
	if e != nil {
		fail(w, 500, "HASH_FAILED", "无法更新密码", nil)
		return
	}
	if e = s.store.UpdatePassword(u.ID, hash); e != nil {
		fail(w, 500, "DB_ERROR", "无法更新密码", nil)
		return
	}
	s.store.Audit(&u.ID, "password.update", "user", &u.ID, map[string]any{})
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func validateHost(h *store.Host) error {
	h.Name = strings.TrimSpace(h.Name)
	h.Hostname = strings.TrimSpace(h.Hostname)
	h.Username = strings.TrimSpace(h.Username)
	if h.Port == 0 {
		h.Port = 22
	}
	if h.Encoding == "" {
		h.Encoding = "UTF-8"
	}
	if h.Name == "" || h.Hostname == "" || h.Username == "" || h.CredentialID == 0 {
		return errors.New("名称、地址、用户名和凭据不能为空")
	}
	if h.Port < 1 || h.Port > 65535 {
		return errors.New("端口必须为 1-65535")
	}
	if h.Encoding != "UTF-8" && h.Encoding != "GB18030" {
		return errors.New("编码仅支持 UTF-8 或 GB18030")
	}
	return nil
}
func (s *Server) hostsList(w http.ResponseWriter, r *http.Request) {
	x, e := s.store.ListHosts(r.URL.Query().Get("q"), r.URL.Query().Get("sort"))
	if e != nil {
		fail(w, 500, "DB_ERROR", "读取主机失败", nil)
		return
	}
	writeJSON(w, 200, map[string]any{"hosts": x})
}
func (s *Server) hostsCreate(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	var h store.Host
	if !decode(w, r, &h) {
		return
	}
	if e := validateHost(&h); e != nil {
		fail(w, 400, "INVALID_HOST", e.Error(), nil)
		return
	}
	h, e := s.store.CreateHost(h)
	if e != nil {
		fail(w, 400, "CREATE_FAILED", "无法创建主机，请检查凭据和分组", nil)
		return
	}
	u := current(r).User
	s.store.Audit(&u.ID, "host.create", "host", &h.ID, map[string]any{"name": h.Name, "hostname": h.Hostname})
	writeJSON(w, 201, h)
}
func (s *Server) hostGet(w http.ResponseWriter, r *http.Request) {
	id, e := idParam(r)
	if e != nil {
		fail(w, 400, "INVALID_ID", "invalid host id", nil)
		return
	}
	h, e := s.store.HostByID(id)
	if e != nil {
		fail(w, 404, "NOT_FOUND", "host not found", nil)
		return
	}
	writeJSON(w, 200, h)
}
func (s *Server) hostUpdate(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	id, e := idParam(r)
	if e != nil {
		fail(w, 400, "INVALID_ID", "invalid host id", nil)
		return
	}
	var h store.Host
	if !decode(w, r, &h) {
		return
	}
	h.ID = id
	if e = validateHost(&h); e != nil {
		fail(w, 400, "INVALID_HOST", e.Error(), nil)
		return
	}
	if e = s.store.UpdateHost(h); e != nil {
		fail(w, 400, "UPDATE_FAILED", "无法更新主机", nil)
		return
	}
	u := current(r).User
	s.store.Audit(&u.ID, "host.update", "host", &id, map[string]any{"name": h.Name})
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (s *Server) hostDelete(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	id, e := idParam(r)
	if e != nil {
		fail(w, 400, "INVALID_ID", "invalid host id", nil)
		return
	}
	if e = s.store.DeleteHost(id); e != nil {
		fail(w, 404, "NOT_FOUND", "host not found", nil)
		return
	}
	u := current(r).User
	s.store.Audit(&u.ID, "host.delete", "host", &id, map[string]any{})
	w.WriteHeader(204)
}
func (s *Server) hostCopy(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	id, e := idParam(r)
	if e != nil {
		fail(w, 400, "INVALID_ID", "invalid host id", nil)
		return
	}
	h, e := s.store.HostByID(id)
	if e != nil {
		fail(w, 404, "NOT_FOUND", "host not found", nil)
		return
	}
	h.ID = 0
	h.Name += " 副本"
	h.HostKeyType = ""
	h.HostKeyFingerprint = ""
	h, e = s.store.CreateHost(h)
	if e != nil {
		fail(w, 400, "COPY_FAILED", "无法复制主机", nil)
		return
	}
	u := current(r).User
	s.store.Audit(&u.ID, "host.copy", "host", &h.ID, map[string]any{"source_id": id})
	writeJSON(w, 201, h)
}
func (s *Server) hostVerify(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	id, e := idParam(r)
	if e != nil {
		fail(w, 400, "INVALID_ID", "invalid host id", nil)
		return
	}
	var in struct {
		Fingerprint string `json:"fingerprint"`
	}
	if !decode(w, r, &in) {
		return
	}
	h, e := s.store.HostByID(id)
	if e != nil {
		fail(w, 404, "NOT_FOUND", "host not found", nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.ConnectTimeout+s.cfg.HandshakeTimeout)
	defer cancel()
	typ, fp, e := s.ssh.ScanHostKey(ctx, h)
	if e != nil {
		fail(w, 502, "SSH_SCAN_FAILED", "无法读取 SSH Host Key", nil)
		return
	}
	if in.Fingerprint == "" || in.Fingerprint != fp {
		fail(w, 409, "FINGERPRINT_CHANGED", "当前指纹与确认值不一致", map[string]any{"key_type": typ, "fingerprint": fp})
		return
	}
	if e = s.store.PinHostKey(id, typ, fp); e != nil {
		fail(w, 500, "DB_ERROR", "保存 Host Key 失败", nil)
		return
	}
	u := current(r).User
	s.store.Audit(&u.ID, "host_key.pin", "host", &id, map[string]any{"key_type": typ, "fingerprint": fp})
	writeJSON(w, 200, map[string]any{"key_type": typ, "fingerprint": fp})
}

type credentialInput struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Username   string `json:"username"`
	Password   string `json:"password"`
	PrivateKey string `json:"private_key"`
	Passphrase string `json:"passphrase"`
}

func (s *Server) makeCredential(in credentialInput, old *store.Credential) (store.Credential, error) {
	c := store.Credential{Name: strings.TrimSpace(in.Name), Type: in.Type, Username: strings.TrimSpace(in.Username)}
	if old != nil {
		c.ID = old.ID
		c.EncryptedSecret = old.EncryptedSecret
		c.KeyFingerprint = old.KeyFingerprint
		if c.Type == "" {
			c.Type = old.Type
		}
		if c.Name == "" {
			c.Name = old.Name
		}
	}
	if old != nil && c.Type != old.Type && in.Password == "" && in.PrivateKey == "" {
		return c, errors.New("更改凭据类型时必须提供新的凭据内容")
	}
	if c.Name == "" || (c.Type != "password" && c.Type != "key") {
		return c, errors.New("名称或类型无效")
	}
	sec := sshgw.Secret{Password: in.Password, PrivateKey: in.PrivateKey, Passphrase: in.Passphrase}
	has := in.Password != "" || in.PrivateKey != "" || in.Passphrase != ""
	if old == nil && !has {
		return c, errors.New("凭据内容不能为空")
	}
	if has {
		if c.Type == "password" && in.Password == "" {
			return c, errors.New("密码不能为空")
		}
		if c.Type == "key" && in.PrivateKey == "" {
			return c, errors.New("私钥不能为空")
		}
		b, _ := json.Marshal(sec)
		enc, e := s.cipher.Encrypt(b)
		if e != nil {
			return c, e
		}
		c.EncryptedSecret = enc
	}
	return c, nil
}
func (s *Server) credentialsList(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	x, e := s.store.ListCredentials()
	if e != nil {
		fail(w, 500, "DB_ERROR", "读取凭据失败", nil)
		return
	}
	writeJSON(w, 200, map[string]any{"credentials": x})
}
func (s *Server) credentialsCreate(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	var in credentialInput
	if !decode(w, r, &in) {
		return
	}
	c, e := s.makeCredential(in, nil)
	if e != nil {
		fail(w, 400, "INVALID_CREDENTIAL", e.Error(), nil)
		return
	}
	c, e = s.store.CreateCredential(c)
	if e != nil {
		fail(w, 400, "CREATE_FAILED", "无法创建凭据，名称可能重复", nil)
		return
	}
	u := current(r).User
	s.store.Audit(&u.ID, "credential.create", "credential", &c.ID, map[string]any{"name": c.Name, "type": c.Type})
	writeJSON(w, 201, c)
}
func (s *Server) credentialsUpdate(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	id, e := idParam(r)
	if e != nil {
		fail(w, 400, "INVALID_ID", "invalid credential id", nil)
		return
	}
	old, e := s.store.CredentialByID(id)
	if e != nil {
		fail(w, 404, "NOT_FOUND", "credential not found", nil)
		return
	}
	var in credentialInput
	if !decode(w, r, &in) {
		return
	}
	c, e := s.makeCredential(in, &old)
	if e != nil {
		fail(w, 400, "INVALID_CREDENTIAL", e.Error(), nil)
		return
	}
	if e = s.store.UpdateCredential(c); e != nil {
		fail(w, 400, "UPDATE_FAILED", "无法更新凭据", nil)
		return
	}
	u := current(r).User
	s.store.Audit(&u.ID, "credential.update", "credential", &id, map[string]any{"name": c.Name, "type": c.Type})
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (s *Server) credentialsDelete(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	id, e := idParam(r)
	if e != nil {
		fail(w, 400, "INVALID_ID", "invalid credential id", nil)
		return
	}
	if e = s.store.DeleteCredential(id); errors.Is(e, store.ErrReferenced) {
		fail(w, 409, "CREDENTIAL_IN_USE", "凭据仍被主机引用", nil)
		return
	} else if e != nil {
		fail(w, 404, "NOT_FOUND", "credential not found", nil)
		return
	}
	u := current(r).User
	s.store.Audit(&u.ID, "credential.delete", "credential", &id, map[string]any{})
	w.WriteHeader(204)
}
func (s *Server) groupsList(w http.ResponseWriter, r *http.Request) {
	x, e := s.store.ListGroups()
	if e != nil {
		fail(w, 500, "DB_ERROR", "读取分组失败", nil)
		return
	}
	writeJSON(w, 200, map[string]any{"groups": x})
}
func (s *Server) groupsCreate(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	var g store.Group
	if !decode(w, r, &g) {
		return
	}
	if strings.TrimSpace(g.Name) == "" {
		fail(w, 400, "INVALID_GROUP", "分组名称不能为空", nil)
		return
	}
	g, e := s.store.CreateGroup(g)
	if e != nil {
		fail(w, 400, "CREATE_FAILED", "无法创建分组，名称可能重复", nil)
		return
	}
	u := current(r).User
	s.store.Audit(&u.ID, "group.create", "group", &g.ID, map[string]any{"name": g.Name})
	writeJSON(w, 201, g)
}
func (s *Server) groupsDelete(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	id, e := idParam(r)
	if e != nil {
		fail(w, 400, "INVALID_ID", "invalid group id", nil)
		return
	}
	if e = s.store.DeleteGroup(id); e != nil {
		fail(w, 404, "NOT_FOUND", "group not found", nil)
		return
	}
	u := current(r).User
	s.store.Audit(&u.ID, "group.delete", "group", &id, map[string]any{})
	w.WriteHeader(204)
}
func (s *Server) auditList(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	n, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	x, e := s.store.ListAudit(n)
	if e != nil {
		fail(w, 500, "DB_ERROR", "读取审计失败", nil)
		return
	}
	writeJSON(w, 200, map[string]any{"events": x})
}
func (s *Server) settingsGet(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	x, e := s.store.Settings()
	if e != nil {
		fail(w, 500, "DB_ERROR", "读取设置失败", nil)
		return
	}
	writeJSON(w, 200, x)
}
func (s *Server) settingsPut(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	var x map[string]json.RawMessage
	if !decode(w, r, &x) {
		return
	}
	for key, raw := range x {
		switch key {
		case "terminal":
			var v struct {
				Scrollback int `json:"scrollback"`
				FontSize   int `json:"font_size"`
			}
			if json.Unmarshal(raw, &v) != nil || (v.Scrollback != 0 && (v.Scrollback < 1000 || v.Scrollback > 100000)) || (v.FontSize != 0 && (v.FontSize < 10 || v.FontSize > 28)) {
				fail(w, 400, "INVALID_SETTINGS", "终端设置超出允许范围", nil)
				return
			}
		case "session":
			var v struct {
				IdleTimeout string `json:"idle_timeout"`
			}
			if json.Unmarshal(raw, &v) != nil {
				fail(w, 400, "INVALID_SETTINGS", "会话设置无效", nil)
				return
			}
			if v.IdleTimeout != "" {
				d, e := time.ParseDuration(v.IdleTimeout)
				if e != nil || d < time.Minute || d > 30*24*time.Hour {
					fail(w, 400, "INVALID_SETTINGS", "会话超时必须在 1 分钟到 30 天之间", nil)
					return
				}
			}
		case "ssh":
			var v map[string]any
			if json.Unmarshal(raw, &v) != nil {
				fail(w, 400, "INVALID_SETTINGS", "SSH 设置无效", nil)
				return
			}
		default:
			fail(w, 400, "INVALID_SETTINGS", "未知设置分组", nil)
			return
		}
	}
	if e := s.store.SetSettings(x); e != nil {
		fail(w, 400, "INVALID_SETTINGS", e.Error(), nil)
		return
	}
	u := current(r).User
	s.store.Audit(&u.ID, "settings.update", "settings", nil, map[string]any{"groups": len(x)})
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) reserve(uid int64) bool {
	s.activeMu.Lock()
	defer s.activeMu.Unlock()
	if s.activeTotal >= s.cfg.MaxSessionsTotal || s.activeUsers[uid] >= s.cfg.MaxSessionsPerUser {
		return false
	}
	s.activeTotal++
	s.activeUsers[uid]++
	return true
}
func (s *Server) release(uid int64) {
	s.activeMu.Lock()
	s.activeTotal--
	s.activeUsers[uid]--
	s.activeMu.Unlock()
}
func classifySSH(err error) (string, string, any) {
	var hk *sshgw.HostKeyError
	if errors.As(err, &hk) {
		return hk.Code, hk.Error(), map[string]any{"key_type": hk.KeyType, "fingerprint": hk.Fingerprint, "expected": hk.Expected}
	}
	m := strings.ToLower(err.Error())
	if strings.Contains(m, "authenticate") || strings.Contains(m, "unable to authenticate") {
		return "SSH_AUTH_FAILED", "SSH 认证失败", nil
	}
	if strings.Contains(m, "timeout") || strings.Contains(m, "deadline") {
		return "SSH_TIMEOUT", "SSH 连接超时", nil
	}
	if strings.Contains(m, "no such host") {
		return "SSH_DNS_FAILED", "无法解析主机名", nil
	}
	return "SSH_CONNECT_FAILED", "SSH 连接失败", nil
}

type outFrame struct {
	typ  websocket.MessageType
	data []byte
	ack  chan struct{}
}

func (s *Server) terminal(w http.ResponseWriter, r *http.Request) {
	x := current(r)
	id, e := idParam(r)
	if e != nil {
		fail(w, 400, "INVALID_ID", "invalid host id", nil)
		return
	}
	h, e := s.store.HostByID(id)
	if e != nil {
		fail(w, 404, "NOT_FOUND", "host not found", nil)
		return
	}
	if !s.reserve(x.User.ID) {
		fail(w, 429, "SESSION_LIMIT", "终端会话数已达到上限", nil)
		return
	}
	defer s.release(x.User.ID)
	c, e := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true, CompressionMode: websocket.CompressionDisabled})
	if e != nil {
		return
	}
	defer c.CloseNow()
	c.SetReadLimit(s.cfg.MaxMessageBytes)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	out := make(chan outFrame, 64)
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		for {
			select {
			case f := <-out:
				wc, cc := context.WithTimeout(ctx, 10*time.Second)
				e := c.Write(wc, f.typ, f.data)
				cc()
				if f.ack != nil {
					close(f.ack)
				}
				if e != nil {
					cancel()
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	sendJSON := func(m message) {
		b, _ := json.Marshal(m)
		select {
		case out <- outFrame{typ: websocket.MessageText, data: b}:
		case <-ctx.Done():
		}
	}
	sendFinal := func(m message) {
		b, _ := json.Marshal(m)
		ack := make(chan struct{})
		select {
		case out <- outFrame{typ: websocket.MessageText, data: b, ack: ack}:
		case <-ctx.Done():
			return
		}
		select {
		case <-ack:
		case <-time.After(2 * time.Second):
		}
	}
	cols, _ := strconv.Atoi(r.URL.Query().Get("cols"))
	rows, _ := strconv.Atoi(r.URL.Query().Get("rows"))
	if cols < 2 || cols > 1000 {
		cols = 100
	}
	if rows < 2 || rows > 500 {
		rows = 30
	}
	cred, e := s.store.CredentialByID(h.CredentialID)
	if e != nil {
		sendFinal(message{V: 1, Type: "error", Code: "CREDENTIAL_MISSING", Message: "凭据不存在"})
		cancel()
		<-writerDone
		return
	}
	conn, e := s.ssh.Dial(ctx, h, cred, cols, rows)
	if e != nil {
		code, msg, data := classifySSH(e)
		sendFinal(message{V: 1, Type: "error", Code: code, Message: msg, Data: data})
		s.store.Audit(&x.User.ID, "connection.failed", "host", &id, map[string]any{"code": code})
		cancel()
		<-writerDone
		return
	}
	defer conn.Close()
	codec := newStreamCodec(h.Encoding)
	sid, e := s.store.StartSession(x.User.ID, id, clientIP(r), r.UserAgent())
	if e != nil {
		sendFinal(message{V: 1, Type: "error", Code: "DB_ERROR", Message: "无法记录会话"})
		cancel()
		<-writerDone
		return
	}
	result := "disconnected"
	defer func() {
		s.store.EndSession(sid, result)
		s.store.Audit(&x.User.ID, "connection.end", "host", &id, map[string]any{"session_id": sid, "result": result})
	}()
	s.store.Audit(&x.User.ID, "connection.start", "host", &id, map[string]any{"session_id": sid})
	sendJSON(message{V: 1, Type: "ready", SessionID: strconv.FormatInt(sid, 10)})
	if h.AutoCommand != "" {
		_, _ = io.WriteString(conn.Stdin, h.AutoCommand+"\n")
	}
	go conn.Keepalive(ctx, s.cfg.KeepaliveInterval)
	readOutput := func(rd io.Reader) {
		buf := make([]byte, 32*1024)
		for {
			n, e := rd.Read(buf)
			if n > 0 {
				b := codec.transcode(buf[:n], false)
				if len(b) == 0 {
					continue
				}
				select {
				case out <- outFrame{typ: websocket.MessageBinary, data: b}:
				default:
					sendJSON(message{V: 1, Type: "error", Code: "SLOW_CLIENT", Message: "客户端读取过慢，会话已关闭"})
					cancel()
					return
				}
			}
			if e != nil {
				return
			}
		}
	}
	go readOutput(conn.Stdout)
	go readOutput(conn.Stderr)
	wait := make(chan error, 1)
	go func() { wait <- conn.Wait() }()
	readDone := make(chan error, 1)
	go func() {
		for {
			typ, b, e := c.Read(ctx)
			if e != nil {
				readDone <- e
				return
			}
			if typ == websocket.MessageBinary {
				b = codec.transcode(b, true)
				if len(b) == 0 {
					continue
				}
				if _, e = conn.Stdin.Write(b); e != nil {
					readDone <- e
					return
				}
				continue
			}
			if typ == websocket.MessageText {
				var m message
				if json.Unmarshal(b, &m) != nil || m.V != 1 {
					continue
				}
				switch m.Type {
				case "resize":
					if m.Cols >= 2 && m.Cols <= 1000 && m.Rows >= 2 && m.Rows <= 500 {
						_ = conn.Resize(m.Cols, m.Rows)
					}
				case "ping":
					sendJSON(message{V: 1, Type: "pong", TS: m.TS})
				case "encoding":
					name := strings.ToUpper(m.Encoding)
					if codec.set(name) {
						sendJSON(message{V: 1, Type: "encoding", Encoding: name})
					}
				}
			}
		}
	}()
	select {
	case e = <-wait:
		result = "exited"
		code := 0
		if e != nil {
			result = "failed"
			code = 1
		}
		sendFinal(message{V: 1, Type: "exit", Code: code})
	case <-readDone:
		result = "browser_disconnected"
	case <-ctx.Done():
		result = "cancelled"
	}
	cancel()
	conn.Close()
	<-writerDone
	_ = c.Close(websocket.StatusNormalClosure, "session closed")
}
