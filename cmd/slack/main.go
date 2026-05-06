package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/git-hulk/cula/internal/runtime/claudecode"
	"github.com/git-hulk/cula/internal/runtime/codex"
	"github.com/git-hulk/cula/internal/runtime/copilot"
	"github.com/git-hulk/cula/internal/runtime/hermes"
	"github.com/git-hulk/cula/internal/runtime/opencode"
	cula "github.com/git-hulk/cula/pkg"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

const (
	answerUpdateInterval   = 750 * time.Millisecond
	activityUpdateInterval = 500 * time.Millisecond
	historyMessageLimit    = 64
	maxHistoryContextLen   = 12000
	maxSlackTextLen        = 39000
	maxSlackBlockTextLen   = 2900

	actionCancelTurn = "cula_cancel_turn"
	actionRetryTurn  = "cula_retry_turn"
)

type config struct {
	botToken   string
	appToken   string
	runtime    cula.RuntimeKind
	model      string
	workingDir string
	permission cula.PermissionMode
	sandbox    cula.SandboxMode
	debug      bool
}

type bot struct {
	api       *slack.Client
	socket    *socketmode.Client
	registry  *cula.Registry
	cfg       config
	botUserID string

	mu      sync.Mutex
	threads map[string]*threadSession
}

type threadSession struct {
	mu            sync.Mutex
	session       cula.Session
	sessionID     string
	historyCursor string
	busy          bool
	lastPrompt    string
	lastEventTS   string
}

type slackTurn struct {
	channel string
	thread  string

	activityTS string
	answerTS   string

	answer             strings.Builder
	lastErr            string
	lastAnswerUpdate   time.Time
	lastActivityUpdate time.Time
	lastActivityText   string
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}

	registry := cula.NewRegistry(
		claudecode.New(cula.Config{}),
		codex.New(cula.Config{}),
		opencode.New(cula.Config{}),
		copilot.New(cula.Config{}),
		hermes.New(cula.Config{}),
	)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err = configureInteractive(ctx, cfg, registry)
	if err != nil {
		log.Fatal(err)
	}
	if _, ok := registry.Runtime(cfg.runtime); !ok {
		log.Fatalf("runtime %q is not registered", cfg.runtime)
	}

	info, err := detectRuntime(ctx, registry, cfg.runtime)
	if err != nil {
		log.Fatalf("detect %s: %v", cfg.runtime, err)
	}
	if !info.Installed {
		log.Fatalf("runtime %s is not installed", cfg.runtime)
	}

	api := slack.New(
		cfg.botToken,
		slack.OptionAppLevelToken(cfg.appToken),
		slack.OptionDebug(cfg.debug),
	)
	auth, err := api.AuthTest()
	if err != nil {
		log.Fatalf("slack auth test: %v", err)
	}

	b := &bot{
		api:       api,
		socket:    socketmode.New(api, socketmode.OptionDebug(cfg.debug)),
		registry:  registry,
		cfg:       cfg,
		botUserID: auth.UserID,
		threads:   make(map[string]*threadSession),
	}

	log.Printf("cula Slack bot running as %s with runtime=%s model=%s working_dir=%s", auth.User, cfg.runtime, valueOr(cfg.model, "default"), cfg.workingDir)
	if cfg.debug {
		log.Printf("debug logging enabled")
	}
	go b.handleSocketEvents(ctx)
	if err := b.socket.RunContext(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal(err)
	}
}

func loadConfig() (config, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return config{}, fmt.Errorf("getwd: %w", err)
	}

	cfg := config{
		botToken:   strings.TrimSpace(os.Getenv("SLACK_BOT_TOKEN")),
		appToken:   strings.TrimSpace(os.Getenv("SLACK_APP_TOKEN")),
		runtime:    cula.RuntimeCodex,
		model:      strings.TrimSpace(os.Getenv("CULA_MODEL")),
		workingDir: valueOr(strings.TrimSpace(os.Getenv("CULA_WORKDIR")), cwd),
		permission: cula.PermissionMode(strings.TrimSpace(os.Getenv("CULA_PERMISSION"))),
		sandbox:    cula.SandboxMode(strings.TrimSpace(os.Getenv("CULA_SANDBOX"))),
		debug:      strings.EqualFold(strings.TrimSpace(os.Getenv("CULA_SLACK_DEBUG")), "true"),
	}
	if cfg.botToken == "" {
		return config{}, fmt.Errorf("SLACK_BOT_TOKEN is required")
	}
	if cfg.appToken == "" {
		return config{}, fmt.Errorf("SLACK_APP_TOKEN is required")
	}
	if rt := strings.TrimSpace(os.Getenv("CULA_RUNTIME")); rt != "" {
		cfg.runtime = cula.RuntimeKind(rt)
	}
	switch cfg.runtime {
	case cula.RuntimeClaudeCode, cula.RuntimeCodex, cula.RuntimeOpenCode, cula.RuntimeCopilot, cula.RuntimeHermes:
	default:
		return config{}, fmt.Errorf("unsupported CULA_RUNTIME %q", cfg.runtime)
	}
	switch cfg.permission {
	case cula.PermissionDefault, cula.PermissionNever, cula.PermissionSkip:
	default:
		return config{}, fmt.Errorf("unsupported CULA_PERMISSION %q", cfg.permission)
	}
	switch cfg.sandbox {
	case cula.SandboxDefault, cula.SandboxWorkspaceWrite, cula.SandboxDangerFullAccess:
	default:
		return config{}, fmt.Errorf("unsupported CULA_SANDBOX %q", cfg.sandbox)
	}
	return cfg, nil
}

