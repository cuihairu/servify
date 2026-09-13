package usersecurity

import (
	"context"
	"fmt"
	"strings"
	"time"

	"servify/apps/server/internal/models"
	platformauth "servify/apps/server/internal/platform/auth"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type Service struct {
	db     *gorm.DB
	logger *logrus.Logger
}

func NewService(db *gorm.DB, logger *logrus.Logger) *Service {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &Service{db: db, logger: logger}
}

func (s *Service) RevokeTokens(ctx context.Context, userID uint) (int, error) {
	if s == nil || s.db == nil {
		return 0, gorm.ErrInvalidDB
	}
	if err := ensureScopedUserAccess(ctx, s.db, userID); err != nil {
		return 0, err
	}
	return RevokeUserTokens(ctx, s.db, userID, time.Now().UTC())
}

func (s *Service) BatchRevokeTokens(ctx context.Context, userIDs []uint) (map[uint]int, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	if len(userIDs) == 0 {
		return map[uint]int{}, nil
	}

	orderedIDs, err := orderedUniqueUserIDs(userIDs)
	if err != nil {
		return nil, err
	}
	allowed, err := scopedUserIDs(ctx, s.db, orderedIDs)
	if err != nil {
		return nil, err
	}
	if len(allowed) != len(orderedIDs) {
		missing := missingScopedUserIDs(orderedIDs, allowed, allowed)
		return nil, fmt.Errorf("user not found: %v", missing)
	}

	results := make(map[uint]int, len(userIDs))
	revokeAt := time.Now().UTC()
	for _, userID := range userIDs {
		version, err := RevokeUserTokens(ctx, s.db, userID, revokeAt)
		if err != nil {
			return nil, err
		}
		results[userID] = version
	}
	return results, nil
}

func (s *Service) GetUser(ctx context.Context, userID uint) (*models.User, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	if err := ensureScopedUserAccess(ctx, s.db, userID); err != nil {
		return nil, err
	}
	var user models.User
	if err := s.db.WithContext(ctx).First(&user, userID).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func (s *Service) GetUsers(ctx context.Context, userIDs []uint) ([]models.User, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	if len(userIDs) == 0 {
		return []models.User{}, nil
	}

	orderedIDs, err := orderedUniqueUserIDs(userIDs)
	if err != nil {
		return nil, err
	}
	allowed, err := scopedUserIDs(ctx, s.db, orderedIDs)
	if err != nil {
		return nil, err
	}

	var users []models.User
	if err := s.db.WithContext(ctx).Where("id IN ?", orderedIDs).Find(&users).Error; err != nil {
		return nil, err
	}
	found := make(map[uint]struct{}, len(users))
	for _, user := range users {
		found[user.ID] = struct{}{}
	}
	if len(users) != len(orderedIDs) || len(allowed) != len(orderedIDs) {
		missing := missingScopedUserIDs(orderedIDs, allowed, found)
		return nil, fmt.Errorf("user not found: %v", missing)
	}

	byID := make(map[uint]models.User, len(users))
	for _, user := range users {
		byID[user.ID] = user
	}

	orderedUsers := make([]models.User, 0, len(userIDs))
	for _, userID := range userIDs {
		// byID 必然包含 userIDs 的每个元素:上面 len(users) == len(orderedIDs)
		// 的穷举校验已通过,且 orderedUniqueUserIDs 不增删元素,查询又限定在
		// orderedIDs 内,故此处无需 ok 兜底分支。
		orderedUsers = append(orderedUsers, byID[userID])
	}
	return orderedUsers, nil
}

func (s *Service) ListUserSessions(ctx context.Context, userID uint) ([]models.UserAuthSession, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	if err := ensureScopedUserAccess(ctx, s.db, userID); err != nil {
		return nil, err
	}

	var sessions []models.UserAuthSession
	if err := s.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("updated_at desc, created_at desc").
		Find(&sessions).Error; err != nil {
		return nil, err
	}
	return sessions, nil
}

func (s *Service) RevokeSession(ctx context.Context, userID uint, sessionID string) (*models.UserAuthSession, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	if err := ensureScopedUserAccess(ctx, s.db, userID); err != nil {
		return nil, err
	}
	if sessionID == "" {
		return nil, fmt.Errorf("session_id required")
	}

	now := time.Now().UTC()
	result := s.db.WithContext(ctx).Model(&models.UserAuthSession{}).
		Where("id = ? AND user_id = ?", sessionID, userID).
		Updates(map[string]interface{}{
			"status":        "revoked",
			"revoked_at":    now,
			"token_version": gorm.Expr("COALESCE(token_version, 0) + 1"),
		})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, fmt.Errorf("session not found")
	}

	var session models.UserAuthSession
	if err := s.db.WithContext(ctx).First(&session, "id = ? AND user_id = ?", sessionID, userID).Error; err != nil {
		return nil, err
	}
	return &session, nil
}

type RevokedTokenResult struct {
	JTI       string
	UserID    uint
	SessionID string
	TokenUse  string
	Reason    string
	ExpiresAt *time.Time
	RevokedAt time.Time
}

type RevokeSessionsResult struct {
	Count    int
	Sessions []models.UserAuthSession
}

func (s *Service) RevokeJWT(ctx context.Context, rawToken, secret, reason string) (*RevokedTokenResult, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	rawToken = strings.TrimSpace(rawToken)
	secret = strings.TrimSpace(secret)
	if rawToken == "" {
		return nil, fmt.Errorf("token required")
	}
	if secret == "" {
		return nil, fmt.Errorf("jwt secret required")
	}

	payload, err := (platformauth.Validator{Secret: secret}).ValidateToken(rawToken)
	if err != nil {
		return nil, fmt.Errorf("invalid token: %w", err)
	}

	jti, ok := stringClaim(payload, "jti", "token_id")
	if !ok || jti == "" {
		return nil, fmt.Errorf("token missing jti")
	}
	userID, _ := uintClaim(payload, "user_id", "sub")
	if hasRequestScope(ctx) {
		if userID == 0 {
			return nil, gorm.ErrRecordNotFound
		}
		if err := ensureScopedUserAccess(ctx, s.db, userID); err != nil {
			return nil, err
		}
	}
	sessionID, _ := stringClaim(payload, "session_id", "sid")
	tokenUse, _ := stringClaim(payload, "token_use")
	exp, hasExp := unixTimeClaim(payload, "exp")
	now := time.Now().UTC()
	var expiresAt *time.Time
	if hasExp {
		expUTC := exp.UTC()
		expiresAt = &expUTC
	}

	record := &models.RevokedToken{
		JTI:       jti,
		UserID:    userID,
		SessionID: sessionID,
		TokenUse:  tokenUse,
		Reason:    strings.TrimSpace(reason),
		ExpiresAt: expiresAt,
		RevokedAt: now,
	}
	if err := s.db.WithContext(ctx).Where("jti = ?", jti).FirstOrCreate(record).Error; err != nil {
		return nil, err
	}
	if strings.TrimSpace(reason) != "" {
		if err := s.db.WithContext(ctx).Model(record).Updates(map[string]any{
			"reason":     strings.TrimSpace(reason),
			"revoked_at": now,
		}).Error; err != nil {
			return nil, err
		}
		record.Reason = strings.TrimSpace(reason)
		record.RevokedAt = now
	}
	return &RevokedTokenResult{
		JTI:       record.JTI,
		UserID:    record.UserID,
		SessionID: record.SessionID,
		TokenUse:  record.TokenUse,
		Reason:    record.Reason,
		ExpiresAt: record.ExpiresAt,
		RevokedAt: record.RevokedAt,
	}, nil
}

func (s *Service) RevokeAllSessions(ctx context.Context, userID uint, exceptSessionID string) (*RevokeSessionsResult, error) {
	if s == nil || s.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	if err := ensureScopedUserAccess(ctx, s.db, userID); err != nil {
		return nil, err
	}

	query := s.db.WithContext(ctx).Model(&models.UserAuthSession{}).
		Where("user_id = ?", userID).
		Where("status = ?", "active").
		Where("revoked_at IS NULL")
	if exceptSessionID = strings.TrimSpace(exceptSessionID); exceptSessionID != "" {
		query = query.Where("id <> ?", exceptSessionID)
	}

	var before []models.UserAuthSession
	if err := query.Order("updated_at desc, created_at desc").Find(&before).Error; err != nil {
		return nil, err
	}
	if len(before) == 0 {
		return &RevokeSessionsResult{Count: 0, Sessions: []models.UserAuthSession{}}, nil
	}

	ids := make([]string, 0, len(before))
	for _, session := range before {
		ids = append(ids, session.ID)
	}

	now := time.Now().UTC()
	if err := s.db.WithContext(ctx).Model(&models.UserAuthSession{}).
		Where("id IN ?", ids).
		Updates(map[string]any{
			"status":        "revoked",
			"revoked_at":    now,
			"token_version": gorm.Expr("COALESCE(token_version, 0) + 1"),
		}).Error; err != nil {
		return nil, err
	}

	var after []models.UserAuthSession
	if err := s.db.WithContext(ctx).
		Where("id IN ?", ids).
		Order("updated_at desc, created_at desc").
		Find(&after).Error; err != nil {
		return nil, err
	}

	return &RevokeSessionsResult{
		Count:    len(after),
		Sessions: after,
	}, nil
}

func stringClaim(payload map[string]interface{}, keys ...string) (string, bool) {
	for _, key := range keys {
		value, ok := payload[key]
		if !ok || value == nil {
			continue
		}
		if typed, ok := value.(string); ok {
			typed = strings.TrimSpace(typed)
			if typed != "" {
				return typed, true
			}
		}
	}
	return "", false
}

func uintClaim(payload map[string]interface{}, keys ...string) (uint, bool) {
	for _, key := range keys {
		value, ok := payload[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case float64:
			return uint(typed), true
		}
	}
	return 0, false
}

func unixTimeClaim(payload map[string]interface{}, keys ...string) (time.Time, bool) {
	for _, key := range keys {
		value, ok := payload[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case float64:
			return time.Unix(int64(typed), 0), true
		}
	}
	return time.Time{}, false
}
