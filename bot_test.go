package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type sent struct{ chat, text, file string }

type fakeTransport struct {
	mu   sync.Mutex
	self string
	msgs chan sent
}

func newFakeTransport() *fakeTransport {
	return &fakeTransport{self: "32470000000@s.whatsapp.net", msgs: make(chan sent, 100)}
}

func (f *fakeTransport) Self() string { return f.self }
func (f *fakeTransport) SendText(chat, text string) error {
	f.msgs <- sent{chat: chat, text: text}
	return nil
}
func (f *fakeTransport) SendFile(chat, name, contentType string, data []byte) error {
	f.msgs <- sent{chat: chat, file: name + ":" + string(data)}
	return nil
}

// next: the next message containing want (the others are skipped)
func (f *fakeTransport) next(t *testing.T, want string) sent {
	t.Helper()
	timeout := time.After(10 * time.Second)
	var seen []string
	for {
		select {
		case m := <-f.msgs:
			if os.Getenv("BOT_DEBUG") != "" {
				t.Logf("<< %s%s", m.text, m.file)
			}
			if strings.Contains(m.text+m.file, want) {
				return m
			}
			seen = append(seen, m.text+m.file)
		case <-timeout:
			t.Fatalf("no message with %q; got:\n%s", want, strings.Join(seen, "\n---\n"))
		}
	}
}

// answerAll: "." (the default) to every question until the command ends
func (f *fakeTransport) answerAll(t *testing.T, eng *Engine) []string {
	t.Helper()
	var all []string
	for {
		select {
		case m := <-f.msgs:
			all = append(all, m.text+m.file)
			if strings.Contains(m.text, "cancel stops") {
				eng.Handle(self("."))
			}
		case <-time.After(500 * time.Millisecond):
			eng.mu.Lock()
			idle := len(eng.busy) == 0
			eng.mu.Unlock()
			if idle {
				return all
			}
		}
	}
}

const me = "32470000000"

func self(text string) Incoming {
	return Incoming{Chat: me, Sender: me, FromMe: true, Text: text}
}

func botEnv(t *testing.T) (*env, *fakeTransport, *Engine) {
	e := newEnv(t)
	e.init(t)
	ft := newFakeTransport()
	eng := NewEngine(ft, e.config, []string{"+32 471 11 22 33"})
	eng.groupDelay = 20 * time.Millisecond
	t.Cleanup(eng.Close)
	return e, ft, eng
}

func TestBotAuthorized(t *testing.T) {
	eng := NewEngine(newFakeTransport(), "", []string{"+32 471 11 22 33", "0032472000000"})
	cases := []struct {
		m    Incoming
		want bool
	}{
		{Incoming{Chat: me + "@s.whatsapp.net", FromMe: true}, true},                  // message yourself
		{Incoming{Chat: "32499999999@s.whatsapp.net", FromMe: true}, false},           // me, writing to someone else
		{Incoming{Chat: "32471112233", Sender: "32471112233:5@s.whatsapp.net"}, true}, // allowed, in its chat
		{Incoming{Chat: "32472000000", Sender: "32472000000"}, true},
		{Incoming{Chat: "32499999999", Sender: "32499999999"}, false},        // a stranger
		{Incoming{Chat: "group", Sender: "32471112233", Group: true}, false}, // groups: never
		{Incoming{Chat: me, Sender: "32471112233"}, false},                   // allowed, but not its chat
	}
	for i, c := range cases {
		if got := eng.authorized(c.m); got != c.want {
			t.Errorf("case %d: %v, want %v", i, got, c.want)
		}
	}
}

func TestCommandArgs(t *testing.T) {
	got := strings.Join(commandArgs(`Artcoded Invoices list --config /etc/x --json "a b"`), "|")
	if got != "invoices|list|--json|a b" {
		t.Fatal(got)
	}
}

func TestSplitMessage(t *testing.T) {
	long := "```\n" + strings.Repeat("0123456789\n", 50) + "```"
	parts := splitMessage(long, 120)
	if len(parts) < 4 {
		t.Fatalf("%d parts", len(parts))
	}
	for _, p := range parts {
		if len(p) > 120 || !strings.HasPrefix(p, "```\n") || !strings.HasSuffix(p, "\n```") {
			t.Fatalf("part %q", p)
		}
	}
}

func TestBotHelpAndBlockedCommands(t *testing.T) {
	_, ft, eng := botEnv(t)
	eng.Handle(self("help"))
	m := ft.next(t, "invoices list")
	if m.chat != me || strings.Contains(m.text, "\x1b[") {
		t.Fatalf("%+v", m)
	}
	eng.Handle(self("reset"))
	ft.next(t, "Not here")
	// strangers get nothing at all
	eng.Handle(Incoming{Chat: "32499999999", Sender: "32499999999", Text: "help"})
	eng.Handle(Incoming{Chat: "32471112233", Sender: "32471112233", Text: "hello"})
	m = ft.next(t, "invoices list")
	if m.chat != "32471112233" {
		t.Fatalf("answer sent to %s", m.chat)
	}
}