func detectRuntime(ctx context.Context, registry *cula.Registry, kind cula.RuntimeKind) (cula.RuntimeInfo, error) {
	rt, ok := registry.Runtime(kind)
	if !ok {
		return cula.RuntimeInfo{}, fmt.Errorf("runtime %q is not registered", kind)
	}
	return rt.Detect(ctx)
}

func (b *bot) handleSocketEvents(ctx context.Context) {
	for {
		select {
		case evt, ok := <-b.socket.Events:
			if !ok {
				log.Printf("slack socket events channel closed")
				return
			}
			b.handleSocketEvent(ctx, evt)
		case <-ctx.Done():
			log.Printf("slack socket event handler stopped: %v", ctx.Err())
			return
		}
	}
}

func (b *bot) handleSocketEvent(ctx context.Context, evt socketmode.Event) {
	b.debugf("socket event received type=%s request=%t data_type=%T", evt.Type, evt.Request != nil, evt.Data)
	switch evt.Type {
	case socketmode.EventTypeEventsAPI:
		if evt.Request != nil {
			b.debugf("acking events api request envelope_id=%s retry_attempt=%d retry_reason=%s", evt.Request.EnvelopeID, evt.Request.RetryAttempt, evt.Request.RetryReason)
			b.socket.Ack(*evt.Request)
		} else {
			b.debugf("events api socket event has no request to ack")
		}
		eventsAPIEvent, ok := evt.Data.(slackevents.EventsAPIEvent)
		if !ok || eventsAPIEvent.Type != slackevents.CallbackEvent {
			b.debugf("dropping events api payload: ok=%t outer_type=%s data_type=%T", ok, eventsAPIEvent.Type, evt.Data)
			return
		}
		b.debugf(
			"events api callback received team=%s app=%s inner_type=%s inner_data_type=%T%s",
			eventsAPIEvent.TeamID,
			eventsAPIEvent.APIAppID,
			eventsAPIEvent.InnerEvent.Type,
			eventsAPIEvent.InnerEvent.Data,
			callbackDebugSuffix(eventsAPIEvent.Data),
		)
		switch ev := eventsAPIEvent.InnerEvent.Data.(type) {
		case *slackevents.AppMentionEvent:
			b.handleAppMention(ctx, ev)
		case *slackevents.MessageEvent:
			b.handleMessage(ctx, ev)
		default:
			b.debugf("dropping unhandled inner event type=%s data_type=%T", eventsAPIEvent.InnerEvent.Type, eventsAPIEvent.InnerEvent.Data)
		}
	case socketmode.EventTypeInteractive:
		if evt.Request != nil {
			b.debugf("acking interactive request envelope_id=%s", evt.Request.EnvelopeID)
			b.socket.Ack(*evt.Request)
		} else {
			b.debugf("interactive socket event has no request to ack")
		}
		cb, ok := evt.Data.(slack.InteractionCallback)
		if !ok {
			b.debugf("dropping interactive payload: data_type=%T", evt.Data)
			return
		}
		b.handleInteractive(ctx, cb)
	default:
		b.debugf("ignoring socket event type=%s", evt.Type)
	}
}

func (b *bot) handleAppMention(ctx context.Context, ev *slackevents.AppMentionEvent) {
	b.debugf(
		"app_mention received channel=%s user=%s ts=%s thread_ts=%s bot_id=%s text_len=%d",
		ev.Channel,
		ev.User,
		ev.TimeStamp,
		ev.ThreadTimeStamp,
		ev.BotID,
		runeLen(ev.Text),
	)
	if ev.User == "" {
		b.debugf("dropping app_mention: missing user channel=%s ts=%s", ev.Channel, ev.TimeStamp)
		return
	}
	if ev.User == b.botUserID {
		b.debugf("dropping app_mention: sent by this bot user=%s channel=%s ts=%s", ev.User, ev.Channel, ev.TimeStamp)
		return
	}
	prompt := cleanPrompt(ev.Text, b.botUserID)
	if prompt == "" {
		b.debugf("dropping app_mention: prompt empty after removing bot mention channel=%s ts=%s", ev.Channel, ev.TimeStamp)
		return
	}
	thread := valueOr(ev.ThreadTimeStamp, ev.TimeStamp)
	b.debugf("starting turn from app_mention channel=%s thread=%s prompt_len=%d", ev.Channel, thread, runeLen(prompt))
	go b.runTurn(ctx, ev.Channel, thread, ev.TimeStamp, prompt, true)
}

