// Copyright 2017 Vector Creations Ltd
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package internal

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/matrix-org/dendrite/internal/config"
	"github.com/matrix-org/dendrite/roomserver/api"

	"github.com/matrix-org/gomatrixserverlib"
)

// ErrRoomNoExists is returned when trying to lookup the state of a room that
// doesn't exist
var ErrRoomNoExists = errors.New("Room does not exist")

// BuildEvent builds a Matrix event using the event builder and roomserver query
// API client provided. If also fills roomserver query API response (if provided)
// in case the function calling FillBuilder needs to use it.
// Returns ErrRoomNoExists if the state of the room could not be retrieved because
// the room doesn't exist
// Returns an error if something else went wrong
func BuildEvent(
	ctx context.Context,
	builder *gomatrixserverlib.EventBuilder, cfg *config.Dendrite, evTime time.Time,
	rsAPI api.RoomserverInternalAPI, queryRes *api.QueryLatestEventsAndStateResponse,
) (*gomatrixserverlib.Event, error) {
	if queryRes == nil {
		queryRes = &api.QueryLatestEventsAndStateResponse{}
	}

	err := AddPrevEventsToEvent(ctx, builder, rsAPI, queryRes)
	if err != nil {
		// This can pass through a ErrRoomNoExists to the caller
		return nil, err
	}

	event, err := builder.Build(
		evTime, cfg.Matrix.ServerName, cfg.Matrix.KeyID,
		cfg.Matrix.PrivateKey, queryRes.RoomVersion,
	)
	if err != nil {
		return nil, err
	}

	return &event, nil
}

// AddPrevEventsToEvent fills out the prev_events and auth_events fields in builder
func AddPrevEventsToEvent(
	ctx context.Context,
	builder *gomatrixserverlib.EventBuilder,
	rsAPI api.RoomserverInternalAPI, queryRes *api.QueryLatestEventsAndStateResponse,
) error {
	eventsNeeded, err := gomatrixserverlib.StateNeededForEventBuilder(builder)
	if err != nil {
		return fmt.Errorf("gomatrixserverlib.StateNeededForEventBuilder: %w", err)
	}

	if len(eventsNeeded.Tuples()) == 0 {
		return errors.New("expecting state tuples for event builder, got none")
	}

	// Ask the roomserver for information about this room
	queryReq := api.QueryLatestEventsAndStateRequest{
		RoomID:       builder.RoomID,
		StateToFetch: eventsNeeded.Tuples(),
	}
	if err = rsAPI.QueryLatestEventsAndState(ctx, &queryReq, queryRes); err != nil {
		return fmt.Errorf("rsAPI.QueryLatestEventsAndState: %w", err)
	}

	if !queryRes.RoomExists {
		return ErrRoomNoExists
	}

	eventFormat, err := queryRes.RoomVersion.EventFormat()
	if err != nil {
		return fmt.Errorf("queryRes.RoomVersion.EventFormat: %w", err)
	}

	builder.Depth = queryRes.Depth

	authEvents := gomatrixserverlib.NewAuthEvents(nil)

	for i := range queryRes.StateEvents {
		err = authEvents.AddEvent(&queryRes.StateEvents[i].Event)
		if err != nil {
			return fmt.Errorf("authEvents.AddEvent: %w", err)
		}
	}

	refs, err := eventsNeeded.AuthEventReferences(&authEvents)
	if err != nil {
		return fmt.Errorf("eventsNeeded.AuthEventReferences: %w", err)
	}

	truncAuth, truncPrev := truncateAuthAndPrevEvents(refs, queryRes.LatestEvents)
	switch eventFormat {
	case gomatrixserverlib.EventFormatV1:
		builder.AuthEvents = truncAuth
		builder.PrevEvents = truncPrev
	case gomatrixserverlib.EventFormatV2:
		v2AuthRefs, v2PrevRefs := []string{}, []string{}
		for _, ref := range truncAuth {
			v2AuthRefs = append(v2AuthRefs, ref.EventID)
		}
		for _, ref := range truncPrev {
			v2PrevRefs = append(v2PrevRefs, ref.EventID)
		}
		builder.AuthEvents = v2AuthRefs
		builder.PrevEvents = v2PrevRefs
	}

	return nil
}

// truncateAuthAndPrevEvents limits the number of events we add into
// an event as prev_events or auth_events.
// NOTSPEC: The limits here feel a bit arbitrary but they are currently
// here because of https://github.com/matrix-org/matrix-doc/issues/2307
// and because Synapse will just drop events that don't comply.
func truncateAuthAndPrevEvents(auth, prev []gomatrixserverlib.EventReference) (
	truncAuth, truncPrev []gomatrixserverlib.EventReference,
) {
	truncAuth, truncPrev = auth, prev
	if len(truncAuth) > 10 {
		truncAuth = truncAuth[:10]
	}
	if len(truncPrev) > 20 {
		truncPrev = truncPrev[:20]
	}
	return
}
