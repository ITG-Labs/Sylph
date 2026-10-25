package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ITG-Labs/sylph/services/core/internal/store"
)

const (
	maxGateTextRunes   = 1000
	maxHelpTextRunes   = 3000
	maxHelpButtons     = 6
	maxHelpLabelRunes  = 32
	maxSettingsURLByte = 512
)

var (
	channelUsername = regexp.MustCompile(`^@[A-Za-z][A-Za-z0-9_]{4,31}$`)
	channelID       = regexp.MustCompile(`^-100[0-9]{5,15}$`)
	linkLabel       = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)
	linkTLD         = regexp.MustCompile(`^([A-Za-z]{2,63}|xn--[A-Za-z0-9-]{1,59})$`)
)

type BotTerms struct {
	Enabled bool   `json:"enabled"`
	Text    string `json:"text"`
	URL     string `json:"url"`
	Version int    `json:"version"`
}

type BotChannel struct {
	Enabled bool   `json:"enabled"`
	Chat    string `json:"chat"`
	URL     string `json:"url"`
	Text    string `json:"text"`
}

type BotHelp struct {
	Enabled bool               `json:"enabled"`
	Text    string             `json:"text"`
	Buttons []store.HelpButton `json:"buttons"`
}

type BotConfig struct {
	Terms   BotTerms   `json:"terms"`
	Channel BotChannel `json:"channel"`
	Help    BotHelp    `json:"help"`
}

func (s *Service) BotConfig(ctx context.Context) (BotConfig, error) {
	st, err := s.store.GetSettings(ctx)
	if err != nil {
		return BotConfig{}, err
	}
	return BotConfig{
		Terms:   BotTerms{Enabled: st.TermsEnabled, Text: st.TermsText, URL: st.TermsURL, Version: st.TermsVersion},
		Channel: BotChannel{Enabled: st.ChannelEnabled, Chat: st.ChannelChat, URL: st.ChannelURL, Text: st.ChannelText},
		Help:    BotHelp{Enabled: st.HelpEnabled, Text: st.HelpText, Buttons: st.HelpButtons},
	}, nil
}

func (s *Service) BumpTerms(ctx context.Context) (store.Settings, error) {
	return s.store.BumpTermsVersion(ctx)
}

func (s *Service) AcceptTerms(ctx context.Context, userID int64, version int) error {
	err := s.store.AcceptTerms(ctx, userID, version)
	if errors.Is(err, store.ErrConflict) {
		return &ConflictError{Field: "version", Message: "terms version is not current"}
	}
	return err
}

func (s *Service) LatestTerms(ctx context.Context, userID int64) (*store.TermsAcceptance, error) {
	return s.store.LatestTerms(ctx, userID)
}

func validLink(field, v string, schemes ...string) error {
	if len(v) > maxSettingsURLByte {
		return fieldErr(field, fmt.Sprintf("must be at most %d bytes", maxSettingsURLByte))
	}
	for _, r := range v {
		if r > unicode.MaxASCII || unicode.IsControl(r) || unicode.IsSpace(r) {
			return fieldErr(field, "must be ASCII without spaces or control characters (use punycode for non-Latin domains)")
		}
	}
	u, err := url.Parse(v)
	if err == nil && u.User == nil && validLinkHost(u) {
		for _, scheme := range schemes {
			if u.Scheme == scheme {
				return nil
			}
		}
	}
	return fieldErr(field, "must be a "+strings.Join(schemes, " or ")+" link")
}

func validLinkHost(u *url.URL) bool {
	if u.Scheme == "tg" {
		return linkLabel.MatchString(u.Host)
	}
	host := u.Hostname()
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return false
		}
	}
	labels := strings.Split(host, ".")
	if len(labels) < 2 || !linkTLD.MatchString(labels[len(labels)-1]) {
		return false
	}
	for _, l := range labels {
		if !linkLabel.MatchString(l) {
			return false
		}
	}
	return true
}

func autoChannelURL(chat string) string {
	if channelUsername.MatchString(chat) {
		return "https://t.me/" + strings.TrimPrefix(chat, "@")
	}
	return ""
}