func (b *bot) handleMessage(ctx context.Context, ev *slackevents.MessageEvent) {
	b.debugf(
		"message received channel=%s channel_type=%s user=%s bot_id=%s subtype=%s ts=%s thread_ts=%s text_len=%d",
		ev.Channel,
		ev.ChannelType,
		ev.User,
		ev.BotID,
		ev.SubType,
		ev.TimeStamp,
		ev.ThreadTimeStamp,
		runeLen(ev.Text),
	)
	if ev.User == "" {
		b.debugf("dropping message: missing user channel=%s ts=%s", ev.Channel, ev.TimeStamp)
		return
	}
	if ev.User == b.botUserID {
		b.debugf("dropping message: sent by this bot user=%s channel=%s ts=%s", ev.User, ev.Channel, ev.TimeStamp)
		return
	}
	if ev.BotID != "" {
		b.debugf("dropping message: bot message bot_id=%s channel=%s ts=%s", ev.BotID, ev.Channel, ev.TimeStamp)
		return
	}
	if ev.SubType != "" {
		b.debugf("dropping message: unsupported subtype=%s channel=%s ts=%s", ev.SubType, ev.Channel, ev.TimeStamp)
		return
	}
	if ev.ChannelType != "im" {
		b.debugf("dropping message: unsupported channel_type=%s channel=%s ts=%s", ev.ChannelType, ev.Channel, ev.TimeStamp)
		return
	}
	prompt := strings.TrimSpace(ev.Text)
	if prompt == "" {
		b.debugf("dropping message: prompt empty channel=%s ts=%s", ev.Channel, ev.TimeStamp)
		return
	}
	thread := valueOr(ev.ThreadTimeStamp, ev.TimeStamp)
	b.debugf("starting turn from direct message channel=%s thread=%s prompt_len=%d", ev.Channel, thread, runeLen(prompt))
	go b.runTurn(ctx, ev.Channel, thread, ev.TimeStamp, prompt, false)
}

func (b *bot) runTurn(ctx context.Context, channel, thread, eventTS, prompt string, includeSlackHistory bool) {
	b.debugf("turn started channel=%s thread=%s prompt_len=%d", channel, thread, runeLen(prompt))
	ts := b.thread(channel, thread)
	ts.mu.Lock()
	if ts.busy {
		ts.mu.Unlock()
		b.debugf("turn rejected because thread is busy channel=%s thread=%s", channel, thread)
		_, _, err := b.api.PostMessage(
			channel,
			slack.MsgOptionText("Still working on the previous request in this thread.", false),
			slack.MsgOptionBlocks(statusBlocks(":hourglass_flowing_sand: *Still working*\nThe previous request in this thread is not finished yet.", "", "")...),
			slack.MsgOptionTS(thread),
			slack.MsgOptionDisableLinkUnfurl(),
			slack.MsgOptionDisableMediaUnfurl(),
		)
		logSlackError("post busy message", err)
		return
	}
	ts.busy = true
	ts.lastPrompt = prompt
	ts.lastEventTS = eventTS
	sess := ts.session
	sessionID := ts.sessionID
	historyCursor := ts.historyCursor
	ts.mu.Unlock()

	turn := &slackTurn{channel: channel, thread: thread}
	if err := b.postActivity(turn, thinkingActivityText()); err != nil {
		logSlackError("post activity", err)
	}

	defer func() {
		ts.mu.Lock()
		ts.busy = false
		ts.mu.Unlock()
		b.debugf("turn marked idle channel=%s thread=%s", channel, thread)
	}()

	if sess == nil {
		var err error
		b.debugf("spawning runtime session runtime=%s model=%s working_dir=%s previous_session_id=%s", b.cfg.runtime, valueOr(b.cfg.model, "default"), b.cfg.workingDir, sessionID)
		sess, err = b.registry.SpawnSession(ctx, cula.SessionInput{
			Runtime:    b.cfg.runtime,
			Model:      b.cfg.model,
			WorkingDir: b.cfg.workingDir,
			SessionID:  sessionID,
			Permission: b.cfg.permission,
			Sandbox:    b.cfg.sandbox,
		})
		if err != nil {
			b.debugf("spawn session failed channel=%s thread=%s err=%v", channel, thread, err)
			turn.lastErr = fmt.Sprintf("start session: %v", err)
			b.flushFinal(turn)
			b.deleteActivity(turn)
			return
		}
		b.debugf("runtime session spawned channel=%s thread=%s", channel, thread)
		ts.mu.Lock()
		ts.session = sess
		ts.mu.Unlock()
	} else {
		b.debugf("reusing runtime session channel=%s thread=%s session_id=%s", channel, thread, sessionID)
	}

	runtimePrompt := prompt
	nextHistoryCursor := historyCursor
	historyCursorReady := false
	if includeSlackHistory {
		historyPrompt, nextCursor, err := b.promptWithSlackHistory(channel, thread, eventTS, historyCursor, prompt)
		if err != nil {
			logSlackError("read slack history", err)
			b.debugf("continuing without slack history channel=%s thread=%s cursor=%s err=%v", channel, thread, historyCursor, err)
		} else {
			runtimePrompt = historyPrompt
			nextHistoryCursor = nextCursor
			historyCursorReady = true
		}
	}

	b.debugf("sending prompt to runtime channel=%s thread=%s prompt_len=%d", channel, thread, runeLen(runtimePrompt))
	if err := sess.Send(ctx, runtimePrompt); err != nil {
		b.debugf("send prompt failed channel=%s thread=%s err=%v", channel, thread, err)
		turn.lastErr = fmt.Sprintf("send prompt: %v", err)
		b.clearSession(ts)
		b.flushFinal(turn)
		b.deleteActivity(turn)
		return
	}
	b.debugf("prompt sent to runtime channel=%s thread=%s", channel, thread)
	if historyCursorReady {
		ts.mu.Lock()
		ts.historyCursor = nextHistoryCursor
		ts.mu.Unlock()
		b.debugf("slack history cursor advanced channel=%s thread=%s previous=%s next=%s", channel, thread, historyCursor, nextHistoryCursor)
	}

	b.consumeTurn(ctx, ts, turn, sess)
}

