-- +goose Up
-- A view token scopes the visible tile set. Keeping one detail row per
-- user/view/item makes impression retries idempotent while the aggregate table
-- remains compact for ranking queries.
create table quick_pick_impression
(
    user_id  varchar(255) not null references user (id) on delete cascade,
    view_id  varchar(255) not null,
    item_key varchar(512) not null,
    shown_at datetime not null,
    primary key (user_id, view_id, item_key)
);

create index quick_pick_impression_user_view
    on quick_pick_impression (user_id, view_id);

-- +goose Down
drop index if exists quick_pick_impression_user_view;
drop table if exists quick_pick_impression;
