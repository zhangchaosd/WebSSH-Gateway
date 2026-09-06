package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("not found")
var ErrReferenced = errors.New("object is still referenced")

type Store struct{ DB *sql.DB }

type User struct {
	ID           int64   `json:"id"`
	Username     string  `json:"username"`
	PasswordHash string  `json:"-"`
	Role         string  `json:"role"`
	Enabled      bool    `json:"enabled"`
	LastLoginAt  *string `json:"last_login_at,omitempty"`
}
type Group struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	ParentID  *int64 `json:"parent_id"`
	SortOrder int    `json:"sort_order"`
}
type Credential struct {
	ID              int64  `json:"id"`
	Name            string `json:"name"`
	Type            string `json:"type"`
	Username        string `json:"username,omitempty"`
	EncryptedSecret string `json:"-"`
	KeyFingerprint  string `json:"key_fingerprint,omitempty"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}
type Host struct {
	ID                 int64  `json:"id"`
	Name               string `json:"name"`
	Hostname           string `json:"hostname"`
	Port               int    `json:"port"`
	Username           string `json:"username"`
	CredentialID       int64  `json:"credential_id"`
	CredentialName     string `json:"credential_name,omitempty"`
	GroupID            *int64 `json:"group_id"`
	GroupName          string `json:"group_name,omitempty"`
	Favorite           bool   `json:"favorite"`
	Tags               string `json:"tags"`
	Notes              string `json:"notes"`
	Encoding           string `json:"encoding"`
	AutoCommand        string `json:"auto_command"`
	HostKeyType        string `json:"host_key_type,omitempty"`
	HostKeyFingerprint string `json:"host_key_fingerprint,omitempty"`
	CreatedAt          string `json:"created_at"`
	UpdatedAt          string `json:"updated_at"`
}
type Audit struct {
	ID         int64           `json:"id"`
	UserID     *int64          `json:"user_id"`
	Username   string          `json:"username,omitempty"`
	Action     string          `json:"action"`
	ObjectType string          `json:"object_type"`
	ObjectID   *int64          `json:"object_id"`
	Metadata   json.RawMessage `json:"metadata"`
	CreatedAt  string          `json:"created_at"`
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{DB: db}
	if _, err = db.Exec(`PRAGMA foreign_keys=ON; PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000; PRAGMA synchronous=NORMAL;`); err != nil {
		db.Close()
		return nil, err
	}
	if err = s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	_ = os.Chmod(path, 0600)
	return s, nil
}

func (s *Store) Close() error { return s.DB.Close() }

func (s *Store) migrate() error {
	const schema = `
CREATE TABLE IF NOT EXISTS schema_migrations(version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
CREATE TABLE IF NOT EXISTS users(id INTEGER PRIMARY KEY, username TEXT NOT NULL UNIQUE COLLATE NOCASE, password_hash TEXT NOT NULL, role TEXT NOT NULL DEFAULT 'admin', enabled INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, last_login_at TEXT);
CREATE TABLE IF NOT EXISTS host_groups(id INTEGER PRIMARY KEY, name TEXT NOT NULL UNIQUE COLLATE NOCASE, parent_id INTEGER REFERENCES host_groups(id) ON DELETE SET NULL, sort_order INTEGER NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS credentials(id INTEGER PRIMARY KEY, name TEXT NOT NULL UNIQUE COLLATE NOCASE, type TEXT NOT NULL CHECK(type IN ('password','key')), username TEXT NOT NULL DEFAULT '', encrypted_secret TEXT NOT NULL, key_fingerprint TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
CREATE TABLE IF NOT EXISTS hosts(id INTEGER PRIMARY KEY, name TEXT NOT NULL, hostname TEXT NOT NULL, port INTEGER NOT NULL DEFAULT 22 CHECK(port BETWEEN 1 AND 65535), username TEXT NOT NULL, credential_id INTEGER NOT NULL REFERENCES credentials(id) ON DELETE RESTRICT, group_id INTEGER REFERENCES host_groups(id) ON DELETE SET NULL, favorite INTEGER NOT NULL DEFAULT 0, tags TEXT NOT NULL DEFAULT '', notes TEXT NOT NULL DEFAULT '', encoding TEXT NOT NULL DEFAULT 'UTF-8' CHECK(encoding IN ('UTF-8','GB18030')), auto_command TEXT NOT NULL DEFAULT '', host_key_type TEXT NOT NULL DEFAULT '', host_key_fingerprint TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
CREATE INDEX IF NOT EXISTS idx_hosts_name ON hosts(name); CREATE INDEX IF NOT EXISTS idx_hosts_group ON hosts(group_id);
CREATE TABLE IF NOT EXISTS sessions(id INTEGER PRIMARY KEY, user_id INTEGER NOT NULL REFERENCES users(id), host_id INTEGER REFERENCES hosts(id) ON DELETE SET NULL, started_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, ended_at TEXT, result TEXT NOT NULL DEFAULT 'active', client_ip TEXT NOT NULL DEFAULT '', user_agent TEXT NOT NULL DEFAULT '');
CREATE TABLE IF NOT EXISTS audit_events(id INTEGER PRIMARY KEY, user_id INTEGER REFERENCES users(id) ON DELETE SET NULL, action TEXT NOT NULL, object_type TEXT NOT NULL, object_id INTEGER, metadata_json TEXT NOT NULL DEFAULT '{}', created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
CREATE INDEX IF NOT EXISTS idx_audit_created ON audit_events(created_at DESC);
CREATE TABLE IF NOT EXISTS settings(key TEXT PRIMARY KEY, value_json TEXT NOT NULL, updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
INSERT OR IGNORE INTO schema_migrations(version) VALUES(1);
INSERT OR IGNORE INTO settings(key,value_json) VALUES('terminal','{"scrollback":8000,"font_size":14}'),('ssh','{"connect_timeout":"10s","keepalive_interval":"30s"}'),('session','{"idle_timeout":"8h","close_confirm":true}');`
	_, err := s.DB.Exec(schema)
	return err
}

func (s *Store) EnsureAdmin(username, hash string) (bool, error) {
	var n int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return false, err
	}
	if n > 0 {
		return false, nil
	}
	_, err := s.DB.Exec(`INSERT INTO users(username,password_hash,role) VALUES(?,?,'admin')`, username, hash)
	return err == nil, err
}
func (s *Store) UserCount() (int, error) {
	var n int
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}
func (s *Store) UserByUsername(username string) (User, error) {
	var u User
	var enabled int
	err := s.DB.QueryRow(`SELECT id,username,password_hash,role,enabled,last_login_at FROM users WHERE username=?`, username).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &enabled, &u.LastLoginAt)
	u.Enabled = enabled == 1
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return u, err
}
func (s *Store) UserByID(id int64) (User, error) {
	var u User
	var enabled int
	err := s.DB.QueryRow(`SELECT id,username,password_hash,role,enabled,last_login_at FROM users WHERE id=?`, id).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &enabled, &u.LastLoginAt)
	u.Enabled = enabled == 1
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return u, err
}
func (s *Store) TouchLogin(id int64) {
	_, _ = s.DB.Exec(`UPDATE users SET last_login_at=CURRENT_TIMESTAMP WHERE id=?`, id)
}
func (s *Store) UpdatePassword(id int64, hash string) error {
	_, err := s.DB.Exec(`UPDATE users SET password_hash=? WHERE id=?`, hash, id)
	return err
}

func (s *Store) ListGroups() ([]Group, error) {
	rows, e := s.DB.Query(`SELECT id,name,parent_id,sort_order FROM host_groups ORDER BY sort_order,name COLLATE NOCASE`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Group{}
	for rows.Next() {
		var g Group
		if e = rows.Scan(&g.ID, &g.Name, &g.ParentID, &g.SortOrder); e != nil {
			return nil, e
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
func (s *Store) CreateGroup(g Group) (Group, error) {
	r, e := s.DB.Exec(`INSERT INTO host_groups(name,parent_id,sort_order) VALUES(?,?,?)`, strings.TrimSpace(g.Name), g.ParentID, g.SortOrder)
	if e != nil {
		return g, e
	}
	g.ID, _ = r.LastInsertId()
	return g, nil
}
func (s *Store) DeleteGroup(id int64) error {
	r, e := s.DB.Exec(`DELETE FROM host_groups WHERE id=?`, id)
	if e != nil {
		return e
	}
	n, _ := r.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListCredentials() ([]Credential, error) {
	rows, e := s.DB.Query(`SELECT id,name,type,username,key_fingerprint,created_at,updated_at FROM credentials ORDER BY name COLLATE NOCASE`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Credential{}
	for rows.Next() {
		var c Credential
		if e = rows.Scan(&c.ID, &c.Name, &c.Type, &c.Username, &c.KeyFingerprint, &c.CreatedAt, &c.UpdatedAt); e != nil {
			return nil, e
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
func (s *Store) CredentialByID(id int64) (Credential, error) {
	var c Credential
	e := s.DB.QueryRow(`SELECT id,name,type,username,encrypted_secret,key_fingerprint,created_at,updated_at FROM credentials WHERE id=?`, id).Scan(&c.ID, &c.Name, &c.Type, &c.Username, &c.EncryptedSecret, &c.KeyFingerprint, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(e, sql.ErrNoRows) {
		e = ErrNotFound
	}
	return c, e
}
func (s *Store) CreateCredential(c Credential) (Credential, error) {
	r, e := s.DB.Exec(`INSERT INTO credentials(name,type,username,encrypted_secret,key_fingerprint) VALUES(?,?,?,?,?)`, strings.TrimSpace(c.Name), c.Type, c.Username, c.EncryptedSecret, c.KeyFingerprint)
	if e != nil {
		return c, e
	}
	c.ID, _ = r.LastInsertId()
	return s.CredentialByID(c.ID)
}
func (s *Store) UpdateCredential(c Credential) error {
	r, e := s.DB.Exec(`UPDATE credentials SET name=?,type=?,username=?,encrypted_secret=?,key_fingerprint=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, strings.TrimSpace(c.Name), c.Type, c.Username, c.EncryptedSecret, c.KeyFingerprint, c.ID)
	if e != nil {
		return e
	}
	n, _ := r.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
func (s *Store) DeleteCredential(id int64) error {
	var n int
	if e := s.DB.QueryRow(`SELECT COUNT(*) FROM hosts WHERE credential_id=?`, id).Scan(&n); e != nil {
		return e
	}
	if n > 0 {
		return ErrReferenced
	}
	r, e := s.DB.Exec(`DELETE FROM credentials WHERE id=?`, id)
	if e != nil {
		return e
	}
	x, _ := r.RowsAffected()
	if x == 0 {
		return ErrNotFound
	}
	return nil
}

const hostSelect = `SELECT h.id,h.name,h.hostname,h.port,h.username,h.credential_id,c.name, h.group_id,COALESCE(g.name,''),h.favorite,h.tags,h.notes,h.encoding,h.auto_command,h.host_key_type,h.host_key_fingerprint,h.created_at,h.updated_at FROM hosts h JOIN credentials c ON c.id=h.credential_id LEFT JOIN host_groups g ON g.id=h.group_id`

func scanHost(row interface{ Scan(...any) error }) (Host, error) {
	var h Host
	var fav int
	e := row.Scan(&h.ID, &h.Name, &h.Hostname, &h.Port, &h.Username, &h.CredentialID, &h.CredentialName, &h.GroupID, &h.GroupName, &fav, &h.Tags, &h.Notes, &h.Encoding, &h.AutoCommand, &h.HostKeyType, &h.HostKeyFingerprint, &h.CreatedAt, &h.UpdatedAt)
	h.Favorite = fav == 1
	return h, e
}
func (s *Store) ListHosts(q, sort string) ([]Host, error) {
	order := "h.favorite DESC,h.name COLLATE NOCASE"
	if sort == "recent" {
		order = "h.updated_at DESC"
	} else if sort == "address" {
		order = "h.hostname COLLATE NOCASE,h.port"
	}
	args := []any{}
	where := ""
	if q = strings.TrimSpace(q); q != "" {
		where = ` WHERE h.name LIKE ? ESCAPE '\' OR h.hostname LIKE ? ESCAPE '\' OR h.tags LIKE ? ESCAPE '\'`
		q = "%" + strings.NewReplacer("%", "\\%", "_", "\\_").Replace(q) + "%"
		args = []any{q, q, q}
	}
	rows, e := s.DB.Query(hostSelect+where+" ORDER BY "+order, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Host{}
	for rows.Next() {
		h, e := scanHost(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
func (s *Store) HostByID(id int64) (Host, error) {
	h, e := scanHost(s.DB.QueryRow(hostSelect+` WHERE h.id=?`, id))
	if errors.Is(e, sql.ErrNoRows) {
		e = ErrNotFound
	}
	return h, e
}
func (s *Store) CreateHost(h Host) (Host, error) {
	r, e := s.DB.Exec(`INSERT INTO hosts(name,hostname,port,username,credential_id,group_id,favorite,tags,notes,encoding,auto_command) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, h.Name, h.Hostname, h.Port, h.Username, h.CredentialID, h.GroupID, h.Favorite, h.Tags, h.Notes, h.Encoding, h.AutoCommand)
	if e != nil {
		return h, e
	}
	h.ID, _ = r.LastInsertId()
	return s.HostByID(h.ID)
}
func (s *Store) UpdateHost(h Host) error {
	r, e := s.DB.Exec(`UPDATE hosts SET name=?,hostname=?,port=?,username=?,credential_id=?,group_id=?,favorite=?,tags=?,notes=?,encoding=?,auto_command=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, h.Name, h.Hostname, h.Port, h.Username, h.CredentialID, h.GroupID, h.Favorite, h.Tags, h.Notes, h.Encoding, h.AutoCommand, h.ID)
	if e != nil {
		return e
	}
	n, _ := r.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
func (s *Store) DeleteHost(id int64) error {
	r, e := s.DB.Exec(`DELETE FROM hosts WHERE id=?`, id)
	if e != nil {
		return e
	}
	n, _ := r.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
func (s *Store) PinHostKey(id int64, keyType, fingerprint string) error {
	r, e := s.DB.Exec(`UPDATE hosts SET host_key_type=?,host_key_fingerprint=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, keyType, fingerprint, id)
	if e != nil {
		return e
	}
	n, _ := r.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) Audit(userID *int64, action, objectType string, objectID *int64, metadata any) {
	b, _ := json.Marshal(metadata)
	if len(b) > 4096 {
		b = []byte(`{"truncated":true}`)
	}
	_, _ = s.DB.Exec(`INSERT INTO audit_events(user_id,action,object_type,object_id,metadata_json) VALUES(?,?,?,?,?)`, userID, action, objectType, objectID, string(b))
}
func (s *Store) ListAudit(limit int) ([]Audit, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	rows, e := s.DB.Query(`SELECT a.id,a.user_id,COALESCE(u.username,''),a.action,a.object_type,a.object_id,a.metadata_json,a.created_at FROM audit_events a LEFT JOIN users u ON u.id=a.user_id ORDER BY a.id DESC LIMIT ?`, limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Audit{}
	for rows.Next() {
		var a Audit
		var raw string
		if e = rows.Scan(&a.ID, &a.UserID, &a.Username, &a.Action, &a.ObjectType, &a.ObjectID, &raw, &a.CreatedAt); e != nil {
			return nil, e
		}
		a.Metadata = json.RawMessage(raw)
		out = append(out, a)
	}
	return out, rows.Err()
}
func (s *Store) Settings() (map[string]json.RawMessage, error) {
	rows, e := s.DB.Query(`SELECT key,value_json FROM settings ORDER BY key`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := map[string]json.RawMessage{}
	for rows.Next() {
		var k, v string
		if e = rows.Scan(&k, &v); e != nil {
			return nil, e
		}
		out[k] = json.RawMessage(v)
	}
	return out, rows.Err()
}
func (s *Store) SetSettings(values map[string]json.RawMessage) error {
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for k, v := range values {
		if k != "terminal" && k != "ssh" && k != "session" {
			return fmt.Errorf("unknown settings group %q", k)
		}
		if !json.Valid(v) {
			return fmt.Errorf("invalid JSON for %s", k)
		}
		if _, e = tx.Exec(`INSERT INTO settings(key,value_json,updated_at) VALUES(?,?,CURRENT_TIMESTAMP) ON CONFLICT(key) DO UPDATE SET value_json=excluded.value_json,updated_at=CURRENT_TIMESTAMP`, k, string(v)); e != nil {
			return e
		}
	}
	return tx.Commit()
}
func (s *Store) StartSession(userID, hostID int64, ip, ua string) (int64, error) {
	r, e := s.DB.Exec(`INSERT INTO sessions(user_id,host_id,client_ip,user_agent) VALUES(?,?,?,?)`, userID, hostID, ip, ua)
	if e != nil {
		return 0, e
	}
	return r.LastInsertId()
}
func (s *Store) EndSession(id int64, result string) {
	_, _ = s.DB.Exec(`UPDATE sessions SET ended_at=CURRENT_TIMESTAMP,result=? WHERE id=?`, result, id)
}
func (s *Store) Ready(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return s.DB.PingContext(ctx)
}