func (b *bot) consumeTurn(ctx context.Context, ts *threadSession, turn *slackTurn, sess cula.Session) {
	for {
		select {
		case ev, ok := <-sess.Events():
			if !ok {
				b.debugf("runtime events channel closed channel=%s thread=%s answer_len=%d last_err=%t", turn.channel, turn.thread, runeLen(turn.answer.String()), turn.lastErr != "")
				b.clearSession(ts)
				if strings.TrimSpace(turn.answer.String()) == "" && turn.lastErr == "" {
					turn.lastErr = "session ended before returning a final answer"
				}
				b.flushFinal(turn)
				b.deleteActivity(turn)
				return
			}
			if ev.SessionID != "" {
				ts.mu.Lock()
				ts.sessionID = ev.SessionID
				ts.mu.Unlock()
				b.debugf("runtime session id observed channel=%s thread=%s session_id=%s", turn.channel, turn.thread, ev.SessionID)
			}
			b.debugf("runtime event channel=%s thread=%s %s", turn.channel, turn.thread, eventDebugSummary(ev))
			if b.handleEvent(turn, ev) {
				b.debugf("runtime turn complete channel=%s thread=%s answer_len=%d last_err=%t", turn.channel, turn.thread, runeLen(turn.answer.String()), turn.lastErr != "")
				b.flushFinal(turn)
				b.deleteActivity(turn)
				return
			}
		case <-ctx.Done():
			b.debugf("turn context canceled channel=%s thread=%s err=%v", turn.channel, turn.thread, ctx.Err())
			_ = sess.Cancel(context.Background())
			b.deleteActivity(turn)
			return
		}
	}
}

func (b *bot) handleEvent(turn *slackTurn, ev cula.Event) bool {
	switch ev.Type {
	case cula.EventText:
		if ev.Text != "" {
			turn.answer.WriteString(ev.Text)
			b.updateAnswer(turn, false)
		}
	case cula.EventActivity:
		if ev.Activity != nil {
			b.updateActivity(turn, renderActivity(ev.Activity), false)
		}
	case cula.EventToolCall:
		if ev.ToolCall != nil {
			b.updateActivity(turn, toolActivityText(ev.ToolCall.Name), false)
		}
	case cula.EventToolResult:
		b.updateActivity(turn, ":repeat: *Finishing tool work*\nReading the tool result and continuing.", false)
	case cula.EventStderr:
		if ev.Error != "" {
			b.updateActivity(turn, ":warning: *Runtime output*\n"+codeBlock(ev.Error), false)
		}
	case cula.EventError:
		if ev.Error != "" {
			turn.lastErr = ev.Error
			b.updateActivity(turn, ":x: *Error*\n"+ev.Error, true)
		}
	case cula.EventDone:
		return true
	case cula.EventState:
		switch ev.State {
		case cula.StateCompleted:
			return true
		case cula.StateFailed:
			if turn.lastErr == "" {
				turn.lastErr = "runtime failed"
			}
			return true
		case cula.StateCanceled:
			if turn.lastErr == "" {
				turn.lastErr = "runtime canceled"
			}
			return true
		}
	}
	return false
}

func (b *bot) thread(channel, thread string) *threadSession {
	key := channel + "\x00" + thread
	b.mu.Lock()
	defer b.mu.Unlock()
	ts := b.threads[key]
	if ts == nil {
		ts = &threadSession{}
		b.threads[key] = ts
		b.debugf("created thread session channel=%s thread=%s", channel, thread)
	}
	return ts
}

