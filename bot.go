package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strings"
	"sync"
	"time"
)

// The chat bot: the commands of the cli, run from a conversation (WhatsApp, or the console to try it).
// The engine knows nothing of WhatsApp: a Transport delivers the messages and sends the answers.

// IncomingFile: a file sent in the conversation
type IncomingFile struct {
	Name, ContentType string
	Data              []byte
}

// Incoming: a message received
type Incoming struct {
	ID     string
	Chat   string // the conversation (a phone number for a direct chat)
	Sender string // who wrote it (a phone number)
	FromMe bool   // written by the account of the bot (the "message yourself" chat)
	Group  bool
	Text   string // the text, or the caption of the files
	Files  []IncomingFile
}

// Transport: the messaging side
type Transport interface {
	Self() string // the number of the account
	SendText(chat, text string) error
	SendFile(chat, name, contentType string, data []byte) error
}

// botHooks: what the commands do differently in a conversation (nil outside the bot)
type botHooks struct {
	sendFile func(name, contentType string, data []byte) error
	sendLink func(url string) error
	width    int
}

var bot *botHooks

// lineSource: where the answers come from in a conversation (nil: the terminal)
var lineSource func(secret bool) (string, error)

type Engine struct {
	t             Transport
	cfgPath       string
	allowed       map[string]bool
	answerTimeout time.Duration
	groupDelay    time.Duration
	width         int

	mu       sync.Mutex
	sessions map[string]*session
	busy     chan struct{} // one command at a time: the cli has a global state
	wg       sync.WaitGroup
}

type session struct {
	chat    string
	running bool
	answers chan string // set while a command waits for an answer
	pending []IncomingFile
	caption string
	timer   *time.Timer
}

func NewEngine(t Transport, cfgPath string, allowed []string) *Engine {
	e := &Engine{t: t, cfgPath: cfgPath, allowed: map[string]bool{}, answerTimeout: 5 * time.Minute,
		groupDelay: 3 * time.Second, width: 42, sessions: map[string]*session{}, busy: make(chan struct{}, 1)}
	for _, n := range allowed {
		if n = normalizeNumber(n); n != "" {
			e.allowed[n] = true
		}
	}
	return e
}

var nonDigits = regexp.MustCompile(`\D`)

// normalizeNumber: +32 470 12 34 56 -> 32470123456
func normalizeNumber(n string) string {
	n = strings.TrimSpace(n)
	if i := strings.IndexAny(n, "@:"); i >= 0 {
		n = n[:i]
	}
	n = nonDigits.ReplaceAllString(n, "")
	return strings.TrimPrefix(n, "00")
}

// authorized: the "message yourself" chat of the account, or a direct chat with an allowed number.
// Everything else (other chats of the account, groups, strangers) is ignored.
func (e *Engine) authorized(m Incoming) bool {
	if m.Group {
		return false
	}
	self := normalizeNumber(e.t.Self())
	chat := normalizeNumber(m.Chat)
	if m.FromMe {
		return chat == self
	}
	sender := normalizeNumber(m.Sender)
	return e.allowed[sender] && chat == sender
}

// Handle a message; never blocks (the commands run in their own goroutine)
func (e *Engine) Handle(m Incoming) {
	if !e.authorized(m) {
		return
	}
	e.mu.Lock()
	s := e.sessions[m.Chat]
	if s == nil {
		s = &session{chat: m.Chat}
		e.sessions[m.Chat] = s
	}
	// an answer to the question of the running command
	if s.answers != nil {
		answers := s.answers
		e.mu.Unlock()
		if len(m.Files) > 0 {
			e.send(s.chat, "Answer the question first (or *cancel*), then send the files again.")
			return
		}
		select {
		case answers <- m.Text:
		default:
		}
		return
	}
	if len(m.Files) > 0 {
		// the files sent together (an album) are one batch
		s.pending = append(s.pending, m.Files...)
		if strings.TrimSpace(m.Text) != "" {
			s.caption = m.Text
		}
		if s.timer == nil || !s.timer.Stop() {
			e.wg.Add(1) // else the stopped timer hands its count over
		}
		s.timer = time.AfterFunc(e.groupDelay, func() {
			defer e.wg.Done()
			e.flushFiles(s)
		})
		e.mu.Unlock()
		return
	}
	running := s.running
	e.mu.Unlock()
	text := strings.TrimSpace(m.Text)
	if text == "" {
		return
	}
	if running {
		e.send(s.chat, "Still working on the previous command…")
		return
	}
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		e.runText(s, text)
	}()
}

