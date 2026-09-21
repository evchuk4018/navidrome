-- +goose Up
-- Keep the representative seed columns for old clients while recording the
-- source and request identity needed to make session creation idempotent.
alter table personal_radio_session add column source_type varchar(32) not null default 'song';
alter table personal_radio_session add column source_id varchar(255) not null default '';
alter table personal_radio_session add column source_playlist_id varchar(255) not null default '';
alter table personal_radio_session add column client_request_id varchar(255) not null default '';
alter table personal_radio_session add column seed_media_file_ids text not null default '';
alter table personal_radio_session add column seed_media_file_weights text not null default '';
alter table personal_radio_session add column revision integer not null default 1;
alter table personal_radio_session add column autoplay integer not null default 1;

update personal_radio_session
set source_type = case when source_playlist_id <> '' then 'playlist' else 'song' end,
    source_id = case when source_playlist_id <> '' then source_playlist_id else seed_media_file_id end,
    seed_media_file_ids = '["' || seed_media_file_id || '"]',
    seed_media_file_weights = '[1]'
where source_id = '';

create unique index personal_radio_session_user_request
    on personal_radio_session (user_id, client_request_id)
    where client_request_id <> '';
create index personal_radio_session_user_revision
    on personal_radio_session (user_id, revision desc, updated_at desc);

-- Track feedback is keyed by RadioTrackKey. Rebuild the old table because its
-- legacy primary key (user_id, recording_mbid) collapses every media-only
-- track into recording_mbid=''. Existing MBID rows are copied under their
-- canonical mbid:* key while media:* rows can now coexist.
create table radio_track_feedback_continuity
(
    user_id            varchar(255) not null references user (id) on delete cascade,
    recording_mbid     varchar(255) not null default '',
    track_key          varchar(512) not null,
    positive_count     integer default 0 not null,
    completed_count    integer default 0 not null,
    neutral_skip_count integer default 0 not null,
    early_skip_count   integer default 0 not null,
    dislike_count      integer default 0 not null,
    unplayable_count   integer default 0 not null,
    last_early_skip_at datetime,
    suppressed_until   datetime,
    updated_at         datetime not null,
    primary key (user_id, track_key)
);

insert into radio_track_feedback_continuity
    (user_id, recording_mbid, track_key, positive_count, completed_count,
     neutral_skip_count, early_skip_count, last_early_skip_at, updated_at)
select user_id, recording_mbid,
       case when trim(recording_mbid) <> '' then 'mbid:' || lower(trim(recording_mbid))
            else 'media:legacy:' || user_id end,
       positive_count, completed_count, neutral_skip_count, early_skip_count,
       last_early_skip_at, updated_at
from radio_track_feedback;
drop table radio_track_feedback;
alter table radio_track_feedback_continuity rename to radio_track_feedback;

create unique index radio_track_feedback_user_track_key
    on radio_track_feedback (user_id, track_key)
    where track_key <> '';
create index radio_track_feedback_suppression
    on radio_track_feedback (user_id, suppressed_until);

-- +goose Down
drop index if exists radio_track_feedback_suppression;
drop index if exists radio_track_feedback_user_track_key;
create table radio_track_feedback_legacy
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
insert into radio_track_feedback_legacy
    (user_id, recording_mbid, positive_count, completed_count, neutral_skip_count,
     early_skip_count, last_early_skip_at, updated_at)
select user_id, substr(track_key, 6), positive_count, completed_count,
       neutral_skip_count, early_skip_count, last_early_skip_at, updated_at
from radio_track_feedback
where track_key like 'mbid:%';
drop table radio_track_feedback;
alter table radio_track_feedback_legacy rename to radio_track_feedback;
drop index if exists personal_radio_session_user_revision;
drop index if exists personal_radio_session_user_request;
alter table personal_radio_session drop column autoplay;
alter table personal_radio_session drop column revision;
alter table personal_radio_session drop column seed_media_file_ids;
alter table personal_radio_session drop column seed_media_file_weights;
alter table personal_radio_session drop column client_request_id;
alter table personal_radio_session drop column source_playlist_id;
alter table personal_radio_session drop column source_id;
alter table personal_radio_session drop column source_type;