func (b *bot) lookupThread(channel, thread string) *threadSession {
	key := channel + "\x00" + thread
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.threads[key]
}

func (b *bot) handleInteractive(ctx context.Context, cb slack.InteractionCallback) {
	if cb.Type != slack.InteractionTypeBlockActions {
		b.debugf("ignoring interactive callback type=%s", cb.Type)
		return
	}
	for _, action := range cb.ActionCallback.BlockActions {
		if action == nil {
			continue
		}
		channel, thread, ok := decodeThreadValue(action.Value)
		if !ok {
			b.debugf("invalid thread value action_id=%s value=%q", action.ActionID, action.Value)
			continue
		}
		switch action.ActionID {
		case actionCancelTurn:
			b.cancelTurn(channel, thread, cb.User.ID)
		case actionRetryTurn:
			go b.retryTurn(ctx, channel, thread, cb.User.ID)
		default:
			b.debugf("ignoring action_id=%s block_id=%s", action.ActionID, action.BlockID)
		}
	}
}

func (b *bot) cancelTurn(channel, thread, userID string) {
	ts := b.lookupThread(channel, thread)
	if ts == nil {
		b.debugf("cancel ignored: no thread session channel=%s thread=%s", channel, thread)
		return
	}
	ts.mu.Lock()
	sess := ts.session
	busy := ts.busy
	ts.mu.Unlock()
	if !busy || sess == nil {
		b.debugf("cancel ignored: idle channel=%s thread=%s", channel, thread)
		return
	}
	b.debugf("cancel requested channel=%s thread=%s user=%s", channel, thread, userID)
	if err := sess.Cancel(context.Background()); err != nil {
		logSlackError("cancel session", err)
	}
}

func (b *bot) retryTurn(ctx context.Context, channel, thread, userID string) {
	ts := b.lookupThread(channel, thread)
	if ts == nil {
		b.debugf("retry ignored: no thread session channel=%s thread=%s", channel, thread)
		return
	}
	ts.mu.Lock()
	prompt := ts.lastPrompt
	eventTS := ts.lastEventTS
	ts.mu.Unlock()
	if prompt == "" {
		b.debugf("retry ignored: no prompt to replay channel=%s thread=%s", channel, thread)
		return
	}
	b.debugf("retry requested channel=%s thread=%s user=%s prompt_len=%d", channel, thread, userID, runeLen(prompt))
	b.runTurn(ctx, channel, thread, eventTS, prompt, false)
}

func encodeThreadValue(channel, thread string) string {
	return channel + "|" + thread
}

func decodeThreadValue(value string) (string, string, bool) {
	idx := strings.IndexByte(value, '|')
	if idx <= 0 || idx == len(value)-1 {
		return "", "", false
	}
	return value[:idx], value[idx+1:], true
}

func (b *bot) clearSession(ts *threadSession) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.session = nil
	b.debugf("cleared runtime session")
}

func (b *bot) promptWithSlackHistory(channel, thread, eventTS, cursor, prompt string) (string, string, error) {
	messages, err := b.readSlackHistory(channel, thread, eventTS, cursor)
	if err != nil {
		return prompt, cursor, err
	}
	nextCursor := eventTS
	if nextCursor == "" {
		nextCursor = latestMessageTimestamp(messages, cursor)
	}
	context := renderSlackHistoryContext(messages, eventTS, b.botUserID)
	if context == "" {
		return prompt, nextCursor, nil
	}
	return fmt.Sprintf(
		"Slack conversation context from human messages since the last cursor. Use it as background, but answer the current user request.\n\n%s\n\nCurrent user request:\n%s",
		context,
		prompt,
	), nextCursor, nil
}

func (b *bot) readSlackHistory(channel, thread, eventTS, cursor string) ([]slack.Message, error) {
	if thread != "" && thread != eventTS {
		params := &slack.GetConversationRepliesParameters{
			ChannelID: channel,
			Timestamp: thread,
			Inclusive: cursor == "",
			Latest:    eventTS,
			Limit:     historyMessageLimit,
		}
		if cursor != "" {
			params.Oldest = cursor
		}
		messages, _, _, err := b.api.GetConversationReplies(params)
		if err != nil {
			return nil, fmt.Errorf("conversation replies: %w", err)
		}
		b.debugf("read slack thread history channel=%s thread=%s cursor=%s messages=%d", channel, thread, cursor, len(messages))
		return chronologicalMessages(messages), nil
	}

	params := &slack.GetConversationHistoryParameters{
		ChannelID: channel,
		Inclusive: cursor == "",
		Latest:    eventTS,
		Limit:     historyMessageLimit,
	}
	if cursor != "" {
		params.Oldest = cursor
	}
	resp, err := b.api.GetConversationHistory(params)
	if err != nil {
		return nil, fmt.Errorf("conversation history: %w", err)
	}
	b.debugf("read slack channel history channel=%s cursor=%s messages=%d", channel, cursor, len(resp.Messages))
	return chronologicalMessages(resp.Messages), nil
}

