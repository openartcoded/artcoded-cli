package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver" // pure Go sqlite: the whatsapp session
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
	"rsc.io/qr"
)

// The WhatsApp side of the bot: a linked device of your account (like WhatsApp Web), paired once
// with a QR code (or a pairing code), the session kept in a sqlite file.

func waSessionPath() string {
	if p := os.Getenv("WHATSAPP_SESSION"); p != "" {
		return expandPath(p)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "whatsapp.db"
	}
	return filepath.Join(home, ".local", "share", "artcoded", "whatsapp.db")
}

func waLogger() waLog.Logger {
	level := strings.ToUpper(os.Getenv("WHATSAPP_LOG_LEVEL"))
	if level == "" {
		level = "WARN"
	}
	return waLog.Stdout("whatsapp", level, false)
}

func openWhatsApp(ctx context.Context) (*whatsmeow.Client, *sqlstore.Container, error) {
	path := waSessionPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, nil, err
	}
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)"
	container, err := sqlstore.New(ctx, "sqlite3", dsn, waLogger())
	if err != nil {
		return nil, nil, fmt.Errorf("whatsapp session %s: %w", path, err)
	}
	device, err := container.GetFirstDevice(ctx)
	if err != nil {
		container.Close()
		return nil, nil, err
	}
	return whatsmeow.NewClient(device, waLogger()), container, nil
}

// waTransport: the Transport of the engine on a whatsmeow client
type waTransport struct {
	cli     *whatsmeow.Client
	mu      sync.Mutex
	chats   map[string]types.JID // the chat key given to the engine -> the jid to answer to
	sentIDs map[types.MessageID]time.Time
	allow   func(Incoming) bool // only the files of an authorized chat are downloaded
}

func (t *waTransport) Self() string {
	if id := t.cli.Store.ID; id != nil {
		return id.User
	}
	return ""
}

// phone: the phone number of a jid (a LID, the newer anonymous ids, is looked up)
func (t *waTransport) phone(ctx context.Context, j types.JID, alt types.JID) string {
	if j.Server == types.HiddenUserServer {
		if !alt.IsEmpty() && alt.Server == types.DefaultUserServer {
			return alt.User
		}
		if lid := t.cli.Store.LID; !lid.IsEmpty() && lid.User == j.User {
			return t.Self()
		}
		if pn, err := t.cli.Store.LIDs.GetPNForLID(ctx, j); err == nil && !pn.IsEmpty() {
			return pn.User
		}
	}
	return j.User
}

func (t *waTransport) jid(chat string) (types.JID, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if j, ok := t.chats[chat]; ok {
		return j, nil
	}
	return types.ParseJID(chat + "@" + types.DefaultUserServer)
}

func (t *waTransport) send(chat string, m *waE2E.Message) error {
	to, err := t.jid(chat)
	if err != nil {
		return err
	}
	// our own answers come back as messages of the "message yourself" chat: remembered to be ignored
	id := t.cli.GenerateMessageID()
	t.mu.Lock()
	t.sentIDs[id] = time.Now()
	for k, at := range t.sentIDs {
		if time.Since(at) > time.Hour {
			delete(t.sentIDs, k)
		}
	}
	t.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	_, err = t.cli.SendMessage(ctx, to, m, whatsmeow.SendRequestExtra{ID: id})
	return err
}

func (t *waTransport) isOwn(id types.MessageID) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, ok := t.sentIDs[id]
	return ok
}

func (t *waTransport) SendText(chat, text string) error {
	return t.send(chat, &waE2E.Message{Conversation: proto.String(text)})
}

func (t *waTransport) SendFile(chat, name, contentType string, data []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	up, err := t.cli.Upload(ctx, data, whatsmeow.MediaDocument)
	if err != nil {
		return fmt.Errorf("upload %s: %w", name, err)
	}
	if contentType == "" {
		contentType = contentTypeFor(name, data)
	}
	return t.send(chat, &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{
		URL:           proto.String(up.URL),
		DirectPath:    proto.String(up.DirectPath),
		MediaKey:      up.MediaKey,
		Mimetype:      proto.String(contentType),
		FileEncSHA256: up.FileEncSHA256,
		FileSHA256:    up.FileSHA256,
		FileLength:    proto.Uint64(up.FileLength),
		FileName:      proto.String(name),
		Title:         proto.String(name),
	}})
}

