-- +goose Up
create table recommendation_taste_affinity
(
    user_id          varchar(255) not null references user (id) on delete cascade,
    entity_type      varchar(32)  not null,
    entity_key       varchar(512) not null,
    score            real         not null,
    positive_weight  real         not null default 0,
    negative_weight  real         not null default 0,
    last_evidence_at datetime,
    updated_at       datetime     not null,
    primary key (user_id, entity_type, entity_key)
);

create index recommendation_taste_affinity_lookup
    on recommendation_taste_affinity (user_id, entity_type, score desc);

-- +goose Down
drop index if exists recommendation_taste_affinity_lookup;
drop table if exists recommendation_taste_affinity;