func (b *bot) postActivity(turn *slackTurn, text string) error {
	text = limitSlackText(text)
	_, ts, err := b.api.PostMessage(
		turn.channel,
		slack.MsgOptionText(activityFallback(text), false),
		slack.MsgOptionBlocks(statusBlocks(text, turn.channel, turn.thread)...),
		slack.MsgOptionTS(turn.thread),
		slack.MsgOptionDisableLinkUnfurl(),
		slack.MsgOptionDisableMediaUnfurl(),
	)
	if err != nil {
		return err
	}
	turn.activityTS = ts
	turn.lastActivityText = text
	turn.lastActivityUpdate = time.Now()
	b.debugf("posted activity message channel=%s thread=%s ts=%s text_len=%d", turn.channel, turn.thread, turn.activityTS, runeLen(text))
	return nil
}

func (b *bot) updateActivity(turn *slackTurn, text string, force bool) {
	text = limitSlackText(strings.TrimSpace(text))
	if text == "" || turn.activityTS == "" {
		if turn.activityTS == "" {
			b.debugf("skipping activity update: no activity message channel=%s thread=%s", turn.channel, turn.thread)
		}
		return
	}
	if !force && text == turn.lastActivityText {
		return
	}
	if !force && time.Since(turn.lastActivityUpdate) < activityUpdateInterval {
		return
	}
	_, _, _, err := b.api.UpdateMessage(
		turn.channel,
		turn.activityTS,
		slack.MsgOptionText(activityFallback(text), false),
		slack.MsgOptionBlocks(statusBlocks(text, turn.channel, turn.thread)...),
		slack.MsgOptionDisableLinkUnfurl(),
		slack.MsgOptionDisableMediaUnfurl(),
	)
	if err != nil {
		logSlackError("update activity", err)
		return
	}
	turn.lastActivityText = text
	turn.lastActivityUpdate = time.Now()
	b.debugf("updated activity message channel=%s thread=%s ts=%s text_len=%d", turn.channel, turn.thread, turn.activityTS, runeLen(text))
}

func (b *bot) deleteActivity(turn *slackTurn) {
	if turn.activityTS == "" {
		return
	}
	_, _, err := b.api.DeleteMessage(turn.channel, turn.activityTS)
	logSlackError("delete activity", err)
	if err == nil {
		b.debugf("deleted activity message channel=%s thread=%s ts=%s", turn.channel, turn.thread, turn.activityTS)
	}
	turn.activityTS = ""
}

func (b *bot) updateAnswer(turn *slackTurn, force bool) {
	text := strings.TrimSpace(turn.answer.String())
	if text == "" {
		return
	}
	if !force && time.Since(turn.lastAnswerUpdate) < answerUpdateInterval {
		return
	}
	b.upsertAnswer(turn, limitSlackText(text))
}

func (b *bot) flushFinal(turn *slackTurn) {
	text := strings.TrimSpace(turn.answer.String())
	if text == "" && turn.lastErr != "" {
		text = "Error: " + turn.lastErr
	}
	if text == "" {
		text = "Done."
	}
	b.upsertAnswer(turn, limitSlackText(text))
}

func (b *bot) upsertAnswer(turn *slackTurn, text string) {
	var err error
	if turn.answerTS == "" {
		_, turn.answerTS, err = b.api.PostMessage(
			turn.channel,
			slack.MsgOptionText(text, false),
			slack.MsgOptionBlocks(b.answerBlocks(text, turn.channel, turn.thread)...),
			slack.MsgOptionTS(turn.thread),
			slack.MsgOptionDisableLinkUnfurl(),
			slack.MsgOptionDisableMediaUnfurl(),
		)
		logSlackError("post answer", err)
		if err == nil {
			b.debugf("posted answer message channel=%s thread=%s ts=%s text_len=%d", turn.channel, turn.thread, turn.answerTS, runeLen(text))
		}
	} else {
		_, _, _, err = b.api.UpdateMessage(
			turn.channel,
			turn.answerTS,
			slack.MsgOptionText(text, false),
			slack.MsgOptionBlocks(b.answerBlocks(text, turn.channel, turn.thread)...),
			slack.MsgOptionDisableLinkUnfurl(),
			slack.MsgOptionDisableMediaUnfurl(),
		)
		logSlackError("update answer", err)
		if err == nil {
			b.debugf("updated answer message channel=%s thread=%s ts=%s text_len=%d", turn.channel, turn.thread, turn.answerTS, runeLen(text))
		}
	}
	if err == nil {
		turn.lastAnswerUpdate = time.Now()
	}
}

