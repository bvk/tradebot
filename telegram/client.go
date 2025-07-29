// Copyright (c) 2025 BVK Chaitanya

package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/bvk/tradebot/ctxutil"
	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/kvutil"
	"github.com/bvk/tradebot/syncmap"
	"github.com/bvkgo/kv"
	"github.com/visvasity/cli"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

type CmdFunc = cli.CmdFunc

type Command struct {
	Name    string
	Purpose string
	Handler CmdFunc
}

type Client struct {
	cg ctxutil.CloseGroup

	db kv.Database

	mu sync.Mutex

	bot *bot.Bot

	self *models.User

	secrets *Secrets

	state *gobs.TelegramState

	commandMap syncmap.Map[string, *Command]
}

var start = time.Now()

func New(ctx context.Context, db kv.Database, secrets *Secrets) (_ *Client, status error) {
	if err := secrets.Check(); err != nil {
		return nil, err
	}

	c := &Client{
		db:      db,
		secrets: secrets.Clone(),
	}

	opts := []bot.Option{
		bot.WithDefaultHandler(c.handler),
	}
	bot, err := bot.New(secrets.BotToken, opts...)
	if err != nil {
		return nil, err
	}
	defer func() {
		bot.Close(ctx)
	}()
	c.bot = bot

	self, err := bot.GetMe(ctx)
	if err != nil {
		return nil, err
	}
	c.self = self

	key := path.Join("/telegram", self.Username, "state")
	state, err := kvutil.GetDB[gobs.TelegramState](ctx, db, key)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		state = &gobs.TelegramState{
			UserChatIDMap: make(map[string]int64),
		}
	}
	c.state = state

	// Configure all commands.
	c.commandMap.Store("uptime", &Command{
		Purpose: "Prints tradebot uptime",
		Handler: c.uptime,
	})
	c.commandMap.Store("version", &Command{
		Purpose: "Prints version information",
		Handler: c.version,
	})

	if ok, err := c.bot.SetMyCommands(ctx, c.commands()); err != nil {
		return nil, err
	} else if !ok {
		return nil, fmt.Errorf("could not set bot commands")
	}

	c.cg.Go(func(ctx context.Context) {
		c.bot.Start(ctx)
	})
	return c, nil
}

func (c *Client) Close() error {
	c.cg.Close()
	return nil
}

func (c *Client) BotUserName() string {
	return c.self.Username
}

func (c *Client) OwnerUserName() string {
	return c.secrets.OwnerID
}

func (c *Client) AddCommand(ctx context.Context, name, purpose string, handler CmdFunc) error {
	if len(name) == 0 || len(purpose) == 0 || handler == nil {
		return os.ErrInvalid
	}
	if _, ok := c.commandMap.Load(name); ok {
		return os.ErrExist
	}
	cdata := &Command{
		Purpose: purpose,
		Handler: handler,
	}
	if _, loaded := c.commandMap.LoadOrStore(name, cdata); loaded {
		return os.ErrExist
	}
	if ok, err := c.bot.SetMyCommands(ctx, c.commands()); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("could not set bot commands")
	}
	return nil
}

func (c *Client) commands() *bot.SetMyCommandsParams {
	c.mu.Lock()
	defer c.mu.Unlock()

	var cmds []models.BotCommand
	for cmd, cdata := range c.commandMap.Range {
		cmds = append(cmds, models.BotCommand{
			Command:     cmd,
			Description: cdata.Purpose,
		})
	}
	p := &bot.SetMyCommandsParams{
		Commands: cmds,
	}
	return p
}

func (c *Client) getCommand(update *models.Update) (string, []string, CmdFunc, error) {
	if update.Message == nil {
		return "", nil, nil, os.ErrInvalid
	}
	if len(update.Message.Entities) == 0 {
		return "", nil, nil, os.ErrInvalid
	}
	entity := update.Message.Entities[0]
	if entity.Type != models.MessageEntityTypeBotCommand {
		return "", nil, nil, os.ErrInvalid
	}
	if entity.Offset != 0 {
		return "", nil, nil, os.ErrInvalid
	}
	if update.Message.Text[0] != '/' {
		return "", nil, nil, os.ErrInvalid
	}
	cmd := update.Message.Text[1:entity.Length]
	// TODO: Handle spaces in quotes?
	args := strings.Fields(strings.TrimSpace(update.Message.Text[entity.Length:]))
	cdata, ok := c.commandMap.Load(cmd)
	if !ok {
		return cmd, nil, nil, os.ErrNotExist
	}
	return cmd, args, cdata.Handler, nil
}

func (c *Client) isValidUser(user string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if user == c.secrets.OwnerID || user == c.secrets.AdminID || slices.Contains(c.secrets.OtherIDs, user) {
		return true
	}
	return false
}