func (s *Service) checkBotPatch(ctx context.Context, p *SettingsPatch) error {
	if p.TermsText != nil {
		if err := validBotHTML("terms_text", *p.TermsText, maxGateTextRunes); err != nil {
			return err
		}
	}
	if p.TermsURL != nil && *p.TermsURL != "" {
		if err := validLink("terms_url", *p.TermsURL, "https"); err != nil {
			return err
		}
	}
	if p.ChannelChat != nil && *p.ChannelChat != "" && !channelUsername.MatchString(*p.ChannelChat) && !channelID.MatchString(*p.ChannelChat) {
		return fieldErr("channel_chat", "must be @username of a public channel or its numeric id like -1001234567890")
	}
	if p.ChannelURL != nil && *p.ChannelURL != "" {
		if err := validLink("channel_url", *p.ChannelURL, "https"); err != nil {
			return err
		}
		if !strings.HasPrefix(*p.ChannelURL, "https://t.me/") {
			return fieldErr("channel_url", "must be a https://t.me/ link")
		}
	}
	if p.ChannelText != nil {
		if err := validBotHTML("channel_text", *p.ChannelText, maxGateTextRunes); err != nil {
			return err
		}
	}
	if p.HelpText != nil {
		if err := validBotHTML("help_text", *p.HelpText, maxHelpTextRunes); err != nil {
			return err
		}
	}
	if p.HelpButtons != nil {
		buttons, err := cleanHelpButtons(*p.HelpButtons)
		if err != nil {
			return err
		}
		p.HelpButtons = &buttons
	}
	if p.TermsEnabled == nil && p.TermsURL == nil && p.ChannelEnabled == nil && p.ChannelChat == nil && p.ChannelURL == nil &&
		p.HelpEnabled == nil && p.HelpText == nil {
		return nil
	}
	cur, err := s.store.GetSettings(ctx)
	if err != nil {
		return err
	}
	pick := func(patch *string, current string) string {
		if patch != nil {
			return *patch
		}
		return current
	}
	on := func(patch *bool, current bool) bool {
		if patch != nil {
			return *patch
		}
		return current
	}
	chat, link := pick(p.ChannelChat, cur.ChannelChat), pick(p.ChannelURL, cur.ChannelURL)
	if link == "" || (p.ChannelURL == nil && p.ChannelChat != nil && link == autoChannelURL(cur.ChannelChat)) {
		if auto := autoChannelURL(chat); auto != link {
			link = auto
			p.ChannelURL = &link
		}
	}
	switch {
	case on(p.TermsEnabled, cur.TermsEnabled) && pick(p.TermsURL, cur.TermsURL) == "":
		return fieldErr("terms_url", "is required to turn the terms on")
	case on(p.ChannelEnabled, cur.ChannelEnabled) && chat == "":
		return fieldErr("channel_chat", "is required to turn the channel check on")
	case on(p.ChannelEnabled, cur.ChannelEnabled) && link == "":
		return fieldErr("channel_url", "is required for a channel without @username")
	case on(p.HelpEnabled, cur.HelpEnabled) && pick(p.HelpText, cur.HelpText) == "":
		return fieldErr("help_text", "is required to turn help on")
	}
	return nil
}

func cleanHelpButtons(in []store.HelpButton) ([]store.HelpButton, error) {
	if len(in) > maxHelpButtons {
		return nil, fieldErr("help_buttons", fmt.Sprintf("at most %d buttons", maxHelpButtons))
	}
	out := make([]store.HelpButton, len(in))
	for i, b := range in {
		label := strings.TrimSpace(b.Label)
		if label == "" || utf8.RuneCountInString(label) > maxHelpLabelRunes {
			return nil, fieldErr("help_buttons", fmt.Sprintf("button %d: label must be 1-%d characters", i+1, maxHelpLabelRunes))
		}
		for _, r := range label {
			if unicode.IsControl(r) {
				return nil, fieldErr("help_buttons", fmt.Sprintf("button %d: label must not contain control characters", i+1))
			}
		}
		if err := validLink("help_buttons", b.URL, "https", "tg"); err != nil {
			return nil, fieldErr("help_buttons", fmt.Sprintf("button %d: link must be https:// or tg://", i+1))
		}
		out[i] = store.HelpButton{Label: label, URL: b.URL}
	}
	return out, nil
}