func renderActivity(activity *cula.Activity) string {
	params := strings.TrimSpace(strings.Join(activity.Parameters, " "))
	switch activity.Type {
	case cula.ActivityThinking:
		return thinkingActivityText()
	case cula.ActivityCommand:
		if params == "" {
			return ":terminal: *Running command*\nWaiting for command output."
		}
		return ":terminal: *Running command*\n" + codeBlock(params)
	case cula.ActivityToolCall:
		if params == "" {
			return toolActivityText("")
		}
		return toolActivityText(params)
	case cula.ActivityNarration:
		if params == "" {
			return ":large_blue_circle: *Working*\nProcessing the request."
		}
		return ":speech_balloon: *Progress update*\n" + params
	default:
		if params == "" {
			return ":large_blue_circle: *Working*\nProcessing the request."
		}
		return params
	}
}

func thinkingActivityText() string {
	return ":thought_balloon: *Thinking*\nReasoning through the request."
}

func toolActivityText(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ":hammer_and_wrench: *Using tool*\nWaiting for the tool to respond."
	}
	return ":hammer_and_wrench: *Using tool*\n`" + inlineCode(name) + "`"
}

func statusBlocks(text, cancelChannel, cancelThread string) []slack.Block {
	blocks := []slack.Block{
		slack.NewContextBlock("", slackMrkdwn(":robot_face: *Cula status*")),
		slack.NewSectionBlock(slackMrkdwn(limitSlackBlockText(text)), nil, nil, slack.SectionBlockOptionExpand(true)),
	}
	if cancelChannel != "" && cancelThread != "" {
		btn := slack.NewButtonBlockElement(
			actionCancelTurn,
			encodeThreadValue(cancelChannel, cancelThread),
			slack.NewTextBlockObject(slack.PlainTextType, "Cancel", false, false),
		)
		btn.Style = slack.StyleDanger
		blocks = append(blocks, slack.NewActionBlock("cula_status_actions", btn))
	}
	return blocks
}

func (b *bot) answerBlocks(text, retryChannel, retryThread string) []slack.Block {
	blocks := []slack.Block{
		slack.NewContextBlock("", slackMrkdwn(fmt.Sprintf(":robot_face: *Cula* | %s | %s", b.cfg.runtime, valueOr(b.cfg.model, "default")))),
	}
	for _, chunk := range splitSlackBlockText(text) {
		blocks = append(blocks, slack.NewSectionBlock(slackMrkdwn(chunk), nil, nil, slack.SectionBlockOptionExpand(true)))
	}
	if retryChannel != "" && retryThread != "" {
		btn := slack.NewButtonBlockElement(
			actionRetryTurn,
			encodeThreadValue(retryChannel, retryThread),
			slack.NewTextBlockObject(slack.PlainTextType, "Retry", false, false),
		)
		blocks = append(blocks, slack.NewActionBlock("cula_answer_actions", btn))
	}
	return blocks
}

func activityFallback(text string) string {
	text = strings.TrimSpace(stripSlackMarkup(text))
	if text == "" {
		return "Cula is working..."
	}
	return text
}

func slackMrkdwn(text string) *slack.TextBlockObject {
	return slack.NewTextBlockObject(slack.MarkdownType, text, false, false)
}

func splitSlackBlockText(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return []string{"Done."}
	}

	var chunks []string
	for len([]rune(text)) > maxSlackBlockTextLen {
		runes := []rune(text)
		cut := maxSlackBlockTextLen
		for i := cut; i > maxSlackBlockTextLen/2; i-- {
			if runes[i] == '\n' {
				cut = i
				break
			}
		}
		if cut == maxSlackBlockTextLen {
			for i := cut; i > maxSlackBlockTextLen/2; i-- {
				if runes[i] == ' ' {
					cut = i
					break
				}
			}
		}
		chunks = append(chunks, strings.TrimSpace(string(runes[:cut])))
		text = strings.TrimSpace(string(runes[cut:]))
	}
	if text != "" {
		chunks = append(chunks, text)
	}
	return chunks
}

func limitSlackBlockText(text string) string {
	chunks := splitSlackBlockText(text)
	if len(chunks) == 0 {
		return "Working..."
	}
	return chunks[0]
}

func codeBlock(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return "```\n \n```"
	}
	return "```\n" + strings.ReplaceAll(text, "```", "'''") + "\n```"
}

func inlineCode(text string) string {
	return strings.ReplaceAll(strings.TrimSpace(text), "`", "'")
}

func stripSlackMarkup(text string) string {
	replacer := strings.NewReplacer(
		"*", "",
		"`", "",
		"_", "",
		":thought_balloon:", "",
		":hourglass_flowing_sand:", "",
		":hammer_and_wrench:", "",
		":terminal:", "",
		":repeat:", "",
		":warning:", "",
		":x:", "",
		":large_blue_circle:", "",
		":speech_balloon:", "",
		":robot_face:", "",
	)
	return replacer.Replace(text)
}

func cleanPrompt(text, botUserID string) string {
	text = strings.ReplaceAll(text, "<@"+botUserID+">", "")
	return strings.TrimSpace(text)
}

