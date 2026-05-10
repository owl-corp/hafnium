package discord

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
)

type Client struct {
	session *discordgo.Session
}

func NewClient(token string) (*Client, error) {
	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("failed to create discord session: %w", err)
	}
	return &Client{session: dg}, nil
}

func (c *Client) SendDM(userID, message string) error {
	channel, err := c.session.UserChannelCreate(userID)
	if err != nil {
		return fmt.Errorf("failed to create DM channel for %s: %w", userID, err)
	}
	_, err = c.session.ChannelMessageSend(channel.ID, message)
	if err != nil {
		return fmt.Errorf("failed to send DM to %s: %w", userID, err)
	}
	return nil
}

func (c *Client) SendReport(channelID, message string) error {
	_, err := c.session.ChannelMessageSend(channelID, message)
	if err != nil {
		return fmt.Errorf("failed to send report to %s: %w", channelID, err)
	}
	return nil
}

func (c *Client) Close() error {
	return c.session.Close()
}
