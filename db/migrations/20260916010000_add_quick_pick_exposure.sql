-- +goose Up
create table quick_pick_exposure
(
    user_id       varchar(255) not null references user (id) on delete cascade,
    item_key      varchar(512) not null,
    show_count    integer not null default 0,
    last_shown_at datetime not null,
    primary key (user_id, item_key)
);

create index quick_pick_exposure_user_last_shown
    on quick_pick_exposure (user_id, last_shown_at desc);

-- +goose Down
drop index if exists quick_pick_exposure_user_last_shown;
drop table if exists quick_pick_exposure;
