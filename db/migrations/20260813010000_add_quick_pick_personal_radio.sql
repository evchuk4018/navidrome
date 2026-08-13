-- +goose Up
create table playlist_play_history
(
    id          integer primary key autoincrement,
    user_id     varchar(255) not null references user (id) on delete cascade,
    playlist_id varchar(255) not null references playlist (id) on delete cascade,
    played_at   datetime not null
);

create index playlist_play_history_user_played
    on playlist_play_history (user_id, played_at desc);
create index playlist_play_history_playlist_played
    on playlist_play_history (playlist_id, played_at desc);

create table personal_radio_session
(
    id                    varchar(255) not null primary key,
    user_id               varchar(255) not null references user (id) on delete cascade,
    seed_media_file_id    varchar(255) not null references media_file (id) on delete cascade,
    status                varchar(32) not null,
    created_at            datetime not null,
    updated_at            datetime not null
);

create index personal_radio_session_user_status
    on personal_radio_session (user_id, status, updated_at desc);

create table personal_radio_item
(
    id                    varchar(255) not null primary key,
    session_id            varchar(255) not null references personal_radio_session (id) on delete cascade,
    position              integer not null,
    item_type             varchar(32) not null,
    status                varchar(32) not null,
    media_file_id         varchar(255) references media_file (id) on delete set null,
    recording_mbid        varchar(255) default '' not null,
    download_job_id       varchar(255) references music_download_job (id) on delete set null,
    created_at            datetime not null,
    updated_at            datetime not null,
    unique (session_id, position)
);

create index personal_radio_item_session_position
    on personal_radio_item (session_id, position);
create index personal_radio_item_download_job
    on personal_radio_item (download_job_id);

create table discovery_track
(
    id                    varchar(255) not null primary key,
    user_id               varchar(255) not null references user (id) on delete cascade,
    recording_mbid        varchar(255) not null,
    media_file_id         varchar(255) references media_file (id) on delete set null,
    state                 varchar(32) not null,
    play_starts           integer default 0 not null,
    expires_at            datetime,
    created_at            datetime not null,
    updated_at            datetime not null,
    unique (user_id, recording_mbid)
);

create index discovery_track_expiry
    on discovery_track (state, expires_at);
create index discovery_track_media_file
    on discovery_track (media_file_id);

create table radio_track_feedback
(
    user_id               varchar(255) not null references user (id) on delete cascade,
    recording_mbid        varchar(255) not null,
    positive_count        integer default 0 not null,
    completed_count       integer default 0 not null,
    neutral_skip_count    integer default 0 not null,
    early_skip_count      integer default 0 not null,
    last_early_skip_at    datetime,
    updated_at            datetime not null,
    primary key (user_id, recording_mbid)
);

alter table music_download_job add column origin varchar(32) default 'manual' not null;
alter table music_download_job add column priority integer default 0 not null;
alter table music_download_job add column radio_item_id varchar(255) default '' not null;
alter table music_download_job add column media_file_id varchar(255) default '' not null;

create index music_download_job_origin_status_priority
    on music_download_job (origin, status, priority desc, created_at);

-- +goose Down
drop index if exists music_download_job_origin_status_priority;
alter table music_download_job drop column media_file_id;
alter table music_download_job drop column radio_item_id;
alter table music_download_job drop column priority;
alter table music_download_job drop column origin;
drop table if exists radio_track_feedback;
drop table if exists discovery_track;
drop table if exists personal_radio_item;
drop table if exists personal_radio_session;
drop table if exists playlist_play_history;
