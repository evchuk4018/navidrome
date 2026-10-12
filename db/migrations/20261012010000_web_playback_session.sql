-- +goose Up
CREATE TABLE web_playback_session (
    user_id TEXT PRIMARY KEY REFERENCES user(id) ON DELETE CASCADE,
    owner_client_id TEXT NOT NULL,
    epoch INTEGER NOT NULL,
    snapshot TEXT NOT NULL
);

-- +goose Down
DROP TABLE web_playback_session;
