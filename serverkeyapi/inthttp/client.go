package inthttp

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/matrix-org/dendrite/internal/caching"
	internalHTTP "github.com/matrix-org/dendrite/internal/http"
	"github.com/matrix-org/dendrite/serverkeyapi/api"
	"github.com/matrix-org/gomatrixserverlib"
	"github.com/opentracing/opentracing-go"
)

// HTTP paths for the internal HTTP APIs
const (
	ServerKeyInputPublicKeyPath = "/serverkeyapi/inputPublicKey"
	ServerKeyQueryPublicKeyPath = "/serverkeyapi/queryPublicKey"
)

// NewServerKeyClient creates a ServerKeyInternalAPI implemented by talking to a HTTP POST API.
// If httpClient is nil an error is returned
func NewServerKeyClient(
	serverKeyAPIURL string,
	httpClient *http.Client,
	cache caching.ServerKeyCache,
) (api.ServerKeyInternalAPI, error) {
	if httpClient == nil {
		return nil, errors.New("NewRoomserverInternalAPIHTTP: httpClient is <nil>")
	}
	return &httpServerKeyInternalAPI{
		serverKeyAPIURL: serverKeyAPIURL,
		httpClient:      httpClient,
		cache:           cache,
	}, nil
}

type httpServerKeyInternalAPI struct {
	serverKeyAPIURL string
	httpClient      *http.Client
	cache           caching.ServerKeyCache
}

func (s *httpServerKeyInternalAPI) KeyRing() *gomatrixserverlib.KeyRing {
	// This is a bit of a cheat - we tell gomatrixserverlib that this API is
	// both the key database and the key fetcher. While this does have the
	// rather unfortunate effect of preventing gomatrixserverlib from handling
	// key fetchers directly, we can at least reimplement this behaviour on
	// the other end of the API.
	return &gomatrixserverlib.KeyRing{
		KeyDatabase: s,
		KeyFetchers: []gomatrixserverlib.KeyFetcher{s},
	}
}

func (s *httpServerKeyInternalAPI) FetcherName() string {
	return "httpServerKeyInternalAPI"
}

func (s *httpServerKeyInternalAPI) StoreKeys(
	_ context.Context,
	results map[gomatrixserverlib.PublicKeyLookupRequest]gomatrixserverlib.PublicKeyLookupResult,
) error {
	// Run in a background context - we don't want to stop this work just
	// because the caller gives up waiting.
	ctx := context.Background()
	request := api.InputPublicKeysRequest{
		Keys: make(map[gomatrixserverlib.PublicKeyLookupRequest]gomatrixserverlib.PublicKeyLookupResult),
	}
	response := api.InputPublicKeysResponse{}
	for req, res := range results {
		request.Keys[req] = res
		s.cache.StoreServerKey(req, res)
	}
	return s.InputPublicKeys(ctx, &request, &response)
}

func (s *httpServerKeyInternalAPI) FetchKeys(
	_ context.Context,
	requests map[gomatrixserverlib.PublicKeyLookupRequest]gomatrixserverlib.Timestamp,
) (map[gomatrixserverlib.PublicKeyLookupRequest]gomatrixserverlib.PublicKeyLookupResult, error) {
	// Run in a background context - we don't want to stop this work just
	// because the caller gives up waiting.
	ctx := context.Background()
	result := make(map[gomatrixserverlib.PublicKeyLookupRequest]gomatrixserverlib.PublicKeyLookupResult)
	request := api.QueryPublicKeysRequest{
		Requests: make(map[gomatrixserverlib.PublicKeyLookupRequest]gomatrixserverlib.Timestamp),
	}
	response := api.QueryPublicKeysResponse{
		Results: make(map[gomatrixserverlib.PublicKeyLookupRequest]gomatrixserverlib.PublicKeyLookupResult),
	}
	now := gomatrixserverlib.AsTimestamp(time.Now())
	for req, ts := range requests {
		if res, ok := s.cache.GetServerKey(req); ok {
			if now > res.ValidUntilTS && res.ExpiredTS == gomatrixserverlib.PublicKeyNotExpired {
				continue
			}
			result[req] = res
			continue
		}
		request.Requests[req] = ts
	}
	err := s.QueryPublicKeys(ctx, &request, &response)
	if err != nil {
		return nil, err
	}
	for req, res := range response.Results {
		result[req] = res
		s.cache.StoreServerKey(req, res)
	}
	return result, nil
}

func (h *httpServerKeyInternalAPI) InputPublicKeys(
	ctx context.Context,
	request *api.InputPublicKeysRequest,
	response *api.InputPublicKeysResponse,
) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "InputPublicKey")
	defer span.Finish()

	apiURL := h.serverKeyAPIURL + ServerKeyInputPublicKeyPath
	return internalHTTP.PostJSON(ctx, span, h.httpClient, apiURL, request, response)
}

func (h *httpServerKeyInternalAPI) QueryPublicKeys(
	ctx context.Context,
	request *api.QueryPublicKeysRequest,
	response *api.QueryPublicKeysResponse,
) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "QueryPublicKey")
	defer span.Finish()

	apiURL := h.serverKeyAPIURL + ServerKeyQueryPublicKeyPath
	return internalHTTP.PostJSON(ctx, span, h.httpClient, apiURL, request, response)
}