// incoming: a whatsapp message as a message of the engine (nil: nothing for the bot)
func (t *waTransport) incoming(ctx context.Context, evt *events.Message) *Incoming {
	info := evt.Info
	if t.isOwn(info.ID) || evt.IsEdit || info.Chat.IsBroadcastList() || info.Chat.Server == types.NewsletterServer {
		return nil
	}
	m := &Incoming{ID: info.ID, FromMe: info.IsFromMe, Group: info.IsGroup}
	m.Sender = t.phone(ctx, info.Sender, info.SenderAlt)
	if info.IsFromMe {
		m.Chat = t.phone(ctx, info.Chat, info.RecipientAlt)
	} else {
		m.Chat = t.phone(ctx, info.Chat, info.SenderAlt)
	}
	t.mu.Lock()
	t.chats[m.Chat] = info.Chat
	t.mu.Unlock()

	msg := evt.Message
	if msg == nil {
		return nil
	}
	var media whatsmeow.DownloadableMessage
	var name, mimeType string
	switch {
	case msg.GetConversation() != "":
		m.Text = msg.GetConversation()
	case msg.GetExtendedTextMessage() != nil:
		m.Text = msg.GetExtendedTextMessage().GetText()
	case msg.GetDocumentMessage() != nil:
		d := msg.GetDocumentMessage()
		media, name, mimeType, m.Text = d, d.GetFileName(), d.GetMimetype(), d.GetCaption()
	case msg.GetDocumentWithCaptionMessage().GetMessage().GetDocumentMessage() != nil:
		d := msg.GetDocumentWithCaptionMessage().GetMessage().GetDocumentMessage()
		media, name, mimeType, m.Text = d, d.GetFileName(), d.GetMimetype(), d.GetCaption()
	case msg.GetImageMessage() != nil:
		i := msg.GetImageMessage()
		media, mimeType, m.Text = i, i.GetMimetype(), i.GetCaption()
		name = "photo-" + info.Timestamp.Format("2006-01-02-150405")
	case msg.GetVideoMessage() != nil:
		v := msg.GetVideoMessage()
		media, mimeType, m.Text = v, v.GetMimetype(), v.GetCaption()
		name = "video-" + info.Timestamp.Format("2006-01-02-150405")
	case msg.GetAudioMessage() != nil:
		a := msg.GetAudioMessage()
		media, mimeType = a, a.GetMimetype()
		name = "audio-" + info.Timestamp.Format("2006-01-02-150405")
	default:
		return nil // reactions, stickers, protocol messages...
	}
	if media != nil {
		if filepath.Ext(name) == "" {
			if exts, _ := mime.ExtensionsByType(strings.TrimSpace(strings.Split(mimeType, ";")[0])); len(exts) > 0 {
				name += exts[len(exts)-1]
			}
		}
		// only the messages of an authorized chat are downloaded
		if t.allow != nil && !t.allow(*m) {
			return nil
		}
		data, err := t.cli.Download(ctx, media)
		if err != nil {
			botLog("download %s: %v", name, err)
			m.Text = ""
			_ = t.SendText(m.Chat, "⚠️ could not download "+name+": "+err.Error())
			return nil
		}
		m.Files = []IncomingFile{{Name: name, ContentType: mimeType, Data: data}}
	}
	return m
}

const waUsage = `artcoded bot: the cli as a WhatsApp bot

Usage:
  artcoded bot                  run the bot (needs ENABLE_WHATSAPP_BOT=true); the first time, scan the QR code
  artcoded bot --phone 32470..  first time: a pairing code to type in WhatsApp instead of the QR code
  artcoded bot status           is the bot paired, with which number
  artcoded bot logout           unlink the bot from your WhatsApp
  artcoded bot --console        try the bot in this terminal (a fake WhatsApp)

Pairing: WhatsApp on your phone > Settings > Linked devices > Link a device, then scan the QR code
(also saved as whatsapp-qr.png next to the session). Then write to yourself ("message yourself"
chat): help, dashboard, invoices list... Send a file without text: the bot asks what it is for.

Environment:
  ENABLE_WHATSAPP_BOT=true      without it, the bot does not start (in a container: it waits, for docker exec)
  WHATSAPP_ALLOWED=32470...,..  numbers allowed to talk to the bot besides yourself (comma separated)
  WHATSAPP_SESSION=<file>       the session (default ~/.local/share/artcoded/whatsapp.db, /data/whatsapp.db in docker)
  WHATSAPP_LOG_LEVEL=WARN       DEBUG, INFO, WARN, ERROR

In the console:
  /file <path> [caption]        send a file (several /file within a second = one batch)
  /from <number> <text>         a message from another number (to try WHATSAPP_ALLOWED)
  /quit                         stop
`

func waStatus() error {
	if _, err := os.Stat(waSessionPath()); err != nil {
		fmt.Fprintln(stdout, "not paired (no session at "+waSessionPath()+"): run artcoded bot")
		return nil
	}
	cli, container, err := openWhatsApp(context.Background())
	if err != nil {
		return err
	}
	defer container.Close()
	if cli.Store.ID == nil {
		fmt.Fprintln(stdout, "not paired: run artcoded bot")
		return nil
	}
	fmt.Fprintf(stdout, "paired with +%s (device %d), session %s\n", cli.Store.ID.User, cli.Store.ID.Device, waSessionPath())
	return nil
}

func waLogout() error {
	ctx := context.Background()
	cli, container, err := openWhatsApp(ctx)
	if err != nil {
		return err
	}
	defer container.Close()
	if cli.Store.ID == nil {
		fmt.Fprintln(stdout, "not paired")
		return nil
	}
	if err := cli.Connect(); err != nil {
		return err
	}
	defer cli.Disconnect()
	if err := cli.Logout(ctx); err != nil {
		// unlinked from the phone already: forget the session anyway
		if derr := cli.Store.Delete(ctx); derr != nil {
			return fmt.Errorf("%v (and %v)", err, derr)
		}
	}
	info("unlinked: the bot is not a linked device of your WhatsApp anymore")
	return nil
}

