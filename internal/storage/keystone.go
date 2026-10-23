package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ubuntu/ubimi/internal/config"
)

// keystone authenticates against Keystone v3 and discovers the object-store
// endpoint from the service catalog. It caches the token until shortly before
// it expires; callers may force a refresh on a 401.
type keystone struct {
	client *http.Client

	authURL                    string
	user, password, userDomain string
	project, projectDomain     string
	region                     string

	mu       sync.Mutex
	token    string
	expires  time.Time
	endpoint string
}

func newKeystone(client *http.Client, cfg config.StorageConfig) *keystone {
	return &keystone{
		client:        client,
		authURL:       strings.TrimRight(cfg.AuthURL, "/"),
		user:          cfg.User,
		password:      cfg.Password,
		userDomain:    cfg.UserDomain,
		project:       cfg.Project,
		projectDomain: cfg.ProjectDomain,
		region:        cfg.Region,
	}
}

type keystoneRequest struct {
	Auth struct {
		Identity struct {
			Methods  []string `json:"methods"`
			Password struct {
				User struct {
					Name   string `json:"name"`
					Domain struct {
						Name string `json:"name"`
					} `json:"domain"`
					Password string `json:"password"`
				} `json:"user"`
			} `json:"password"`
		} `json:"identity"`
		Scope struct {
			Project struct {
				Name   string `json:"name"`
				Domain struct {
					Name string `json:"name"`
				} `json:"domain"`
			} `json:"project"`
		} `json:"scope"`
	} `json:"auth"`
}

type keystoneResponse struct {
	Token struct {
		ExpiresAt string `json:"expires_at"`
		Catalog   []struct {
			Type      string `json:"type"`
			Endpoints []struct {
				Interface string `json:"interface"`
				Region    string `json:"region"`
				URL       string `json:"url"`
			} `json:"endpoints"`
		} `json:"catalog"`
	} `json:"token"`
}

// refresh performs a fresh authentication request.
func (k *keystone) refresh(ctx context.Context) error {
	var body keystoneRequest
	body.Auth.Identity.Methods = []string{"password"}
	body.Auth.Identity.Password.User.Name = k.user
	body.Auth.Identity.Password.User.Domain.Name = k.userDomain
	body.Auth.Identity.Password.User.Password = k.password
	body.Auth.Scope.Project.Name = k.project
	body.Auth.Scope.Project.Domain.Name = k.projectDomain

	raw, err := json.Marshal(&body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, k.authURL+"/auth/tokens", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := k.client.Do(req)
	if err != nil {
		return fmt.Errorf("keystone auth: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("keystone auth: unexpected status %s", resp.Status)
	}
	token := resp.Header.Get("X-Subject-Token")
	if token == "" {
		return fmt.Errorf("keystone auth: missing X-Subject-Token")
	}

	var parsed keystoneResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return fmt.Errorf("keystone auth: decode token: %w", err)
	}
	endpoint, err := pickEndpoint(parsed, k.region)
	if err != nil {
		return err
	}
	expires, _ := time.Parse(time.RFC3339, parsed.Token.ExpiresAt)

	k.mu.Lock()
	k.token, k.endpoint, k.expires = token, endpoint, expires
	k.mu.Unlock()
	return nil
}

func pickEndpoint(r keystoneResponse, region string) (string, error) {
	for _, svc := range r.Token.Catalog {
		if svc.Type != "object-store" {
			continue
		}
		for _, ep := range svc.Endpoints {
			if ep.Interface != "public" {
				continue
			}
			if region != "" && ep.Region != region {
				continue
			}
			return strings.TrimRight(ep.URL, "/"), nil
		}
	}
	return "", fmt.Errorf("keystone auth: no public object-store endpoint in catalog")
}

// ensure returns a token valid for at least another minute, refreshing if needed.
func (k *keystone) ensure(ctx context.Context) error {
	k.mu.Lock()
	valid := k.token != "" && time.Until(k.expires) > time.Minute
	k.mu.Unlock()
	if valid {
		return nil
	}
	return k.refresh(ctx)
}

// credentials returns the current token and object-store endpoint.
func (k *keystone) credentials(ctx context.Context) (string, string, error) {
	if err := k.ensure(ctx); err != nil {
		return "", "", err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.token, k.endpoint, nil
}

// invalidate drops the cached token, forcing the next call to re-authenticate.
func (k *keystone) invalidate() {
	k.mu.Lock()
	k.token, k.expires = "", time.Time{}
	k.mu.Unlock()
}
