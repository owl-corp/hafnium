package github

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/go-github/v62/github"
)

type Client struct {
	client *github.Client
	org    string
}

func NewClient(token, org string) *Client {
	return &Client{
		client: github.NewClient(nil).WithAuthToken(token),
		org:    org,
	}
}

func (c *Client) ListOrgMembers(ctx context.Context) (map[string]string, error) {
	opts := &github.ListMembersOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}
	members := make(map[string]string)
	for {
		users, resp, err := c.client.Organizations.ListMembers(ctx, c.org, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to list org members: %w", err)
		}
		for _, u := range users {
			members[fmt.Sprintf("%d", *u.ID)] = *u.Login
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return members, nil
}

func (c *Client) AddOrgMember(ctx context.Context, inviteeID int64) error {
	_, _, err := c.client.Organizations.CreateOrgInvitation(ctx, c.org, &github.CreateOrgInvitationOptions{
		InviteeID: new(inviteeID),
	})
	if err != nil {
		return fmt.Errorf("failed to add org member ID %d: %w", inviteeID, err)
	}
	return nil
}

func (c *Client) RemoveOrgMember(ctx context.Context, username string) error {
	_, err := c.client.Organizations.RemoveMember(ctx, c.org, username)
	if err != nil {
		return fmt.Errorf("failed to remove org member %s: %w", username, err)
	}
	return nil
}

func (c *Client) ListTeamMembers(ctx context.Context, teamSlug string) ([]string, error) {
	opts := &github.TeamListTeamMembersOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}
	var members []string
	for {
		users, resp, err := c.client.Teams.ListTeamMembersBySlug(ctx, c.org, teamSlug, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to list team members for %s: %w", teamSlug, err)
		}
		for _, u := range users {
			members = append(members, *u.Login)
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return members, nil
}

func (c *Client) AddTeamMember(ctx context.Context, teamSlug, username string) error {
	opts := &github.TeamAddTeamMembershipOptions{Role: "member"}
	_, _, err := c.client.Teams.AddTeamMembershipBySlug(ctx, c.org, teamSlug, username, opts)
	if err != nil {
		return fmt.Errorf("failed to add team member %s to %s: %w", username, teamSlug, err)
	}
	return nil
}

func (c *Client) RemoveTeamMember(ctx context.Context, teamSlug, username string) error {
	_, err := c.client.Teams.RemoveTeamMembershipBySlug(ctx, c.org, teamSlug, username)
	if err != nil {
		return fmt.Errorf("failed to remove team member %s from %s: %w", username, teamSlug, err)
	}
	return nil
}

func (c *Client) ListPendingInvitations(ctx context.Context) (set map[string]struct{}, err error) {
	opts := &github.ListOptions{PerPage: 100}
	set = make(map[string]struct{})
	for {
		invites, resp, err := c.client.Organizations.ListPendingOrgInvitations(ctx, c.org, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to list pending invitations: %w", err)
		}
		for _, inv := range invites {
			if inv.Login != nil {
				set[strings.ToLower(*inv.Login)] = struct{}{}
			}
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return set, nil
}

func (c *Client) ListFailedInvitations(ctx context.Context) (set map[string]struct{}, err error) {
	opts := &github.ListOptions{PerPage: 100}
	set = make(map[string]struct{})
	for {
		invites, resp, err := c.client.Organizations.ListFailedOrgInvitations(ctx, c.org, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to list failed invitations: %w", err)
		}
		for _, inv := range invites {
			if inv.Login != nil {
				set[strings.ToLower(*inv.Login)] = struct{}{}
			}
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return set, nil
}

func (c *Client) RemoveFailedInvitation(ctx context.Context, username string) error {
	// Using RemoveOrgMembership to cancel invitation as documented in go-github
	_, err := c.client.Organizations.RemoveOrgMembership(ctx, username, c.org)
	return err
}

func (c *Client) GetUsernameForID(ctx context.Context, id int64) (string, error) {
	u, _, err := c.client.Users.GetByID(ctx, id)
	if err != nil {
		return "", err
	}
	return *u.Login, nil
}