// Close cancels the questions waiting for an answer and waits for the commands to end
func (e *Engine) Close() {
	e.mu.Lock()
	for _, s := range e.sessions {
		if s.answers != nil {
			select {
			case s.answers <- "cancel":
			default:
			}
		}
	}
	e.mu.Unlock()
	e.wg.Wait()
}

func (e *Engine) send(chat, text string) {
	if err := e.t.SendText(chat, text); err != nil {
		botLog("send to %s: %v", chat, err)
	}
}

// botLog: the logs of the bot (never in the conversation)
var botLogOut = os.Stderr

func botLog(format string, a ...any) {
	fmt.Fprintf(botLogOut, "%s %s\n", time.Now().Format("2006-01-02 15:04:05"), fmt.Sprintf(format, a...))
}

// commands that make no sense in a conversation
var notInChat = map[string]string{
	"init": "the setup", "setup": "the setup", "reset": "reset", "update": "updates", "upgrade": "updates",
	"completion": "the shell completion", "menu": "the interactive menu", "bot": "the bot", "login": "the login", "logout": "the logout",
}

// commandArgs: the words of a message as arguments of the cli
func commandArgs(text string) []string {
	words := shellWords(text)
	if len(words) > 0 && strings.EqualFold(words[0], "artcoded") {
		words = words[1:]
	}
	if len(words) > 0 {
		words[0] = strings.ToLower(words[0])
	}
	// --config is the one of the bot
	var out []string
	for i := 0; i < len(words); i++ {
		if words[i] == "--config" {
			i++
			continue
		}
		if strings.HasPrefix(words[i], "--config=") {
			continue
		}
		out = append(out, words[i])
	}
	return out
}

func (e *Engine) runText(s *session, text string) {
	args := commandArgs(text)
	if len(args) == 0 {
		return
	}
	switch args[0] {
	case "hi", "hello", "hey", "start", "?":
		args = []string{"help"}
	case "cancel", "stop":
		e.send(s.chat, "Nothing to cancel.")
		return
	}
	if what, ok := notInChat[args[0]]; ok {
		e.send(s.chat, fmt.Sprintf("Not here: %s is done with the cli in a terminal.", what))
		return
	}
	if args[0] == "config" && (len(args) < 2 || args[1] != "show") {
		e.send(s.chat, "Only *config show* here: the configuration is changed with the cli.")
		return
	}
	e.exec(s, func() error { return run(append([]string{"--config", e.cfgPath}, args...)) })
}

// chatWriter: what a command writes, sent as messages when it asks something or ends
type chatWriter struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (w *chatWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *chatWriter) take() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	s := w.buf.String()
	w.buf.Reset()
	return s
}

