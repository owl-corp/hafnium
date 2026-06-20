package keycloak

import (
	"context"
	"fmt"

	"github.com/Nerzal/gocloak/v13"
)

type Client struct {
	client   *gocloak.GoCloak
	token    *gocloak.JWT
	realm    string
	username string
	password string
}

type Identity struct {
	UserID   string
	UserName string
}

func NewClient(url, realm, username, password string) *Client {
	return &Client{
		client:   gocloak.NewClient(url),
		realm:    realm,
		username: username,
		password: password,
	}
}

func (c *Client) ResetToken() {
	c.token = nil
}

func (c *Client) login(ctx context.Context) error {
	if c.token != nil {
		return nil
	}
	token, err := c.client.LoginAdmin(ctx, c.username, c.password, "master")
	if err != nil {
		return fmt.Errorf("failed to login to keycloak: %w", err)
	}
	c.token = token
	return nil
}

func (c *Client) GetUsers(ctx context.Context) ([]*gocloak.User, error) {
	if err := c.login(ctx); err != nil {
		return nil, err
	}

	params := gocloak.GetUsersParams{
		Max: new(1000),
	}
	users, err := c.client.GetUsers(ctx, c.token.AccessToken, c.realm, params)
	if err != nil {
		return nil, fmt.Errorf("failed to get users: %w", err)
	}
	return users, nil
}

func (c *Client) GetUserFederatedIdentities(ctx context.Context, userID string) ([]*gocloak.FederatedIdentityRepresentation, error) {
	if err := c.login(ctx); err != nil {
		return nil, err
	}

	identities, err := c.client.GetUserFederatedIdentities(ctx, c.token.AccessToken, c.realm, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get federated identities: %w", err)
	}
	return identities, nil
}

func (c *Client) GetUserFederatedIdentity(ctx context.Context, userID, provider string) (*Identity, error) {
	identities, err := c.GetUserFederatedIdentities(ctx, userID)
	if err != nil {
		return nil, err
	}

	for _, ident := range identities {
		if *ident.IdentityProvider == provider {
			return &Identity{
				UserID:   *ident.UserID,
				UserName: *ident.UserName,
			}, nil
		}
	}
	return nil, nil
}

func (c *Client) GetUserRoles(ctx context.Context, userID string) ([]*gocloak.Role, error) {
	if err := c.login(ctx); err != nil {
		return nil, err
	}

	roles, err := c.client.GetRealmRolesByUserID(ctx, c.token.AccessToken, c.realm, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user roles: %w", err)
	}
	return roles, nil
}

func (c *Client) RemoveUserFederatedIdentity(ctx context.Context, userID, provider string) error {
	if err := c.login(ctx); err != nil {
		return err
	}

	err := c.client.DeleteUserFederatedIdentity(ctx, c.token.AccessToken, c.realm, userID, provider)
	if err != nil {
		return fmt.Errorf("failed to remove federated identity: %w", err)
	}
	return nil
}
