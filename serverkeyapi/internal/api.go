package internal

import (
	"context"
	"fmt"
	"time"

	"github.com/matrix-org/dendrite/serverkeyapi/api"
	"github.com/matrix-org/gomatrixserverlib"
)

type ServerKeyAPI struct {
	api.ServerKeyInternalAPI

	OurKeyRing gomatrixserverlib.KeyRing
	FedClient  *gomatrixserverlib.FederationClient
}

func (s *ServerKeyAPI) KeyRing() *gomatrixserverlib.KeyRing {
	// Return a real keyring - one that has the real database and real
	// fetchers.
	return &s.OurKeyRing
}

func (s *ServerKeyAPI) StoreKeys(
	_ context.Context,
	results map[gomatrixserverlib.PublicKeyLookupRequest]gomatrixserverlib.PublicKeyLookupResult,
) error {
	// Run in a background context - we don't want to stop this work just
	// because the caller gives up waiting.
	ctx := context.Background()
	// Store any keys that we were given in our database.
	return s.OurKeyRing.KeyDatabase.StoreKeys(ctx, results)
}

func (s *ServerKeyAPI) FetchKeys(
	_ context.Context,
	requests map[gomatrixserverlib.PublicKeyLookupRequest]gomatrixserverlib.Timestamp,
) (map[gomatrixserverlib.PublicKeyLookupRequest]gomatrixserverlib.PublicKeyLookupResult, error) {
	// Run in a background context - we don't want to stop this work just
	// because the caller gives up waiting.
	ctx := context.Background()
	results := map[gomatrixserverlib.PublicKeyLookupRequest]gomatrixserverlib.PublicKeyLookupResult{}
	// First consult our local database and see if we have the requested
	// keys. These might come from a cache, depending on the database
	// implementation used.
	now := gomatrixserverlib.AsTimestamp(time.Now())
	if dbResults, err := s.OurKeyRing.KeyDatabase.FetchKeys(ctx, requests); err == nil {
		// We successfully got some keys. Add them to the results and
		// remove them from the request list.
		for req, res := range dbResults {
			if now > res.ValidUntilTS && res.ExpiredTS == gomatrixserverlib.PublicKeyNotExpired {
				continue
			}
			results[req] = res
			delete(requests, req)
		}
	}
	// For any key requests that we still have outstanding, next try to
	// fetch them directly. We'll go through each of the key fetchers to
	// ask for the remaining keys.
	for _, fetcher := range s.OurKeyRing.KeyFetchers {
		if len(requests) == 0 {
			break
		}
		if fetcherResults, err := fetcher.FetchKeys(ctx, requests); err == nil {
			// We successfully got some keys. Add them to the results and
			// remove them from the request list.
			for req, res := range fetcherResults {
				results[req] = res
				delete(requests, req)
			}
			if err = s.OurKeyRing.KeyDatabase.StoreKeys(ctx, fetcherResults); err != nil {
				return nil, fmt.Errorf("server key API failed to store retrieved keys: %w", err)
			}
		}
	}
	// If we failed to fetch any keys then we should report an error.
	if len(requests) > 0 {
		return results, fmt.Errorf("server key API failed to fetch %d keys", len(requests))
	}
	// Return the keys.
	return results, nil
}

func (s *ServerKeyAPI) FetcherName() string {
	return s.OurKeyRing.KeyDatabase.FetcherName()
}
