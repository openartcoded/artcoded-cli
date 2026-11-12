package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const botUsage = `artcoded bot: the cli as a WhatsApp bot

Usage:
  artcoded bot --console        try the bot in this terminal (a fake WhatsApp)
  artcoded bot                  the WhatsApp bot (needs ENABLE_WHATSAPP_BOT=true)

Environment:
  ENABLE_WHATSAPP_BOT=true      without it, the bot does not start (the console works anyway)
  WHATSAPP_ALLOWED=32470...,..  numbers allowed to talk to the bot besides yourself (comma separated)

Talk to the bot in your "message yourself" chat: help, invoices list, dashboard...
Send a file without text: the bot asks what it is for (expense, document...).
In the console:
  /file <path> [caption]        send a file (several paths: quote them, one /file per line within 3s = one batch)
  /from <number> <text>         a message from another number (to try WHATSAPP_ALLOWED)
  /quit                         stop
`

func botAllowedEnv() []string {
	return strings.FieldsFunc(os.Getenv("WHATSAPP_ALLOWED"), func(r rune) bool { return r == ',' || r == ';' || r == ' ' })
}

func runBot(c *Ctx, cfgPath string) error {
	if c.boolFlag("help") {
		fmt.Fprint(stdout, botUsage)
		return nil
	}
	if _, err := loadConfig(cfgPath); err != nil {
		return fmt.Errorf("the bot uses the configuration of the cli: %w (run 'artcoded init' first)", err)
	}
	if c.boolFlag("console") {
		return runBotConsole(cfgPath, os.Stdin, os.Stdout)
	}
	if on, _ := parseBool(os.Getenv("ENABLE_WHATSAPP_BOT")); !on {
		return errors.New("the WhatsApp bot is disabled: set ENABLE_WHATSAPP_BOT=true (or try it with: artcoded bot --console)")
	}
	return errors.New("the WhatsApp connection is not there yet: try artcoded bot --console")
}

// consoleTransport: WhatsApp in a terminal, to try the bot
type consoleTransport struct {
	mu     sync.Mutex
	out    io.Writer
	dir    string
	prefix func(string) string
}

func (t *consoleTransport) Self() string { return "console" }

func (t *consoleTransport) SendText(chat, text string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	who := "bot"
	if chat != "console" {
		who = "bot → " + chat
	}
	_, err := fmt.Fprintf(t.out, "\n%s\n%s\n", t.prefix(who+":"), text)
	return err
}

func (t *consoleTransport) SendFile(chat, name, contentType string, data []byte) error {
	if err := os.MkdirAll(t.dir, 0o755); err != nil {
		return err
	}
	p := uniquePath(filepath.Join(t.dir, filepath.Base(name)))
	if err := os.WriteFile(p, data, 0o644); err != nil {
		return err
	}
	return t.SendText(chat, fmt.Sprintf("📎 %s (%s, %s) → saved in %s", name, contentType, humanSize(float64(len(data))), p))
}

func runBotConsole(cfgPath string, in io.Reader, out io.Writer) error {
	color := useColor // the commands turn it off while they run
	t := &consoleTransport{out: out, dir: filepath.Join(os.TempDir(), "artcoded-bot-console"), prefix: func(s string) string {
		if color {
			return "\x1b[1;36m" + s + "\x1b[0m"
		}
		return s
	}}
	e := NewEngine(t, cfgPath, botAllowedEnv())
	e.groupDelay = time.Second
	defer e.Close()
	dim := func(s string) string { // not the global one: the commands change useColor while they run
		if color {
			return "\x1b[2m" + s + "\x1b[0m"
		}
		return s
	}
	red := func(s string) string { return s }
	fmt.Fprintln(out, dim("fake WhatsApp: you are in your \"message yourself\" chat. Try: help · /file <path> [caption] · /quit"))
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	n := 0
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		n++
		m := Incoming{ID: fmt.Sprint(n), Chat: "console", Sender: "console", FromMe: true, Text: line}
		switch {
		case line == "/quit" || line == "/exit":
			return nil
		case strings.HasPrefix(line, "/from "):
			parts := strings.SplitN(strings.TrimSpace(strings.TrimPrefix(line, "/from ")), " ", 2)
			m.Chat, m.Sender, m.FromMe = parts[0], parts[0], false
			m.Text = ""
			if len(parts) > 1 {
				m.Text = parts[1]
			}
			if !e.authorized(m) {
				fmt.Fprintln(out, dim("(ignored: "+parts[0]+" is not in WHATSAPP_ALLOWED)"))
				continue
			}
		case strings.HasPrefix(line, "/file "):
			words := shellWords(strings.TrimPrefix(line, "/file "))
			if len(words) == 0 {
				fmt.Fprintln(out, dim("usage: /file <path> [caption]"))
				continue
			}
			p := expandPath(words[0])
			data, err := os.ReadFile(p)
			if err != nil {
				fmt.Fprintln(out, red("! ")+err.Error())
				continue
			}
			m.Text = strings.Join(words[1:], " ")
			m.Files = []IncomingFile{{Name: filepath.Base(p), ContentType: contentTypeFor(p, data), Data: data}}
		case strings.HasPrefix(line, "/"):
			fmt.Fprintln(out, dim("console commands: /file <path> [caption] · /from <number> <text> · /quit"))
			continue
		}
		e.Handle(m)
	}
	return sc.Err()
}