// waitForever: a disabled bot in a container stays up (docker exec), without restarting in a loop
func waitForever(msg string) error {
	botLog("%s", msg)
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	<-ch
	return nil
}

func runWhatsApp(cfgPath, phone string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cli, container, err := openWhatsApp(ctx)
	if err != nil {
		return err
	}
	defer container.Close()

	t := &waTransport{cli: cli, chats: map[string]types.JID{}, sentIDs: map[types.MessageID]time.Time{}}
	allowed := botAllowedEnv()
	eng := NewEngine(t, cfgPath, allowed)
	t.allow = eng.authorized
	started := time.Now()
	cli.AddEventHandler(func(evt any) {
		switch e := evt.(type) {
		case *events.Message:
			// what was sent while the bot was down: not run hours later
			if e.Info.Timestamp.Before(started.Add(-2 * time.Minute)) {
				return
			}
			go func() {
				if m := t.incoming(ctx, e); m != nil {
					eng.Handle(*m)
				}
			}()
		case *events.Connected:
			botLog("connected as +%s, allowed besides you: %s", t.Self(), orNone(allowed))
		case *events.Disconnected:
			botLog("disconnected (reconnecting)")
		case *events.LoggedOut:
			botLog("logged out from the phone (%v): run artcoded bot again to pair", e.Reason)
			stop()
		case *events.StreamReplaced:
			botLog("another instance of the bot connected with this session: stopping")
			stop()
		case *events.PairSuccess:
			botLog("paired with +%s", e.ID.User)
			_ = os.Remove(qrPNGPath())
		}
	})

	if cli.Store.ID == nil {
		if err := waPair(ctx, cli, phone); err != nil {
			return err
		}
	} else if err := cli.Connect(); err != nil {
		return err
	}
	<-ctx.Done()
	botLog("stopping")
	cli.Disconnect()
	eng.Close()
	return nil
}

func orNone(l []string) string {
	if len(l) == 0 {
		return "nobody"
	}
	return strings.Join(l, ", ")
}

func qrPNGPath() string { return filepath.Join(filepath.Dir(waSessionPath()), "whatsapp-qr.png") }

// waPair links the bot to the account: a QR code to scan, or a pairing code with --phone
func waPair(ctx context.Context, cli *whatsmeow.Client, phone string) error {
	qrChan, err := cli.GetQRChannel(ctx)
	if err != nil {
		return err
	}
	if err := cli.Connect(); err != nil {
		return err
	}
	if phone = normalizeNumber(phone); phone != "" {
		code, err := cli.PairPhone(ctx, phone, true, whatsmeow.PairClientChrome, "Chrome (Linux)")
		if err != nil {
			return fmt.Errorf("pairing code: %w", err)
		}
		botLog("WhatsApp on +%s: tap the notification (or Linked devices > Link with phone number) and type: %s", phone, code)
	}
	for item := range qrChan {
		switch item.Event {
		case "code":
			if phone != "" {
				continue
			}
			botLog("WhatsApp > Settings > Linked devices > Link a device, and scan (valid %s):", item.Timeout.Round(time.Second))
			printQR(stderr, item.Code)
		case "success":
			return nil
		case "timeout":
			return errors.New("pairing timed out: run artcoded bot again")
		case "error":
			return fmt.Errorf("pairing: %v", item.Error)
		default:
			return fmt.Errorf("pairing: %s", item.Event)
		}
	}
	if ctx.Err() != nil {
		return nil
	}
	return errors.New("pairing ended")
}

// printQR: the code with half blocks (two rows per line), white on black whatever the terminal colors,
// and as a png next to the session
func printQR(w io.Writer, text string) {
	code, err := qr.Encode(text, qr.L)
	if err != nil {
		botLog("qr: %v", err)
		return
	}
	if err := os.WriteFile(qrPNGPath(), code.PNG(), 0o600); err == nil {
		botLog("(also in %s)", qrPNGPath())
	}
	const quiet = 2
	dark := func(x, y int) bool {
		return x >= 0 && y >= 0 && x < code.Size && y < code.Size && code.Black(x, y)
	}
	var b strings.Builder
	for y := -quiet; y < code.Size+quiet; y += 2 {
		b.WriteString("\x1b[40;97m")
		for x := -quiet; x < code.Size+quiet; x++ {
			top, bottom := !dark(x, y), !dark(x, y+1) // light modules drawn in white
			switch {
			case top && bottom:
				b.WriteString("█")
			case top:
				b.WriteString("▀")
			case bottom:
				b.WriteString("▄")
			default:
				b.WriteString(" ")
			}
		}
		b.WriteString("\x1b[0m\n")
	}
	fmt.Fprint(w, b.String())
}