// exec runs f like a command of the conversation: its output and its questions go to the chat
func (e *Engine) exec(s *session, f func() error) {
	select {
	case e.busy <- struct{}{}:
	default:
		e.send(s.chat, "Busy with another command, try again in a moment.")
		return
	}
	e.mu.Lock()
	s.running = true
	e.mu.Unlock()
	out := &chatWriter{}
	flush := func(question bool) {
		text := chatText(out.take())
		if question {
			hint := chatHint
			if hint == "" {
				hint = ". for the default"
			}
			chatHint = ""
			text = strings.TrimRight(text, " :") + "\n" + chatStyle("↳ "+hint+" · cancel stops", "_")
		}
		if strings.TrimSpace(text) != "" {
			for _, part := range splitMessage(text, 3500) {
				e.send(s.chat, part)
			}
		}
	}
	// the global state of the cli, for the time of the command
	oldOut, oldErr, oldColor := stdout, stderr, useColor
	stdout, stderr, useColor = out, out, false
	forceChatInteractive = true
	bot = &botHooks{
		width: e.width,
		sendFile: func(name, contentType string, data []byte) error {
			flush(false)
			return e.t.SendFile(s.chat, name, contentType, data)
		},
		sendLink: func(url string) error {
			flush(false)
			return e.t.SendText(s.chat, url)
		},
	}
	lineSource = func(secret bool) (string, error) {
		if secret {
			return "", errors.New("secrets are not asked in a conversation: put it in the configuration")
		}
		// ready for the answer before the question is out
		answers := make(chan string, 1)
		e.mu.Lock()
		s.answers = answers
		e.mu.Unlock()
		flush(true)
		defer func() {
			e.mu.Lock()
			s.answers = nil
			e.mu.Unlock()
		}()
		select {
		case a := <-answers:
			a = strings.TrimSpace(a)
			switch strings.ToLower(a) {
			case "cancel", "stop", "/cancel":
				return "", errCancel
			case ".", "-", "default":
				return "", nil
			}
			return a, nil
		case <-time.After(e.answerTimeout):
			return "", fmt.Errorf("no answer for %s: cancelled", e.answerTimeout)
		}
	}
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				botLog("panic: %v\n%s", r, debug.Stack())
				err = fmt.Errorf("unexpected error: %v", r)
			}
		}()
		err = f()
	}()
	flush(false)
	stdout, stderr, useColor = oldOut, oldErr, oldColor
	forceChatInteractive = false
	bot, lineSource = nil, nil
	demoMode = false
	e.mu.Lock()
	s.running = false
	e.mu.Unlock()
	<-e.busy
	switch {
	case errors.Is(err, errCancel):
		e.send(s.chat, "Cancelled.")
	case err != nil:
		e.send(s.chat, "⚠️ "+err.Error())
	}
}

// chatText: the output of a command as a message (the commands shape it for a chat already)
func chatText(s string) string {
	s = strings.TrimRight(stripANSI(s), "\n ")
	// no more than one empty line in a row
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	return strings.TrimLeft(s, "\n")
}

// splitMessage: long outputs in several messages, cut between lines (preferably empty ones);
// a ``` block cut in two is closed and opened again
func splitMessage(s string, limit int) []string {
	if len(s) <= limit {
		return []string{s}
	}
	var parts []string
	var cur strings.Builder
	inBlock := false
	flush := func() {
		p := strings.TrimRight(cur.String(), "\n")
		if inBlock {
			p += "\n```"
		}
		if strings.TrimSpace(strings.Trim(p, "`\n")) != "" {
			parts = append(parts, p)
		}
		cur.Reset()
		if inBlock {
			cur.WriteString("```\n")
		}
	}
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		fence := strings.HasPrefix(strings.TrimSpace(line), "```")
		if fence && inBlock {
			// the end of a block stays with it
			cur.WriteString(line + "\n")
			inBlock = false
			continue
		}
		if cur.Len()+len(line)+5 > limit && cur.Len() > 0 {
			flush()
		} else if line == "" && !inBlock && cur.Len() > limit*3/4 {
			// a good place to cut: between two cards
			flush()
			continue
		}
		cur.WriteString(line)
		if i < len(lines)-1 {
			cur.WriteString("\n")
		}
		if fence {
			inBlock = true
		}
	}
	if cur.Len() > 0 {
		inBlock = false
		flush()
	}
	return parts
}

// ---------- files ----------

// what a file can be for, and the command handling it ({files}: the paths)
type fileUse struct {
	label   string
	module  string
	args    []string
	perFile bool // the command takes one file
}

