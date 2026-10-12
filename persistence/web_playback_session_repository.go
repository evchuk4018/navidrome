package persistence

import (
	"context"
	"encoding/json"

	. "github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/model"
	"github.com/pocketbase/dbx"
)

type webPlaybackSessionRepository struct{ sqlRepository }

func (r *webPlaybackSessionRepository) RadioItemForUser(userID, sessionID, itemID string) (*model.PersonalRadioItem, error) {
	var item model.PersonalRadioItem
	err := r.queryOne(Select("i.id", "i.session_id", "i.item_type", "i.status", "coalesce(i.media_file_id,'') AS media_file_id").
		From("personal_radio_item i").Join("personal_radio_session s ON s.id=i.session_id").
		Where(Eq{"s.user_id": userID, "s.id": sessionID, "i.id": itemID}), &item)
	return &item, err
}

func NewWebPlaybackSessionRepository(ctx context.Context, builder dbx.Builder) model.WebPlaybackSessionRepository {
	return &webPlaybackSessionRepository{sqlRepository: sqlRepository{ctx: ctx, db: builder, tableName: "web_playback_session"}}
}

func (r *webPlaybackSessionRepository) Retrieve(userID string) (*model.WebPlaybackSession, error) {
	var row struct{ Snapshot string }
	err := r.queryOne(Select("snapshot").From(r.tableName).Where(Eq{"user_id": userID}), &row)
	if err != nil {
		return nil, err
	}
	var session model.WebPlaybackSession
	if err := json.Unmarshal([]byte(row.Snapshot), &session); err != nil {
		return nil, err
	}
	session.UserID = userID
	return &session, nil
}

// Store is called within WithTxImmediate together with the ownership check.
func (r *webPlaybackSessionRepository) Store(session *model.WebPlaybackSession) error {
	payload, err := json.Marshal(session)
	if err != nil {
		return err
	}
	_, err = r.executeSQL(Expr(`INSERT INTO web_playback_session (user_id,owner_client_id,epoch,snapshot)
		VALUES (?,?,?,?) ON CONFLICT(user_id) DO UPDATE SET
		owner_client_id=excluded.owner_client_id,epoch=excluded.epoch,snapshot=excluded.snapshot`,
		session.UserID, session.OwnerClientID, session.Epoch, string(payload)))
	return err
}
