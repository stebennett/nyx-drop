package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"golang.org/x/oauth2"
)

// GitHubEndpoints allows tests to point the OAuth flow at a local stub. The
// zero value uses GitHub.com; these endpoints are not environment settings.
type GitHubEndpoints struct {
	AuthorizeURL string
	TokenURL     string
	UserURL      string
}

type GitHub struct {
	oauth   oauth2.Config
	userURL string
	client  *http.Client
}

func NewGitHub(clientID, clientSecret, callback string, endpoints GitHubEndpoints) *GitHub {
	if endpoints.AuthorizeURL == "" {
		endpoints.AuthorizeURL = "https://github.com/login/oauth/authorize"
	}
	if endpoints.TokenURL == "" {
		endpoints.TokenURL = "https://github.com/login/oauth/access_token"
	}
	if endpoints.UserURL == "" {
		endpoints.UserURL = "https://api.github.com/user"
	}
	return &GitHub{
		oauth: oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  callback,
			Endpoint: oauth2.Endpoint{
				AuthURL:   endpoints.AuthorizeURL,
				TokenURL:  endpoints.TokenURL,
				AuthStyle: oauth2.AuthStyleInParams,
			},
		},
		userURL: endpoints.UserURL,
		client: &http.Client{
			Timeout: 10 * time.Second,
			// Never follow a redirect on a request carrying credentials.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

// NewState generates two independent 256-bit values: the browser round-trip
// nonce and the private PKCE verifier. crypto/rand.Read never returns an error
// on this project's Go version.
func NewState() (nonce, verifier string) {
	var random [64]byte
	rand.Read(random[:])
	return base64.RawURLEncoding.EncodeToString(random[:32]), base64.RawURLEncoding.EncodeToString(random[32:])
}

func (g *GitHub) AuthorizeURL(state, verifier string) string {
	return g.oauth.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("prompt", "select_account"))
}

// ExchangeLogin uses a GitHub token only to fetch the authenticated login. It
// neither persists the token nor returns raw provider errors or bodies: they
// may contain credentials.
func (g *GitHub) ExchangeLogin(ctx context.Context, code, verifier string) (string, error) {
	ctx = context.WithValue(ctx, oauth2.HTTPClient, g.client)
	token, err := g.oauth.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return "", errors.New("GitHub code exchange failed")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.userURL, nil)
	if err != nil {
		return "", errors.New("invalid GitHub user endpoint")
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	token.SetAuthHeader(req)
	resp, err := g.client.Do(req)
	if err != nil {
		return "", errors.New("GitHub user request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", errors.New("GitHub user request failed")
	}
	const maxUserResponse = 64 << 10
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxUserResponse+1))
	var user struct {
		Login string `json:"login"`
	}
	if err != nil || len(body) > maxUserResponse || json.Unmarshal(body, &user) != nil || user.Login == "" {
		return "", errors.New("invalid GitHub user response")
	}
	return user.Login, nil
}
