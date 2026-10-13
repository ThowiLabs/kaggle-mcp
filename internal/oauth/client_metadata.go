package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"time"
)

const maxClientMetadataBytes = 5 << 10

var blockedClientMetadataPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.31.196.0/24"),
	netip.MustParsePrefix("192.52.193.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("192.175.48.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("100:0:0:1::/64"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("2620:4f:8000::/48"),
	netip.MustParsePrefix("3fff::/20"),
	netip.MustParsePrefix("5f00::/16"),
}

type clientMetadataDocument struct {
	ClientID                          string   `json:"client_id"`
	ClientName                        string   `json:"client_name"`
	RedirectURIs                      []string `json:"redirect_uris"`
	GrantTypes                        []string `json:"grant_types"`
	ResponseTypes                     []string `json:"response_types"`
	TokenEndpointAuthMethod           string   `json:"token_endpoint_auth_method"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
}

func newClientMetadataHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, fmt.Errorf("parse client metadata address: %w", err)
			}
			resolved, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, fmt.Errorf("resolve client metadata host: %w", err)
			}
			if len(resolved) == 0 {
				return nil, errors.New("client metadata host resolved to no addresses")
			}
			for _, candidate := range resolved {
				if !publicClientMetadataIP(candidate.IP) {
					return nil, errors.New("client metadata host resolved to a non-public address")
				}
			}
			var lastErr error
			for _, candidate := range resolved {
				conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(candidate.IP.String(), port))
				if err == nil {
					return conn, nil
				}
				lastErr = err
			}
			return nil, fmt.Errorf("connect to client metadata host: %w", lastErr)
		},
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
		IdleConnTimeout:       30 * time.Second,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func publicClientMetadataIP(ip net.IP) bool {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsMulticast() || addr.IsUnspecified() {
		return false
	}
	for _, prefix := range blockedClientMetadataPrefixes {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}

func validateClientMetadataURL(raw string) error {
	if raw != strings.TrimSpace(raw) {
		return errors.New("client_id metadata document URL cannot contain surrounding whitespace")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
		return errors.New("client_id metadata document must be an https URL without credentials or fragment")
	}
	if u.Path == "" {
		return errors.New("client_id metadata document URL must contain a path")
	}
	for _, segment := range strings.Split(u.Path, "/") {
		if segment == "." || segment == ".." {
			return errors.New("client_id metadata document URL cannot contain dot path segments")
		}
	}
	return nil
}

func (s *Server) resolveOAuthClient(ctx context.Context, clientID string) (Client, bool, error) {
	s.store.mu.Lock()
	client, ok := s.store.data.Clients[clientID]
	s.store.mu.Unlock()
	if ok {
		return client, true, nil
	}
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(clientID)), "https://") {
		return Client{}, false, nil
	}
	client, err := resolveClientMetadata(ctx, s.clientMetadataHTTPClient, clientID)
	if err != nil {
		return Client{}, false, err
	}
	return client, true, nil
}

func resolveClientMetadata(ctx context.Context, client *http.Client, clientID string) (Client, error) {
	if err := validateClientMetadataURL(clientID); err != nil {
		return Client{}, err
	}
	if client == nil {
		return Client{}, errors.New("client metadata HTTP client is unavailable")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, clientID, nil)
	if err != nil {
		return Client{}, fmt.Errorf("create client metadata request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return Client{}, fmt.Errorf("fetch client metadata document: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Client{}, fmt.Errorf("client metadata document returned HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxClientMetadataBytes+1))
	if err != nil {
		return Client{}, fmt.Errorf("read client metadata document: %w", err)
	}
	if len(raw) > maxClientMetadataBytes {
		return Client{}, errors.New("client metadata document is too large")
	}
	var metadata clientMetadataDocument
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return Client{}, fmt.Errorf("decode client metadata document: %w", err)
	}
	if metadata.ClientID != clientID {
		return Client{}, errors.New("client metadata document client_id does not match its URL")
	}
	if strings.TrimSpace(metadata.ClientName) == "" {
		return Client{}, errors.New("client metadata document client_name is required")
	}
	if len(metadata.RedirectURIs) == 0 {
		return Client{}, errors.New("client metadata document requires at least one redirect URI")
	}
	for _, redirectURI := range metadata.RedirectURIs {
		if !validRedirectURI(redirectURI) {
			return Client{}, errors.New("client metadata document contains an invalid redirect URI")
		}
	}
	if len(metadata.GrantTypes) > 0 && !slices.Contains(metadata.GrantTypes, "authorization_code") {
		return Client{}, errors.New("client metadata document must support authorization_code")
	}
	if len(metadata.ResponseTypes) > 0 && !slices.Contains(metadata.ResponseTypes, "code") {
		return Client{}, errors.New("client metadata document must support code response type")
	}
	if len(metadata.TokenEndpointAuthMethodsSupported) > 0 {
		if !slices.Contains(metadata.TokenEndpointAuthMethodsSupported, "none") {
			return Client{}, errors.New("client metadata document does not support token endpoint authentication method none")
		}
	} else if metadata.TokenEndpointAuthMethod != "" && metadata.TokenEndpointAuthMethod != "none" {
		return Client{}, errors.New("client metadata document must use token_endpoint_auth_method none")
	}
	return Client{
		ID:           clientID,
		Name:         metadata.ClientName,
		RedirectURIs: append([]string(nil), metadata.RedirectURIs...),
		CreatedAt:    time.Now().UTC(),
	}, nil
}
