// Copyright 2017-2018 New Vector Ltd
// Copyright 2019-2020 The Matrix.org Foundation C.I.C.
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

package sqlite3

import (
	"context"
	"database/sql"

	"github.com/matrix-org/dendrite/common"
	"github.com/matrix-org/dendrite/syncapi/types"
	"github.com/matrix-org/gomatrixserverlib"
)

const inviteEventsSchema = `
CREATE TABLE IF NOT EXISTS syncapi_invite_events (
	id INTEGER PRIMARY KEY,
	event_id TEXT NOT NULL,
	room_id TEXT NOT NULL,
	target_user_id TEXT NOT NULL,
	event_json TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS syncapi_invites_target_user_id_idx ON syncapi_invite_events (target_user_id, id);
CREATE INDEX IF NOT EXISTS syncapi_invites_event_id_idx ON syncapi_invite_events (event_id);
`

const insertInviteEventSQL = "" +
	"INSERT INTO syncapi_invite_events" +
	" (id, room_id, event_id, target_user_id, event_json)" +
	" VALUES ($1, $2, $3, $4, $5)"

const deleteInviteEventSQL = "" +
	"DELETE FROM syncapi_invite_events WHERE event_id = $1"

const selectInviteEventsInRangeSQL = "" +
	"SELECT room_id, event_json FROM syncapi_invite_events" +
	" WHERE target_user_id = $1 AND id > $2 AND id <= $3" +
	" ORDER BY id DESC"

const selectMaxInviteIDSQL = "" +
	"SELECT MAX(id) FROM syncapi_invite_events"

type inviteEventsStatements struct {
	streamIDStatements            *streamIDStatements
	insertInviteEventStmt         *sql.Stmt
	selectInviteEventsInRangeStmt *sql.Stmt
	deleteInviteEventStmt         *sql.Stmt
	selectMaxInviteIDStmt         *sql.Stmt
}

func (s *inviteEventsStatements) prepare(db *sql.DB, streamID *streamIDStatements) (err error) {
	s.streamIDStatements = streamID
	_, err = db.Exec(inviteEventsSchema)
	if err != nil {
		return
	}
	if s.insertInviteEventStmt, err = db.Prepare(insertInviteEventSQL); err != nil {
		return
	}
	if s.selectInviteEventsInRangeStmt, err = db.Prepare(selectInviteEventsInRangeSQL); err != nil {
		return
	}
	if s.deleteInviteEventStmt, err = db.Prepare(deleteInviteEventSQL); err != nil {
		return
	}
	if s.selectMaxInviteIDStmt, err = db.Prepare(selectMaxInviteIDSQL); err != nil {
		return
	}
	return
}

func (s *inviteEventsStatements) insertInviteEvent(
	ctx context.Context, txn *sql.Tx, inviteEvent gomatrixserverlib.Event, streamPos types.StreamPosition,
) (err error) {
	_, err = txn.Stmt(s.insertInviteEventStmt).ExecContext(
		ctx,
		streamPos,
		inviteEvent.RoomID(),
		inviteEvent.EventID(),
		*inviteEvent.StateKey(),
		inviteEvent.JSON(),
	)
	return
}

func (s *inviteEventsStatements) deleteInviteEvent(
	ctx context.Context, inviteEventID string,
) error {
	_, err := s.deleteInviteEventStmt.ExecContext(ctx, inviteEventID)
	return err
}

// selectInviteEventsInRange returns a map of room ID to invite event for the
// active invites for the target user ID in the supplied range.
func (s *inviteEventsStatements) selectInviteEventsInRange(
	ctx context.Context, txn *sql.Tx, targetUserID string, startPos, endPos types.StreamPosition,
) (map[string]gomatrixserverlib.Event, error) {
	stmt := common.TxStmt(txn, s.selectInviteEventsInRangeStmt)
	rows, err := stmt.QueryContext(ctx, targetUserID, startPos, endPos)
	if err != nil {
		return nil, err
	}
	defer common.CloseAndLogIfError(ctx, rows, "selectInviteEventsInRange: rows.close() failed")
	result := map[string]gomatrixserverlib.Event{}
	for rows.Next() {
		var (
			roomID    string
			eventJSON []byte
		)
		if err = rows.Scan(&roomID, &eventJSON); err != nil {
			return nil, err
		}

		event, err := gomatrixserverlib.NewEventFromTrustedJSON(eventJSON, false)
		if err != nil {
			return nil, err
		}

		result[roomID] = event
	}
	return result, nil
}

func (s *inviteEventsStatements) selectMaxInviteID(
	ctx context.Context, txn *sql.Tx,
) (id int64, err error) {
	var nullableID sql.NullInt64
	stmt := common.TxStmt(txn, s.selectMaxInviteIDStmt)
	err = stmt.QueryRowContext(ctx).Scan(&nullableID)
	if nullableID.Valid {
		id = nullableID.Int64
	}
	return
}