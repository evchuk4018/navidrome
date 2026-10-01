-- +goose Up
alter table quick_pick_impression add column clicked_at datetime;

create index quick_pick_impression_user_latest
    on quick_pick_impression (user_id, shown_at desc, view_id desc);

-- +goose Down
drop index if exists quick_pick_impression_user_latest;
alter table quick_pick_impression drop column clicked_at;