var fileUses = []fileUse{
	{"an expense (subject, tag, price)", "expenses", []string{"expenses", "add"}, false},
	{"an administrative document", "documents", []string{"documents", "upload"}, true},
	{"the payment proof of an expense", "expenses", []string{"expenses", "attach", "--payment-proof"}, true},
	{"a document of a client", "clients", []string{"clients", "upload"}, true},
	{"files of an activity report", "reports", []string{"reports", "attach"}, false},
	{"just keep it (uploads)", "uploads", []string{"uploads", "upload"}, false},
}

// caption shortcuts: "expense" = expenses add...
var captionShortcuts = map[string][]string{
	"expense": {"expenses", "add"}, "expenses": {"expenses", "add"}, "fee": {"expenses", "add"},
	"document": {"documents", "upload"}, "documents": {"documents", "upload"}, "doc": {"documents", "upload"},
	"proof": {"expenses", "attach", "--payment-proof"}, "upload": {"uploads", "upload"}, "file": {"uploads", "upload"},
}

func (e *Engine) flushFiles(s *session) {
	e.mu.Lock()
	files, caption := s.pending, s.caption
	s.pending, s.caption, s.timer = nil, "", nil
	running := s.running
	e.mu.Unlock()
	if len(files) == 0 {
		return
	}
	if running {
		e.send(s.chat, "Still working on the previous command: send the files again after.")
		return
	}
	e.exec(s, func() error { return e.handleFiles(files, caption) })
}

func (e *Engine) handleFiles(files []IncomingFile, caption string) error {
	dir, err := os.MkdirTemp("", "artcoded-bot-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	var paths []string
	for i, f := range files {
		name := filepath.Base(strings.TrimSpace(f.Name))
		if name == "" || name == "." || name == "/" {
			name = fmt.Sprintf("file-%d", i+1)
		}
		p := uniquePath(filepath.Join(dir, name))
		if err := os.WriteFile(p, f.Data, 0o600); err != nil {
			return err
		}
		paths = append(paths, p)
	}
	words := commandArgs(caption)
	var use *fileUse
	var extra []string
	if len(words) > 0 {
		if sc, ok := captionShortcuts[words[0]]; ok {
			for i := range fileUses {
				if strings.Join(fileUses[i].args, " ") == strings.Join(sc, " ") {
					use = &fileUses[i]
				}
			}
			caption = strings.Join(words[1:], " ")
		} else if m := findModule(words[0]); m != nil && len(words) > 1 && m.find(words[1]) != nil {
			// a command of the cli: the files are its arguments
			use = &fileUse{args: words, perFile: false}
		}
	}
	if use == nil {
		visible := visibleModules(e.cfgPath)
		var uses []fileUse
		var labels []string
		for _, u := range fileUses {
			if visible == nil || visible[findModule(u.module)] {
				uses = append(uses, u)
				labels = append(labels, u.label)
			}
		}
		q := "What is this file for?"
		if len(files) > 1 {
			q = fmt.Sprintf("What are these %d files for?", len(files))
		}
		i, err := choose(q, labels)
		if err != nil {
			return err
		}
		use = &uses[i]
	}
	// the text of the caption (not a command): the subject / title
	if strings.TrimSpace(caption) != "" && len(use.args) >= 2 && use.label != "" {
		switch use.args[0] {
		case "expenses":
			if use.args[1] == "add" {
				extra = []string{"--subject", caption}
			}
		case "documents":
			extra = []string{"--title", caption}
		}
	}
	// --file: the commands taking an id first (attach...) do not mistake a path for it
	run1 := func(ps []string) error {
		args := append(append([]string{"--config", e.cfgPath}, use.args...), extra...)
		for _, p := range ps {
			args = append(args, "--file", p)
		}
		return run(args)
	}
	if !use.perFile {
		return run1(paths)
	}
	for _, p := range paths {
		if err := run1([]string{p}); err != nil {
			return err
		}
	}
	return nil
}

// chatHint: how to answer the question being asked (set by the prompts)
var chatHint string

// forceChatInteractive: the questions go to the conversation
var forceChatInteractive bool
