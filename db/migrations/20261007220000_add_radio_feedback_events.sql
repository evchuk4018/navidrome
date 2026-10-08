-- +goose Up
create table radio_feedback_event
(
    user_id     varchar(255) not null references user (id) on delete cascade,
    event_id    varchar(255) not null,
    session_id  varchar(255) not null references personal_radio_session (id) on delete cascade,
    item_id     varchar(255) not null references personal_radio_item (id) on delete cascade,
    event       varchar(255) not null,
    listened_ms integer not null,
    duration_ms integer not null,
    created_at  datetime not null,
    primary key (user_id, event_id)
);
create index radio_feedback_event_session on radio_feedback_event (session_id);
create index radio_feedback_event_item on radio_feedback_event (item_id);

-- +goose Down
drop table radio_feedback_event;