func chronologicalMessages(messages []slack.Message) []slack.Message {
	ordered := append([]slack.Message(nil), messages...)
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].Timestamp < ordered[j].Timestamp
	})
	return ordered
}

func renderSlackHistoryContext(messages []slack.Message, currentTS, botUserID string) string {
	var lines []string
	for _, msg := range messages {
		if !isHumanSlackMessage(msg, botUserID) || msg.Timestamp == currentTS {
			continue
		}
		text := cleanPrompt(msg.Text, botUserID)
		if text == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("- %s <@%s>: %s", msg.Timestamp, msg.User, text))
	}
	return limitHistoryContext(strings.Join(lines, "\n"))
}

func isHumanSlackMessage(msg slack.Message, botUserID string) bool {
	return msg.User != "" &&
		msg.User != botUserID &&
		msg.BotID == "" &&
		msg.SubType == "" &&
		strings.TrimSpace(msg.Text) != ""
}

func latestMessageTimestamp(messages []slack.Message, fallback string) string {
	latest := fallback
	for _, msg := range messages {
		if msg.Timestamp > latest {
			latest = msg.Timestamp
		}
	}
	return latest
}

func limitHistoryContext(text string) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= maxHistoryContextLen {
		return string(runes)
	}
	return "[earlier Slack context truncated]\n" + string(runes[len(runes)-maxHistoryContextLen:])
}

func limitSlackText(text string) string {
	runes := []rune(text)
	if len(runes) <= maxSlackTextLen {
		return text
	}
	return string(runes[:maxSlackTextLen]) + "\n\n[truncated]"
}

func valueOr(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func runeLen(text string) int {
	return len([]rune(text))
}

func callbackDebugSuffix(data any) string {
	switch ev := data.(type) {
	case slackevents.EventsAPICallbackEvent:
		return fmt.Sprintf(" event_id=%s event_time=%d authed_users=%d", ev.EventID, ev.EventTime, len(ev.AuthedUsers))
	case *slackevents.EventsAPICallbackEvent:
		return fmt.Sprintf(" event_id=%s event_time=%d authed_users=%d", ev.EventID, ev.EventTime, len(ev.AuthedUsers))
	default:
		return fmt.Sprintf(" callback_data_type=%T", data)
	}
}

func eventDebugSummary(ev cula.Event) string {
	switch ev.Type {
	case cula.EventText:
		return fmt.Sprintf("type=%s runtime=%s session_id=%s text_len=%d", ev.Type, ev.Runtime, ev.SessionID, runeLen(ev.Text))
	case cula.EventActivity:
		if ev.Activity == nil {
			return fmt.Sprintf("type=%s runtime=%s session_id=%s activity=<nil>", ev.Type, ev.Runtime, ev.SessionID)
		}
		return fmt.Sprintf("type=%s runtime=%s session_id=%s activity_type=%s params=%d", ev.Type, ev.Runtime, ev.SessionID, ev.Activity.Type, len(ev.Activity.Parameters))
	case cula.EventToolCall:
		if ev.ToolCall == nil {
			return fmt.Sprintf("type=%s runtime=%s session_id=%s tool=<nil>", ev.Type, ev.Runtime, ev.SessionID)
		}
		return fmt.Sprintf("type=%s runtime=%s session_id=%s tool_name=%s tool_id=%s", ev.Type, ev.Runtime, ev.SessionID, ev.ToolCall.Name, ev.ToolCall.ID)
	case cula.EventToolResult:
		if ev.ToolResult == nil {
			return fmt.Sprintf("type=%s runtime=%s session_id=%s tool_result=<nil>", ev.Type, ev.Runtime, ev.SessionID)
		}
		return fmt.Sprintf("type=%s runtime=%s session_id=%s tool_call_id=%s content_len=%d", ev.Type, ev.Runtime, ev.SessionID, ev.ToolResult.ToolCallID, runeLen(ev.ToolResult.Content))
	case cula.EventState:
		exitCode := ""
		if ev.ExitCode != nil {
			exitCode = fmt.Sprintf(" exit_code=%d", *ev.ExitCode)
		}
		return fmt.Sprintf("type=%s runtime=%s session_id=%s state=%s%s", ev.Type, ev.Runtime, ev.SessionID, ev.State, exitCode)
	case cula.EventStderr, cula.EventError:
		return fmt.Sprintf("type=%s runtime=%s session_id=%s error_len=%d", ev.Type, ev.Runtime, ev.SessionID, runeLen(ev.Error))
	default:
		return fmt.Sprintf("type=%s runtime=%s session_id=%s raw_len=%d", ev.Type, ev.Runtime, ev.SessionID, len(ev.Raw))
	}
}

func (b *bot) debugf(format string, args ...any) {
	if b.cfg.debug {
		log.Printf("debug: "+format, args...)
	}
}

func logSlackError(action string, err error) {
	if err != nil {
		log.Printf("%s: %v", action, err)
	}
}