func TestBotAsksAndCancels(t *testing.T) {
	_, ft, eng := botEnv(t)
	eng.Handle(self("expenses add"))
	ft.next(t, "send the file in the chat")
	eng.Handle(Incoming{Chat: me, Sender: me, FromMe: true, Files: []IncomingFile{{Name: "a.pdf", Data: []byte("A")}}})
	ft.next(t, "What is this file for?")
	eng.Handle(self("cancel"))
	ft.next(t, "Cancelled.")
	// the session is free again
	eng.Handle(self("help"))
	ft.next(t, "invoices list")
}

func TestBotFileWithoutCaptionBecomesAnExpense(t *testing.T) {
	e, ft, eng := botEnv(t)
	eng.Handle(Incoming{Chat: me, Sender: me, FromMe: true, Files: []IncomingFile{{Name: "train ticket.pdf", Data: []byte("A")}}})
	eng.Handle(Incoming{Chat: me, Sender: me, FromMe: true, Files: []IncomingFile{{Name: "receipt.png", Data: []byte("B")}}})
	ft.next(t, "What are these 2 files for?")
	eng.Handle(self("1"))
	ft.next(t, "Subject")
	eng.Handle(self(".")) // the default: the name of the first file
	// then the description, the tag and the price: the defaults
	out := strings.Join(ft.answerAll(t, eng), "\n")
	if !strings.Contains(out, "expense added: train ticket") {
		t.Fatal(out)
	}
	files := strings.Join(e.f.files["/api/fee/manual-submit"], "|")
	if files != "files=train ticket.pdf:A|files=receipt.png:B" {
		t.Fatalf("files %s", files)
	}
	if got := e.f.fields["/api/fee/manual-submit"]["subject"]; len(got) != 1 || got[0] != "train ticket" {
		t.Fatalf("subject %v", got)
	}
}

func TestBotFileCaptions(t *testing.T) {
	e, ft, eng := botEnv(t)
	// a shortcut and the title
	eng.Handle(Incoming{Chat: me, Sender: me, FromMe: true, Text: "document Statuts 2026", Files: []IncomingFile{{Name: "s.pdf", Data: []byte("S")}}})
	ft.answerAll(t, eng)
	if f := e.f.fields["/api/administrative-document/save"]; len(f["title"]) != 1 || f["title"][0] != "Statuts 2026" {
		t.Fatalf("fields %v", f)
	}
	// a command of the cli: the file is its --file
	_, ft, eng = botEnv(t)
	eng.Handle(Incoming{Chat: me, Sender: me, FromMe: true, Text: "uploads upload --public", Files: []IncomingFile{{Name: "logo.png", Data: []byte("L")}}})
	out := strings.Join(ft.answerAll(t, eng), "\n")
	// (the fake backend has no uploads: the error shows the file was sent)
	if strings.Contains(out, "not a flag") || !strings.Contains(out, "logo.png") {
		t.Fatal(out)
	}
}

func TestBotSendsDownloads(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.txt")
	_ = os.WriteFile(p, []byte("X"), 0o644)
	ft := newFakeTransport()
	eng := NewEngine(ft, "", nil)
	eng.exec(&session{chat: me}, func() error { return openInBrowser(p) })
	if m := ft.next(t, "x.txt"); m.file != "x.txt:X" {
		t.Fatalf("%+v", m)
	}
	eng.exec(&session{chat: me}, func() error { return openInBrowser("https://example.org/invoices") })
	ft.next(t, "https://example.org/invoices")
	if bot != nil || lineSource != nil || stdout != os.Stdout {
		t.Fatal("the global state is not restored")
	}
}

func TestBotConsole(t *testing.T) {
	e := newEnv(t)
	e.init(t)
	var out strings.Builder
	in := strings.NewReader("help\n/from 32499999999 help\n/quit\n")
	w := &lockedWriter{w: &out}
	if err := runBotConsole(e.config, in, w); err != nil {
		t.Fatal(err)
	}
	s := w.String()
	if !strings.Contains(s, "invoices list") || !strings.Contains(s, "ignored: 32499999999") {
		t.Fatal(s)
	}
}

type lockedWriter struct {
	mu sync.Mutex
	w  *strings.Builder
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func (l *lockedWriter) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.String()
}

func TestChatTables(t *testing.T) {
	bot = &botHooks{width: 42}
	defer func() { bot = nil }()
	var b strings.Builder
	// narrow: stays a table, monospaced
	table(&b, []string{"month", "total"}, [][]string{{"Jan", "12,00 €"}, {"Feb", "1 200,00 €"}})
	if s := b.String(); !strings.HasPrefix(s, "```\nMONTH") || !strings.Contains(s, "Feb    1 200,00 €") {
		t.Fatal(s)
	}
	// wide: a card per row
	b.Reset()
	table(&b, []string{"number", "client", "description", "total", "id"},
		[][]string{{"2026-001", "ACME", dim("a long description of the invoice"), "1 210,00 €", dim("i1")}})
	if s := b.String(); s != "*2026-001*\n_client_: ACME\n_description_: a long description of the invoice\n_total_: 1 210,00 €\nid `i1`\n" {
		t.Fatalf("%q", s)
	}
	// no headers: key: value lines
	b.Reset()
	table(&b, []string{"", ""}, [][]string{{"Revenue", "7 000,00 €"}})
	if s := b.String(); s != "*Revenue*: 7 000,00 €\n" {
		t.Fatalf("%q", s)
	}
}
