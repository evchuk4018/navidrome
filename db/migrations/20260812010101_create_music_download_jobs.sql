-- +goose Up
create table music_download_job
(
    id           varchar(255) not null primary key,
    user_id      varchar(255) not null references user (id) on delete cascade,
    kind         varchar(32)  not null,
    source_id    varchar(255) not null,
    artist       varchar(255) default '' not null,
    album        varchar(255) default '' not null,
    title        varchar(255) default '' not null,
    status       varchar(32)  not null,
    message      varchar(1024) default '' not null,
    error        varchar(4096) default '' not null,
    output_path  varchar(1024) default '' not null,
    completed    integer default 0 not null,
    total        integer default 0 not null,
    created_at   datetime not null,
    updated_at   datetime not null,
    started_at   datetime,
    finished_at  datetime
);

create index music_download_job_user_created
    on music_download_job (user_id, created_at);

create index music_download_job_status_created
    on music_download_job (status, created_at);

-- +goose Down
drop table music_download_job;
