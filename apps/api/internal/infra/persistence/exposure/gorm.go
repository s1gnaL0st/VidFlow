package infraexposure

import (
	domainexposure "GCFeed/internal/domain/exposure"
	domainoutbox "GCFeed/internal/domain/outbox"
	domainvideo "GCFeed/internal/domain/video"
	infraoutbox "GCFeed/internal/infra/persistence/outbox"
	infravideo "GCFeed/internal/infra/persistence/video"
	"context"
	"errors"

	"github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// SaveViewEvent 写入观看行为，并在 exposed 事件时 upsert 用户视频曝光聚合。
func (r *Repository) SaveViewEvent(ctx context.Context, event *domainexposure.ViewEvent) (*domainexposure.ViewEvent, *domainexposure.Exposure, error) {
	var savedEvent *domainexposure.ViewEvent
	var exposure *domainexposure.Exposure
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		savedEvent, exposure, err = saveViewEvent(tx, event)
		return err
	})
	if err != nil {
		return nil, nil, mapExposureError(err)
	}
	return savedEvent, exposure, nil
}

// SaveViewEventWithOutbox 在同一事务内写入观看事实和待投递事件。
func (r *Repository) SaveViewEventWithOutbox(
	ctx context.Context,
	event *domainexposure.ViewEvent,
	factory func(*domainexposure.ViewEvent, *domainexposure.Exposure) (*domainoutbox.Event, error),
) (*domainexposure.ViewEvent, *domainexposure.Exposure, error) {
	var savedEvent *domainexposure.ViewEvent
	var exposure *domainexposure.Exposure
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		savedEvent, exposure, err = saveViewEvent(tx, event)
		if err != nil {
			return err
		}
		outboxEvent, err := factory(savedEvent, exposure)
		if err != nil {
			return err
		}
		if outboxEvent == nil {
			return domainoutbox.ErrInvalidEvent
		}
		model := infraoutbox.ModelFromEvent(outboxEvent)
		return tx.Create(&model).Error
	})
	if err != nil {
		return nil, nil, mapExposureError(err)
	}
	return savedEvent, exposure, nil
}

func saveViewEvent(tx *gorm.DB, event *domainexposure.ViewEvent) (*domainexposure.ViewEvent, *domainexposure.Exposure, error) {
	var eventModel ViewEventModel
	var exposureModel ExposureModel
	if err := ensurePublishedVideo(tx, event.VideoID); err != nil {
		return nil, nil, err
	}

	eventModel = ViewEventModel{
		UserID:    event.UserID,
		VideoID:   event.VideoID,
		Scene:     event.Scene,
		RequestID: stringPtr(event.RequestID),
		EventType: event.EventType,
		WatchMs:   event.WatchMs,
		Completed: event.Completed,
	}
	if err := tx.Create(&eventModel).Error; err != nil {
		return nil, nil, err
	}

	if !event.CountsAsExposure() {
		return restoreViewEvent(eventModel), nil, nil
	}

	exposureModel = ExposureModel{
		UserID:         event.UserID,
		VideoID:        event.VideoID,
		FirstExposedAt: eventModel.CreatedAt,
		LastExposedAt:  eventModel.CreatedAt,
		ExposureCount:  1,
		LastScene:      event.Scene,
	}
	if err := tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "user_id"},
			{Name: "video_id"},
		},
		DoUpdates: clause.Assignments(map[string]any{
			"last_exposed_at": gorm.Expr("VALUES(last_exposed_at)"),
			"exposure_count":  gorm.Expr("exposure_count + 1"),
			"last_scene":      gorm.Expr("VALUES(last_scene)"),
			"updated_at":      gorm.Expr("VALUES(updated_at)"),
		}),
	}).Create(&exposureModel).Error; err != nil {
		return nil, nil, err
	}

	if err := tx.Where("user_id = ? AND video_id = ?", event.UserID, event.VideoID).Take(&exposureModel).Error; err != nil {
		return nil, nil, err
	}
	return restoreViewEvent(eventModel), restoreExposure(exposureModel), nil
}

func ensurePublishedVideo(tx *gorm.DB, videoID int64) error {
	var video infravideo.VideoModel
	err := tx.Select("id").
		Where("id = ? AND status = ?", videoID, domainvideo.StatusPublished).
		Take(&video).
		Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domainexposure.ErrVideoNotFound
		}
		return err
	}
	return nil
}

func restoreViewEvent(model ViewEventModel) *domainexposure.ViewEvent {
	return domainexposure.RestoreViewEvent(
		model.ID,
		model.UserID,
		model.VideoID,
		model.Scene,
		stringValue(model.RequestID),
		model.EventType,
		model.WatchMs,
		model.Completed,
		model.CreatedAt,
	)
}

func restoreExposure(model ExposureModel) *domainexposure.Exposure {
	return domainexposure.RestoreExposure(
		model.ID,
		model.UserID,
		model.VideoID,
		model.FirstExposedAt,
		model.LastExposedAt,
		model.ExposureCount,
		model.LastScene,
		model.CreatedAt,
		model.UpdatedAt,
	)
}

func stringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func mapExposureError(err error) error {
	if errors.Is(err, domainexposure.ErrVideoNotFound) {
		return err
	}
	if isDuplicateKeyError(err) {
		return err
	}
	return err
}

func isDuplicateKeyError(err error) bool {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}
