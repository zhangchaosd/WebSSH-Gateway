package sshgw

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"webssh/internal/security"
	"webssh/internal/store"
)

type Settings struct {
	ConnectTimeout, HandshakeTimeout, Keepalive time.Duration
	DefaultTerm                                 string
	Listen                                      string
}
type Gateway struct {
	cipher *security.Cipher
	cfg    Settings
}
type Secret struct {
	Password   string `json:"password,omitempty"`
	PrivateKey string `json:"private_key,omitempty"`
	Passphrase string `json:"passphrase,omitempty"`
}
type HostKeyError struct{ Code, KeyType, Fingerprint, Expected string }

func (e *HostKeyError) Error() string {
	if e.Code == "SSH_HOST_KEY_UNKNOWN" {
		return "host key has not been trusted"
	}
	return "host key does not match the pinned fingerprint"
}

type Connection struct {
	Client  *ssh.Client
	Session *ssh.Session
	Stdin   io.WriteCloser
	Stdout  io.Reader
	Stderr  io.Reader
}

func New(cipher *security.Cipher, cfg Settings) *Gateway { return &Gateway{cipher: cipher, cfg: cfg} }
func (g *Gateway) decrypt(c store.Credential) (Secret, error) {
	var x Secret
	b, e := g.cipher.Decrypt(c.EncryptedSecret)
	if e != nil {
		return x, e
	}
	e = json.Unmarshal(b, &x)
	return x, e
}
func (g *Gateway) authMethod(c store.Credential) (ssh.AuthMethod, error) {
	x, e := g.decrypt(c)
	if e != nil {
		return nil, fmt.Errorf("decrypt credential: %w", e)
	}
	switch c.Type {
	case "password":
		if x.Password == "" {
			return nil, errors.New("empty password")
		}
		return ssh.Password(x.Password), nil
	case "key":
		var signer ssh.Signer
		if x.Passphrase != "" {
			signer, e = ssh.ParsePrivateKeyWithPassphrase([]byte(x.PrivateKey), []byte(x.Passphrase))
		} else {
			signer, e = ssh.ParsePrivateKey([]byte(x.PrivateKey))
		}
		if e != nil {
			return nil, fmt.Errorf("parse private key: %w", e)
		}
		return ssh.PublicKeys(signer), nil
	default:
		return nil, fmt.Errorf("unsupported credential type")
	}
}

func validateAddress(host string, port int, listen string) error {
	if port < 1 || port > 65535 {
		return errors.New("invalid SSH port")
	}
	lower := strings.ToLower(strings.TrimSpace(host))
	if lower == "169.254.169.254" || lower == "metadata.google.internal" || lower == "100.100.100.200" {
		return errors.New("metadata endpoints are blocked")
	}
	if lh, lp, e := net.SplitHostPort(listen); e == nil && portString(port) == lp && (host == lh || ((lh == "0.0.0.0" || lh == "::") && (lower == "localhost" || lower == "127.0.0.1" || lower == "::1"))) {
		return errors.New("connection to the WebSSH management listener is blocked")
	}
	return nil
}
func portString(p int) string { return fmt.Sprintf("%d", p) }

func (g *Gateway) netConn(ctx context.Context, h store.Host) (net.Conn, error) {
	if e := validateAddress(h.Hostname, h.Port, g.cfg.Listen); e != nil {
		return nil, e
	}
	d := net.Dialer{Timeout: g.cfg.ConnectTimeout}
	return d.DialContext(ctx, "tcp", net.JoinHostPort(h.Hostname, portString(h.Port)))
}
func hostKeyCallback(h store.Host) ssh.HostKeyCallback {
	return func(_ string, _ net.Addr, key ssh.PublicKey) error {
		fp := ssh.FingerprintSHA256(key)
		if h.HostKeyFingerprint == "" {
			return &HostKeyError{Code: "SSH_HOST_KEY_UNKNOWN", KeyType: key.Type(), Fingerprint: fp}
		}
		if fp != h.HostKeyFingerprint || key.Type() != h.HostKeyType {
			return &HostKeyError{Code: "SSH_HOST_KEY_MISMATCH", KeyType: key.Type(), Fingerprint: fp, Expected: h.HostKeyFingerprint}
		}
		return nil
	}
}

func (g *Gateway) ScanHostKey(ctx context.Context, h store.Host) (string, string, error) {
	conn, e := g.netConn(ctx, h)
	if e != nil {
		return "", "", e
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(g.cfg.HandshakeTimeout))
	var typ, fp string
	cfg := &ssh.ClientConfig{User: h.Username, HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
		typ = key.Type()
		fp = ssh.FingerprintSHA256(key)
		return errors.New("host key observed")
	}, Timeout: g.cfg.HandshakeTimeout}
	_, _, _, _ = ssh.NewClientConn(conn, net.JoinHostPort(h.Hostname, portString(h.Port)), cfg)
	if fp == "" {
		return "", "", errors.New("server did not present a host key")
	}
	return typ, fp, nil
}

func (g *Gateway) Dial(ctx context.Context, h store.Host, c store.Credential, cols, rows int) (*Connection, error) {
	method, e := g.authMethod(c)
	if e != nil {
		return nil, e
	}
	conn, e := g.netConn(ctx, h)
	if e != nil {
		return nil, fmt.Errorf("tcp connect: %w", e)
	}
	_ = conn.SetDeadline(time.Now().Add(g.cfg.HandshakeTimeout))
	cfg := &ssh.ClientConfig{User: h.Username, Auth: []ssh.AuthMethod{method}, HostKeyCallback: hostKeyCallback(h), Timeout: g.cfg.HandshakeTimeout}
	cc, chans, reqs, e := ssh.NewClientConn(conn, net.JoinHostPort(h.Hostname, portString(h.Port)), cfg)
	if e != nil {
		conn.Close()
		return nil, e
	}
	_ = conn.SetDeadline(time.Time{})
	client := ssh.NewClient(cc, chans, reqs)
	session, e := client.NewSession()
	if e != nil {
		client.Close()
		return nil, e
	}
	modes := ssh.TerminalModes{ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 38400, ssh.TTY_OP_OSPEED: 38400}
	if e = session.RequestPty(g.cfg.DefaultTerm, rows, cols, modes); e != nil {
		session.Close()
		client.Close()
		return nil, fmt.Errorf("request PTY: %w", e)
	}
	stdin, e := session.StdinPipe()
	if e != nil {
		session.Close()
		client.Close()
		return nil, e
	}
	stdout, e := session.StdoutPipe()
	if e != nil {
		session.Close()
		client.Close()
		return nil, e
	}
	stderr, e := session.StderrPipe()
	if e != nil {
		session.Close()
		client.Close()
		return nil, e
	}
	if e = session.Shell(); e != nil {
		session.Close()
		client.Close()
		return nil, e
	}
	return &Connection{Client: client, Session: session, Stdin: stdin, Stdout: stdout, Stderr: stderr}, nil
}
func (c *Connection) Close()                      { _ = c.Stdin.Close(); _ = c.Session.Close(); _ = c.Client.Close() }
func (c *Connection) Resize(cols, rows int) error { return c.Session.WindowChange(rows, cols) }
func (c *Connection) Wait() error                 { return c.Session.Wait() }
func (c *Connection) Keepalive(ctx context.Context, every time.Duration) {
	if every <= 0 {
		return
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_, _, e := c.Client.SendRequest("keepalive@openssh.com", true, nil)
			if e != nil {
				return
			}
		}
	}
}
