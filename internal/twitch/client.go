// Package twitch ingests a Twitch channel's chat over anonymous IRC and hands
// each message to a callback. It owns its own reconnect loop — exponential
// backoff with jitter — so a dropped connection recovers without restarting
// the process. No OAuth: anonymous justinfan read-only (see plan non-goals).
package twitch

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"time"

	gotwitch "github.com/gempir/go-twitch-irc/v4"
)

// Message is the minimal chat event the rest of the bridge consumes. The user
// name lives here only for hashing/overlay downstream — it never reaches the
// game world.
type Message struct {
	User string
	Text string
}

// stableConn is how long a connection must last to count as "healthy" and
// reset the backoff. Shorter connections keep backing off (anti-flap).
const stableConn = 30 * time.Second

// Client streams one channel's chat to onMsg.
type Client struct {
	channel string
	nick    string
	onMsg   func(Message)
	log     *slog.Logger

	baseDelay time.Duration
	maxDelay  time.Duration
	rnd       func() float64 // injectable; defaults to rand.Float64

	// ircAddress overrides the Twitch IRC endpoint (empty = real Twitch TLS).
	// Used for proxying.
	ircAddress string

	// connectOnce runs a single connection attempt, blocking until it drops or
	// ctx is cancelled. Injectable so the reconnect loop is testable without
	// network; defaults to the real IRC connection.
	connectOnce func(ctx context.Context) error
}

// SetIRCAddress points the client at a non-default IRC endpoint (plaintext).
func (c *Client) SetIRCAddress(addr string) { c.ircAddress = addr }

// New builds a client for channel; onMsg is called for every chat message.
func New(channel string, onMsg func(Message), log *slog.Logger) *Client {
	c := &Client{
		channel: channel,
		// anonymous read-only nick: justinfan + random number (the library's
		// own anonymous client hardcodes one nick, which would collide across
		// instances)
		nick:      fmt.Sprintf("justinfan%d", 100000+rand.Intn(899999)),
		onMsg:     onMsg,
		log:       log,
		baseDelay: time.Second,
		maxDelay:  60 * time.Second,
		rnd:       rand.Float64,
	}
	c.connectOnce = c.ircConnectOnce
	return c
}

// Run connects and streams until ctx is cancelled, reconnecting on its own.
// It returns ctx.Err() on shutdown.
func (c *Client) Run(ctx context.Context) error {
	attempt := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		start := time.Now()
		err := c.connectOnce(ctx)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr // shutdown, not a connection failure
		}
		c.log.Warn("twitch connection ended", "err", err,
			"uptime", time.Since(start).Round(time.Second))

		if time.Since(start) >= stableConn {
			attempt = 0 // healthy session dropped → retry promptly
		} else {
			attempt++
		}

		delay := backoff(attempt, c.baseDelay, c.maxDelay, c.rnd())
		c.log.Info("reconnecting", "in", delay.Round(time.Millisecond), "attempt", attempt)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
}

// ircConnectOnce is the real single-attempt connection: it joins the channel
// and blocks until the IRC connection drops or ctx is cancelled.
func (c *Client) ircConnectOnce(ctx context.Context) error {
	irc := gotwitch.NewClient(c.nick, "oauth:59301") // dummy pass; anon read
	if c.ircAddress != "" {
		irc.IrcAddress = c.ircAddress
		irc.TLS = false
	}
	irc.OnConnect(func() {
		c.log.Info("twitch connected", "channel", c.channel, "nick", c.nick)
	})
	irc.OnPrivateMessage(func(m gotwitch.PrivateMessage) {
		c.onMsg(Message{User: m.User.Name, Text: m.Message})
	})
	irc.Join(c.channel)

	errCh := make(chan error, 1)
	go func() { errCh <- irc.Connect() }()

	select {
	case <-ctx.Done():
		_ = irc.Disconnect() // unblocks Connect; goroutine drains into errCh
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

// backoff returns the delay before retry `attempt` (0-based): base·2^attempt
// capped at max, then ±25% jittered (jitter in [0,1)) and clamped to [base,max].
func backoff(attempt int, base, max time.Duration, jitter float64) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	d := max
	if attempt < 32 {
		if s := base << uint(attempt); s > 0 && s < max {
			d = s
		}
	}
	d += time.Duration((jitter - 0.5) * 0.5 * float64(d)) // ±25%
	if d > max {
		d = max
	}
	if d < base {
		d = base
	}
	return d
}
