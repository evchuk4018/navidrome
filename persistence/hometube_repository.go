package persistence

import (
	"context"
	"fmt"
	"strings"
	"time"

	. "github.com/Masterminds/squirrel"
	"github.com/deluan/rest"
	"github.com/navidrome/navidrome/model"
	"github.com/pocketbase/dbx"
)

type homeTubeRepository struct{ sqlRepository }

func NewHomeTubeRepository(ctx context.Context, builder dbx.Builder) model.HomeTubeVideoRepository {
	r := &homeTubeRepository{sqlRepository: sqlRepository{ctx: ctx, db: builder, tableName: "hometube_video"}}
	r.registerModel(&model.HomeTubeVideo{}, nil)
	return r
}

func (r *homeTubeRepository) Get(id string) (*model.HomeTubeVideo, error) {
	var video model.HomeTubeVideo
	err := r.queryOne(r.newSelect().Columns("hometube_video.*", "coalesce(annotation.starred,0) as starred").
		LeftJoin("annotation ON annotation.item_id = hometube_video.id AND annotation.item_type = 'hometube_video' AND annotation.user_id = ?", loggedUser(r.ctx).ID).
		Where(Eq{"hometube_video.id": id}), &video)
	return &video, err
}

func (r *homeTubeRepository) Put(video *model.HomeTubeVideo) error {
	if strings.TrimSpace(video.ID) == "" || len(video.ID) > 255 || strings.TrimSpace(video.Title) == "" || video.DurationSeconds < 0 {
		return fmt.Errorf("invalid video metadata")
	}
	_, err := r.put(video.ID, video)
	return err
}

func (r *homeTubeRepository) SetFavorite(id string, favorite bool) error {
	if loggedUser(r.ctx).ID == invalidUserId {
		return model.ErrNotAuthorized
	}
	_, err := r.executeSQL(Expr(`INSERT INTO annotation(item_id,item_type,user_id,starred,starred_at) VALUES (?,'hometube_video',?,?,?) ON CONFLICT(item_id,item_type,user_id) DO UPDATE SET starred=excluded.starred,starred_at=excluded.starred_at`, id, loggedUser(r.ctx).ID, favorite, time.Now()))
	return err
}

func (r *homeTubeRepository) Read(id string) (any, error) { return r.Get(id) }
func (r *homeTubeRepository) ReadAll(...rest.QueryOptions) (any, error) {
	return []model.HomeTubeVideo{}, nil
}
func (r *homeTubeRepository) Count(...rest.QueryOptions) (int64, error) { return 0, nil }
func (r *homeTubeRepository) EntityName() string                        { return "hometube_video" }
func (r *homeTubeRepository) NewInstance() any                          { return &model.HomeTubeVideo{} }
