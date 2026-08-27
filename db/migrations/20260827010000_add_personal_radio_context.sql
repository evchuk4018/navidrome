-- +goose Up
alter table personal_radio_session
    add column mode varchar(32) not null default 'balanced';

alter table personal_radio_item
    add column playback_outcome varchar(32) not null default '';
alter table personal_radio_item
    add column listened_ms integer not null default 0;
alter table personal_radio_item
    add column duration_ms integer not null default 0;
alter table personal_radio_item
    add column transition_source_item_id varchar(255);
alter table personal_radio_item
    add column transition_source_key varchar(512) not null default '';
alter table personal_radio_item
    add column last_feedback_at datetime;

create index personal_radio_item_session_feedback
    on personal_radio_item (session_id, playback_outcome, last_feedback_at desc);

create table radio_transition_feedback
(
    user_id               varchar(255) not null references user (id) on delete cascade,
    source_key            varchar(512) not null,
    target_key            varchar(512) not null,
    source_media_file_id  varchar(255),
    target_media_file_id  varchar(255),
    attempt_count         integer not null default 0,
    accepted_count        integer not null default 0,
    completed_count       integer not null default 0,
    early_skip_count      integer not null default 0,
    neutral_skip_count    integer not null default 0,
    keep_count            integer not null default 0,
    last_attempt_at       datetime,
    last_positive_at      datetime,
    last_negative_at      datetime,
    updated_at            datetime not null,
    primary key (user_id, source_key, target_key)
);

create index radio_transition_feedback_source_idx
    on radio_transition_feedback (user_id, source_key);

-- +goose Down
drop index if exists radio_transition_feedback_source_idx;
drop table if exists radio_transition_feedback;
drop index if exists personal_radio_item_session_feedback;
alter table personal_radio_item drop column last_feedback_at;
alter table personal_radio_item drop column transition_source_key;
alter table personal_radio_item drop column transition_source_item_id;
alter table personal_radio_item drop column duration_ms;
alter table personal_radio_item drop column listened_ms;
alter table personal_radio_item drop column playback_outcome;
alter table personal_radio_session drop column mode;