func (c *Client) saveState(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := path.Join("/telegram", c.BotUserName(), "state")
	return kvutil.SetDB(ctx, c.db, key, c.state)
}

func (c *Client) SendMessage(ctx context.Context, at time.Time, text string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	msg := at.Format("2006-01-02 15:04:05 MST") + " " + text
	slog.Info("sending notification", "at", at, "message", text)

	receivers := append([]string{c.secrets.OwnerID}, c.secrets.OtherIDs...)
	for _, receiver := range receivers {
		cid, ok := c.state.UserChatIDMap[receiver]
		if !ok {
			slog.Warn("could not notify receiver without chat id", "receiver", receiver)
			continue
		}

		m := &bot.SendMessageParams{
			ChatID: cid,
			Text:   msg,
		}
		if _, err := c.bot.SendMessage(ctx, m); err != nil {
			slog.Error("could not notify receiver (ignored)", "receiver", receiver, "err", err)
			continue
		}
	}
	return nil
}

func (c *Client) handler(ctx context.Context, bot *bot.Bot, update *models.Update) {
	if bot != c.bot {
		slog.Error("handler invoked with invalid bot value", "want", c.bot, "got", bot)
		return
	}

	sender := update.Message.From.Username
	if !c.isValidUser(sender) {
		slog.Warn("received message from non-owner (ignored)", "sender", sender, "message", update.Message.Text)
		return
	}

	if err := c.updateChatIDs(ctx, update); err != nil {
		slog.Warn("could not update chat id values (ignored)", "err", err)
	}

	if err := c.respond(ctx, update); err != nil {
		slog.Error("could not respond to user command (ignored)", "user", sender, "err", err)
		return
	}
}

func (c *Client) respond(ctx context.Context, update *models.Update) (status error) {
	True := true

	var reply string
	defer func() {
		if len(reply) != 0 {
			p := &bot.SendMessageParams{
				ChatID: update.Message.Chat.ID,
				Text:   reply,
				ReplyParameters: &models.ReplyParameters{
					MessageID: update.Message.ID,
				},
				LinkPreviewOptions: &models.LinkPreviewOptions{
					IsDisabled: &True,
				},
			}
			if _, err := c.bot.SendMessage(ctx, p); err != nil {
				status = err
			}
		}
	}()

	defer func() {
		if status != nil {
			reply = status.Error()
			status = nil
		}
	}()

	cmd, args, handler, err := c.getCommand(update)
	if err != nil {
		return err
	}

	var sb strings.Builder
	if err := handler(cli.WithStdout(ctx, &sb), args); err != nil {
		sender := update.Message.From.Username
		slog.Error("could not handle user command (ignored)", "cmd", cmd, "user", sender, "err", err)
		return err
	}

	reply = sb.String()
	return nil
}

func (c *Client) updateChatIDs(ctx context.Context, update *models.Update) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	sender := update.Message.From.Username
	if id, ok := c.state.UserChatIDMap[sender]; !ok || id != update.Message.Chat.ID {
		c.state.UserChatIDMap[sender] = update.Message.Chat.ID
		slog.Info("updating chat id message received from authorized user with chat id", "user", sender, "chat-id", update.Message.Chat.ID)

		key := path.Join("/telegram", c.BotUserName(), "state")
		if err := kvutil.SetDB(ctx, c.db, key, c.state); err != nil {
			slog.Error("could not save telegram state to the db", "err", err)
			return err
		}
	}
	return nil
}

func (c *Client) uptime(ctx context.Context, args []string) error {
	stdout := cli.Stdout(ctx)
	const day = 24 * time.Hour
	d := time.Since(start)
	if d < day {
		fmt.Fprintf(stdout, "%v", time.Since(start))
		return nil
	}
	days := d / day
	fmt.Fprintf(stdout, "%dd%v", days, d%day)
	return nil
}

func (c *Client) version(ctx context.Context, _ []string) error {
	stdout := cli.Stdout(ctx)
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return fmt.Errorf("could not read build information")
	}
	// Do not print version information for the dependencies. It can overflow the
	// Telegram size limits.
	fmt.Fprintln(stdout, "Go: ", info.GoVersion)
	fmt.Fprintln(stdout, "Binary Path: ", info.Path)
	fmt.Fprintln(stdout, "Main Module Path: ", info.Main.Path)
	fmt.Fprintln(stdout, "Main Module Version: ", info.Main.Version)
	fmt.Fprintln(stdout, "Main Module Checksum: ", info.Main.Sum)
	for _, s := range info.Settings {
		fmt.Fprintln(stdout, s.Key, ": ", s.Value)
	}
	return nil
}
