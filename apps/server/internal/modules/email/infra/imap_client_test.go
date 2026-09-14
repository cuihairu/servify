package infra

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"servify/apps/server/internal/config"

	"github.com/emersion/go-imap/v2"
	"math/big"
)

// genSelfSignedCert 生成仅用于回环测试的自签证书。
func genSelfSignedCert(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:     []string{"localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// imapFixture 描述假服务器里唯一一封可 FETCH 的邮件。
type imapFixture struct {
	uid         uint32
	fromName    string
	fromMailbox string
	fromHost    string
	subject     string
	messageID   string
	body        string
	attachment  bool
}

func (f imapFixture) envelope() string {
	// 地址列表是嵌套结构：list of (name NIL mailbox host)；
	// ENVELOPE 共 10 字段：date subject from sender reply-to to cc bcc in-reply-to message-id
	addr := fmt.Sprintf("((%q NIL %q %q))", f.fromName, f.fromMailbox, f.fromHost)
	return fmt.Sprintf("(NIL %q %s %s %s NIL NIL NIL NIL %q)",
		f.subject, addr, addr, addr, f.messageID)
}

func (f imapFixture) bodyStructure() string {
	dsp := "NIL"
	if f.attachment {
		dsp = `("attachment" ("filename" "a.pdf"))`
	}
	return fmt.Sprintf(`("text" "plain" ("charset" "utf-8") NIL NIL "7bit" %d 1 NIL %s NIL NIL)`,
		len(f.body), dsp)
}

// fetchResponse 不含行尾 CRLF：writeLine 统一追加，literal 内的 CRLF 除外。
func (f imapFixture) fetchResponse(seq uint32) string {
	return fmt.Sprintf("* %d FETCH (UID %d ENVELOPE %s BODYSTRUCTURE %s BODY[TEXT] {%d}\r\n%s)",
		seq, f.uid, f.envelope(), f.bodyStructure(), len(f.body), f.body)
}

// fakeIMAPServer 是仅覆盖 GoIMAPClient 所用命令子集的内存 IMAP 服务。
type fakeIMAPServer struct {
	ln            net.Listener
	tlsCert       *tls.Certificate // 非空 = TLS 监听
	loginOK       bool
	examineOK     bool
	searchOK      bool
	searchEsearch bool // 真则对 SEARCH 回带 correlator 的 ESEARCH（IMAP4rev2 风格）
	fetchOK       bool
	fetchEmpty    bool
	uidValidity   uint32
	uids          []uint32
	fixture       imapFixture

	tlsConfig *tls.Config // 客户端侧校验配置（tls.Dialer 用）

	searchMu      sync.Mutex
	lastSearchCmd string // 最近一条 SEARCH 命令的命令 token（如 "UID SEARCH"）
}

// lastSearch 返回最近一条 SEARCH 命令 token（并发安全）。
func (s *fakeIMAPServer) lastSearch() string {
	s.searchMu.Lock()
	defer s.searchMu.Unlock()
	return s.lastSearchCmd
}

func startFakeIMAPServer(t *testing.T, mutate func(*fakeIMAPServer)) *fakeIMAPServer {
	t.Helper()
	srv := &fakeIMAPServer{
		loginOK:     true,
		examineOK:   true,
		searchOK:    true,
		fetchOK:     true,
		uidValidity: 77,
		uids:        []uint32{3, 5, 9},
		fixture: imapFixture{
			uid:         3,
			fromName:    "Alice",
			fromMailbox: "alice",
			fromHost:    "example.com",
			subject:     "subj here",
			messageID:   "<m1@x>",
			body:        "hello world",
		},
	}
	if mutate != nil {
		mutate(srv)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv.ln = ln
	if srv.tlsCert != nil {
		srv.tlsConfig = &tls.Config{Certificates: []tls.Certificate{*srv.tlsCert}}
		ln = tls.NewListener(ln, srv.tlsConfig)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go srv.serve(conn)
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return srv
}

func (s *fakeIMAPServer) address() string {
	return s.ln.Addr().String()
}

func (s *fakeIMAPServer) clientConfig() config.EmailConfig {
	host, portStr, _ := net.SplitHostPort(s.address())
	var port int
	fmt.Sscanf(portStr, "%d", &port)
	return config.EmailConfig{
		Host:       host,
		Port:       port,
		Username:   "user",
		Password:   "pass",
		UseTLS:     s.tlsCert != nil,
		SkipVerify: true,
	}
}

func (s *fakeIMAPServer) serve(conn net.Conn) {
	defer conn.Close()
	if !s.writeLine(conn, "* OK IMAP4rev1 ready") {
		return
	}
	br := bufio.NewReader(conn)
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return
		}
		tag := fields[0]
		cmd := strings.ToUpper(fields[1])
		if cmd == "UID" && len(fields) >= 3 {
			// UID SEARCH / UID FETCH：真正的命令名在第三个 token
			cmd = strings.ToUpper(fields[2])
		}
		switch cmd {
		case "LOGIN":
			if s.loginOK {
				if !s.writeLine(conn, tag+" OK LOGIN done") {
					return
				}
			} else if !s.writeLine(conn, tag+" NO [AUTHENTICATIONFAILED] bad credentials") {
				return
			}
		case "SELECT", "EXAMINE":
			if !s.examineOK {
				if !s.writeLine(conn, tag+" NO no such mailbox") {
					return
				}
				continue
			}
			if !s.writeLine(conn, "* 3 EXISTS") ||
				!s.writeLine(conn, fmt.Sprintf("* OK [UIDVALIDITY %d] UIDs valid", s.uidValidity)) ||
				!s.writeLine(conn, tag+" OK [READ-ONLY] done") {
				return
			}
		case "SEARCH":
			cmdTokens := fields[1:]
			if len(cmdTokens) > 2 {
				cmdTokens = cmdTokens[:2]
			}
			s.searchMu.Lock()
			s.lastSearchCmd = strings.ToUpper(strings.Join(cmdTokens, " "))
			s.searchMu.Unlock()
			if !s.searchOK {
				if !s.writeLine(conn, tag+" NO search failed") {
					return
				}
				continue
			}
			// RFC 3501：UID SEARCH 的应答是普通 "* SEARCH <n>..."，数字即
			// UID（与序号无关）；假服务器固定回 s.uids，序号本应是 1..3，
			// 客户端若误发普通 SEARCH 或按序号解释应答都得不到 [3 5 9]。
			uids := make([]string, 0, len(s.uids))
			for _, uid := range s.uids {
				uids = append(uids, strconv.Itoa(int(uid)))
			}
			var data string
			if s.searchEsearch {
				// IMAP4rev2/ESEARCH 服务器改用带 search-correlator 的应答；
				// go-imap 要求 tag 匹配 pending 命令，且 "UID ALL" 才会落进 UIDSet。
				data = fmt.Sprintf("* ESEARCH (TAG %q) UID ALL %s", tag, strings.Join(uids, ","))
			} else {
				data = "* SEARCH " + strings.Join(uids, " ")
			}
			if !s.writeLine(conn, data) ||
				!s.writeLine(conn, tag+" OK search done") {
				return
			}
		case "FETCH":
			if !s.fetchOK {
				if !s.writeLine(conn, tag+" NO fetch failed") {
					return
				}
				continue
			}
			if s.fetchEmpty {
				if !s.writeLine(conn, tag+" OK fetch done") {
					return
				}
				continue
			}
			if !s.writeLine(conn, s.fixture.fetchResponse(1)) ||
				!s.writeLine(conn, tag+" OK fetch done") {
				return
			}
		case "LOGOUT":
			_ = s.writeLine(conn, "* BYE")
			_ = s.writeLine(conn, tag+" OK logout done")
			return
		default:
			if !s.writeLine(conn, tag+" NO unknown command") {
				return
			}
		}
	}
}

func (s *fakeIMAPServer) writeLine(conn net.Conn, line string) bool {
	_, err := io.WriteString(conn, line+"\r\n")
	return err == nil
}

func newTestIMAPClient(cfg config.EmailConfig) *GoIMAPClient {
	return NewGoIMAPClient(cfg, nil)
}

func TestNewGoIMAPClientDefaultsAndAddress(t *testing.T) {
	// nil logger 落到 StandardLogger；port<=0 落到 993
	c := NewGoIMAPClient(config.EmailConfig{Host: "imap.example.com"}, nil)
	if c.logger == nil {
		t.Fatal("nil logger must fall back to standard logger")
	}
	if got := c.address(); got != "imap.example.com:993" {
		t.Fatalf("address = %q, want default 993 port", got)
	}
	c.cfg.Port = 1234
	if got := c.address(); got != "imap.example.com:1234" {
		t.Fatalf("address = %q, want configured port", got)
	}
}

func TestGoIMAPClientHappyPathSelectSearchFetchClose(t *testing.T) {
	srv := startFakeIMAPServer(t, nil)
	c := newTestIMAPClient(srv.clientConfig())

	uv, err := c.Select(t.Context(), "INBOX")
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if uv != 77 {
		t.Fatalf("uidvalidity = %d, want 77", uv)
	}

	uids, err := c.SearchSinceUID(t.Context(), 0)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(uids) != 3 || uids[0] != 3 || uids[2] != 9 {
		t.Fatalf("uids = %v, want [3 5 9]", uids)
	}

	msg, err := c.Fetch(t.Context(), 3)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if msg.UID != 3 {
		t.Fatalf("uid = %d, want 3", msg.UID)
	}
	if msg.From != "alice@example.com" {
		t.Fatalf("from = %q", msg.From)
	}
	if msg.Subject != "subj here" {
		t.Fatalf("subject = %q", msg.Subject)
	}
	// IMAP ENVELOPE 的 message-id 不带尖括号
	if msg.MessageID != "m1@x" {
		t.Fatalf("message id = %q", msg.MessageID)
	}
	if msg.TextBody != "hello world" {
		t.Fatalf("body = %q", msg.TextBody)
	}
	if msg.HasAttachment {
		t.Fatal("plain mail must not be flagged as attachment")
	}

	if err := c.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	// Close 幂等：client 已清空后直接返回 nil
	if err := c.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

// TestGoIMAPClientSearchSinceUIDSendsUIDSearch 是语义 bug 回归：
// SearchSinceUID 必须发 UID SEARCH（client.UIDSearch）。旧实现发普通
// SEARCH——应答数字是序号，落进 SeqSet 后 AllUIDs() 的类型断言失败
// 静默返回空列表，轮询器永远看不到新邮件。
func TestGoIMAPClientSearchSinceUIDSendsUIDSearch(t *testing.T) {
	srv := startFakeIMAPServer(t, nil)
	c := newTestIMAPClient(srv.clientConfig())
	if _, err := c.Select(t.Context(), "INBOX"); err != nil {
		t.Fatalf("select: %v", err)
	}
	uids, err := c.SearchSinceUID(t.Context(), 0)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	// 假服务器 EXISTS=3（序号 1..3）但 UID 是 3/5/9：结果必须按 UID 解出
	if len(uids) != 3 || uids[0] != 3 || uids[1] != 5 || uids[2] != 9 {
		t.Fatalf("uids = %v, want [3 5 9]", uids)
	}
	if got := srv.lastSearch(); got != "UID SEARCH" {
		t.Fatalf("wire command = %q, want %q", got, "UID SEARCH")
	}
}

// TestGoIMAPClientSearchSinceUIDParsesEsearchResponse 覆盖 ESEARCH 应答
// 变体：IMAP4rev2/ESEARCH 服务器以 "* ESEARCH (TAG ...) UID ALL ..." 回
// UID SEARCH，客户端须按 correlator tag 关联命令并把结果落进 UIDSet。
func TestGoIMAPClientSearchSinceUIDParsesEsearchResponse(t *testing.T) {
	srv := startFakeIMAPServer(t, func(s *fakeIMAPServer) { s.searchEsearch = true })
	c := newTestIMAPClient(srv.clientConfig())
	if _, err := c.Select(t.Context(), "INBOX"); err != nil {
		t.Fatalf("select: %v", err)
	}
	uids, err := c.SearchSinceUID(t.Context(), 0)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(uids) != 3 || uids[0] != 3 || uids[1] != 5 || uids[2] != 9 {
		t.Fatalf("uids = %v, want [3 5 9]", uids)
	}
	if got := srv.lastSearch(); got != "UID SEARCH" {
		t.Fatalf("wire command = %q, want %q", got, "UID SEARCH")
	}
}

func TestGoIMAPClientSearchSinceUIDFiltersByCursor(t *testing.T) {
	srv := startFakeIMAPServer(t, nil)
	c := newTestIMAPClient(srv.clientConfig())
	if _, err := c.Select(t.Context(), "INBOX"); err != nil {
		t.Fatalf("select: %v", err)
	}
	// sinceUID>0 走 AddRange(sinceUID+1, "*") 分支；假服务器仍回全部 UID，
	// 这里只验证命令成功且解析出升序结果。
	uids, err := c.SearchSinceUID(t.Context(), 3)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(uids) == 0 {
		t.Fatal("expected search results")
	}
}

func TestGoIMAPClientFetchDetectsAttachmentDisposition(t *testing.T) {
	srv := startFakeIMAPServer(t, func(s *fakeIMAPServer) {
		s.fixture.attachment = true
	})
	c := newTestIMAPClient(srv.clientConfig())
	if _, err := c.Select(t.Context(), "INBOX"); err != nil {
		t.Fatalf("select: %v", err)
	}
	msg, err := c.Fetch(t.Context(), 3)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if !msg.HasAttachment {
		t.Fatal("attachment disposition must be detected")
	}
}

func TestGoIMAPClientFetchEmptyBuffersIsNoSuchMessage(t *testing.T) {
	srv := startFakeIMAPServer(t, func(s *fakeIMAPServer) { s.fetchEmpty = true })
	c := newTestIMAPClient(srv.clientConfig())
	if _, err := c.Select(t.Context(), "INBOX"); err != nil {
		t.Fatalf("select: %v", err)
	}
	if _, err := c.Fetch(t.Context(), 3); err == nil || !strings.Contains(err.Error(), "no such message") {
		t.Fatalf("expected no-such-message error, got %v", err)
	}
}

func TestGoIMAPClientOperationsBeforeSelectFail(t *testing.T) {
	c := newTestIMAPClient(config.EmailConfig{Host: "127.0.0.1"})
	if _, err := c.SearchSinceUID(t.Context(), 0); err == nil || !strings.Contains(err.Error(), "not selected") {
		t.Fatalf("search before select must fail, got %v", err)
	}
	if _, err := c.Fetch(t.Context(), 1); err == nil || !strings.Contains(err.Error(), "not selected") {
		t.Fatalf("fetch before select must fail, got %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("close without connection must be no-op, got %v", err)
	}
}

func TestGoIMAPClientConnectDialError(t *testing.T) {
	// 占住端口再释放：得到一个确定无人监听的本地端口
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	c := newTestIMAPClient(config.EmailConfig{Host: "127.0.0.1", Port: port})
	if _, err := c.Select(t.Context(), "INBOX"); err == nil || !strings.Contains(err.Error(), "imap dial") {
		t.Fatalf("expected dial error, got %v", err)
	}
}

func TestGoIMAPClientLoginFailure(t *testing.T) {
	srv := startFakeIMAPServer(t, func(s *fakeIMAPServer) { s.loginOK = false })
	c := newTestIMAPClient(srv.clientConfig())
	if _, err := c.Select(t.Context(), "INBOX"); err == nil || !strings.Contains(err.Error(), "imap login") {
		t.Fatalf("expected login error, got %v", err)
	}
}

func TestGoIMAPClientSelectFailureDropsConnection(t *testing.T) {
	srv := startFakeIMAPServer(t, func(s *fakeIMAPServer) { s.examineOK = false })
	c := newTestIMAPClient(srv.clientConfig())
	if _, err := c.Select(t.Context(), "INBOX"); err == nil || !strings.Contains(err.Error(), "imap select") {
		t.Fatalf("expected select error, got %v", err)
	}
	// dropConn 已把连接清掉：后续 Search 报 not selected 而非复用坏连接
	if _, err := c.SearchSinceUID(t.Context(), 0); err == nil || !strings.Contains(err.Error(), "not selected") {
		t.Fatalf("connection must be dropped after select failure, got %v", err)
	}
}

func TestGoIMAPClientSearchFailureDropsConnection(t *testing.T) {
	srv := startFakeIMAPServer(t, func(s *fakeIMAPServer) { s.searchOK = false })
	c := newTestIMAPClient(srv.clientConfig())
	if _, err := c.Select(t.Context(), "INBOX"); err != nil {
		t.Fatalf("select: %v", err)
	}
	if _, err := c.SearchSinceUID(t.Context(), 0); err == nil || !strings.Contains(err.Error(), "imap search") {
		t.Fatalf("expected search error, got %v", err)
	}
	if _, err := c.Fetch(t.Context(), 3); err == nil || !strings.Contains(err.Error(), "not selected") {
		t.Fatalf("connection must be dropped after search failure, got %v", err)
	}
}

func TestGoIMAPClientFetchFailure(t *testing.T) {
	srv := startFakeIMAPServer(t, func(s *fakeIMAPServer) { s.fetchOK = false })
	c := newTestIMAPClient(srv.clientConfig())
	if _, err := c.Select(t.Context(), "INBOX"); err != nil {
		t.Fatalf("select: %v", err)
	}
	if _, err := c.Fetch(t.Context(), 3); err == nil || !strings.Contains(err.Error(), "imap fetch uid 3") {
		t.Fatalf("expected fetch error, got %v", err)
	}
}

func TestGoIMAPClientTLSConnection(t *testing.T) {
	cert := genSelfSignedCert(t)
	srv := startFakeIMAPServer(t, func(s *fakeIMAPServer) { s.tlsCert = &cert })
	c := newTestIMAPClient(srv.clientConfig())

	uv, err := c.Select(t.Context(), "INBOX")
	if err != nil {
		t.Fatalf("tls select: %v", err)
	}
	if uv != 77 {
		t.Fatalf("uidvalidity = %d, want 77", uv)
	}
	if _, err := c.SearchSinceUID(t.Context(), 0); err != nil {
		t.Fatalf("tls search: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestFirstEmailAddressPicksFirstUsable(t *testing.T) {
	addrs := []imap.Address{
		{Mailbox: "", Host: "skip.example.com"},
		{Mailbox: "real", Host: ""},
		{Mailbox: "alice", Host: "example.com"},
	}
	if got := firstEmailAddress(addrs); got != "alice@example.com" {
		t.Fatalf("firstEmailAddress = %q", got)
	}
	if got := firstEmailAddress(nil); got != "" {
		t.Fatalf("empty list must yield empty string, got %q", got)
	}
	if got := firstEmailAddress([]imap.Address{{Mailbox: "", Host: "x.com"}}); got != "" {
		t.Fatalf("unusable address must yield empty string, got %q", got)
	}
}

func TestHasAttachmentDispositionWalksParts(t *testing.T) {
	if hasAttachmentDisposition(nil) {
		t.Fatal("nil bodystructure must not be attachment")
	}
	plain := &imap.BodyStructureSinglePart{}
	if hasAttachmentDisposition(plain) {
		t.Fatal("plain part must not be attachment")
	}
	withAtt := &imap.BodyStructureSinglePart{
		Extended: &imap.BodyStructureSinglePartExt{
			Disposition: &imap.BodyStructureDisposition{Value: "Attachment"},
		},
	}
	if !hasAttachmentDisposition(withAtt) {
		t.Fatal("attachment disposition must be detected case-insensitively")
	}
	multi := &imap.BodyStructureMultiPart{
		Children: []imap.BodyStructure{plain, withAtt},
	}
	if !hasAttachmentDisposition(multi) {
		t.Fatal("nested attachment must be found by walk")
	}
}
