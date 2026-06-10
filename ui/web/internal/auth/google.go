package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/idtoken"
)

type GoogleConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

type GoogleProvider struct {
	clientID string
	oauth    *oauth2.Config
}

func NewGoogleProvider(cfg GoogleConfig) (*GoogleProvider, error) {
	if cfg.ClientID == "" || cfg.ClientSecret == "" || cfg.RedirectURL == "" {
		return nil, errors.New("auth: google client id, client secret, and redirect URL are required")
	}
	return &GoogleProvider{
		clientID: cfg.ClientID,
		oauth: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Endpoint:     google.Endpoint,
			Scopes:       []string{"openid", "email", "profile"},
		},
	}, nil
}

func (g *GoogleProvider) AuthCodeURL(state, codeVerifier string) (string, error) {
	if state == "" || codeVerifier == "" {
		return "", errors.New("auth: state and code verifier required")
	}
	challenge := pkceChallenge(codeVerifier)
	return g.oauth.AuthCodeURL(state,
		oauth2.SetAuthURLParam("code_challenge", challenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
		oauth2.SetAuthURLParam("prompt", "select_account"),
	), nil
}

func (g *GoogleProvider) Exchange(ctx context.Context, code, codeVerifier string) (Identity, error) {
	token, err := g.oauth.Exchange(ctx, code, oauth2.SetAuthURLParam("code_verifier", codeVerifier))
	if err != nil {
		return Identity{}, fmt.Errorf("auth: exchange google code: %w", err)
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return Identity{}, errors.New("auth: google response missing id_token")
	}
	payload, err := idtoken.Validate(ctx, rawIDToken, g.clientID)
	if err != nil {
		return Identity{}, fmt.Errorf("auth: validate google id_token: %w", err)
	}
	if verified, ok := payload.Claims["email_verified"].(bool); ok && !verified {
		return Identity{}, errors.New("auth: google email not verified")
	}
	email, _ := payload.Claims["email"].(string)
	name, _ := payload.Claims["name"].(string)
	picture, _ := payload.Claims["picture"].(string)
	if email == "" {
		return Identity{}, errors.New("auth: google id_token missing email")
	}
	return Identity{
		Email:     email,
		Name:      name,
		AvatarURL: picture,
	}, nil
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
