package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"

	"zhiwellcare/backend/internal/model"
)

// ---------- 设备型号 / 游戏目录（内存演示） ----------

func (m *Memory) ListDeviceModels(_ context.Context, includeOff bool) ([]model.DeviceModel, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make([]model.DeviceModel, 0, len(m.devices))
	for _, item := range m.devices {
		if !includeOff && item.Status != "on" {
			continue
		}
		items = append(items, *item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ModelID < items[j].ModelID })
	return items, nil
}

func (m *Memory) GetDeviceModel(_ context.Context, modelID string) (*model.DeviceModel, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if item := m.devices[modelID]; item != nil {
		copyItem := *item
		return &copyItem, nil
	}
	return nil, ErrNotFound
}

func (m *Memory) UpsertDeviceModel(_ context.Context, item model.DeviceModel) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if item.Status == "" {
		item.Status = "on"
	}
	// 与 PostgreSQL 的 jsonb NOT NULL DEFAULT '{}' 行为保持一致
	if len(item.Config) == 0 {
		item.Config = json.RawMessage(`{}`)
	}
	item.CreatedAt = time.Now()
	item.UpdatedAt = time.Now()
	m.devices[item.ModelID] = &item
	return nil
}

func (m *Memory) DeleteDeviceModel(_ context.Context, modelID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.devices[modelID]; !exists {
		return ErrNotFound
	}
	delete(m.devices, modelID)
	return nil
}

func (m *Memory) SetDeviceModelStatus(_ context.Context, modelID, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	item := m.devices[modelID]
	if item == nil {
		return ErrNotFound
	}
	item.Status = status
	item.UpdatedAt = time.Now()
	return nil
}

func (m *Memory) ListGames(_ context.Context, includeOff bool) ([]model.GameCatalogItem, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make([]model.GameCatalogItem, 0, len(m.games))
	for _, item := range m.games {
		if !includeOff && item.Status != "on" {
			continue
		}
		items = append(items, *item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].GameID < items[j].GameID })
	return items, nil
}

func (m *Memory) GetGame(_ context.Context, gameID string) (*model.GameCatalogItem, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if item := m.games[gameID]; item != nil {
		copyItem := *item
		return &copyItem, nil
	}
	return nil, ErrNotFound
}

func (m *Memory) UpsertGame(_ context.Context, item model.GameCatalogItem) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if item.Status == "" {
		item.Status = "off"
	}
	// 与 PostgreSQL 的 jsonb NOT NULL DEFAULT '{}' 行为保持一致
	if len(item.Config) == 0 {
		item.Config = json.RawMessage(`{}`)
	}
	item.CreatedAt = time.Now()
	item.UpdatedAt = time.Now()
	m.games[item.GameID] = &item
	return nil
}

func (m *Memory) DeleteGame(_ context.Context, gameID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.games[gameID]; !exists {
		return ErrNotFound
	}
	delete(m.games, gameID)
	return nil
}

func (m *Memory) SetGameStatus(_ context.Context, gameID, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	item := m.games[gameID]
	if item == nil {
		return ErrNotFound
	}
	item.Status = status
	item.UpdatedAt = time.Now()
	return nil
}

// ---------- 训练摘要（内存演示） ----------

// SaveTrainingRecord 落训练摘要。
//
// 幂等语义与 PostgreSQL 分区表保持一致：同 (recordId, completedAt) 重复上报只保留 1 行
// （分区表复合主键 (record_id, completed_at) + ON CONFLICT DO NOTHING）。
func (m *Memory) SaveTrainingRecord(_ context.Context, record model.TrainingSummary) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, existing := range m.records {
		if existing.RecordID == record.RecordID && existing.CompletedAt.Equal(record.CompletedAt) {
			return nil
		}
	}
	m.nextRecord++
	record.ID = m.nextRecord
	record.UploadedAt = time.Now()
	m.records = append(m.records, record)
	return nil
}

func (m *Memory) ListTrainingRecords(_ context.Context, filter TrainingRecordFilter) ([]model.TrainingSummary, int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var matched []model.TrainingSummary
	for _, record := range m.records {
		if filter.UserID != "" && (record.UserID == nil || *record.UserID != filter.UserID) {
			continue
		}
		if filter.GameID != "" && record.GameID != filter.GameID {
			continue
		}
		if filter.ModelID != "" && record.DeviceModelID != filter.ModelID {
			continue
		}
		matched = append(matched, record)
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].CompletedAt.After(matched[j].CompletedAt) })
	page, size := normalizePage(filter.Page, filter.PageSize)
	start := (page - 1) * size
	if start > len(matched) {
		start = len(matched)
	}
	end := start + size
	if end > len(matched) {
		end = len(matched)
	}
	return matched[start:end], int64(len(matched)), nil
}

// GetTrainingRecord 按 recordId 返回单条摘要（同 id 多条时取完成时间最新的一条）；
// 不存在返回 ErrNotFound。
func (m *Memory) GetTrainingRecord(_ context.Context, recordID string) (*model.TrainingSummary, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var found *model.TrainingSummary
	for index := range m.records {
		record := m.records[index]
		if record.RecordID != recordID {
			continue
		}
		if found == nil || record.CompletedAt.After(found.CompletedAt) {
			copyRecord := record
			found = &copyRecord
		}
	}
	if found == nil {
		return nil, ErrNotFound
	}
	return found, nil
}

func (m *Memory) DashboardStats(_ context.Context) (*model.DashboardStats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	stats := &model.DashboardStats{
		DeviceModels: int64(len(m.devices)),
		Games:        int64(len(m.games)),
		Users:        int64(len(m.users)),
		TrainingRecs: int64(len(m.records)),
	}
	today := time.Now().Truncate(24 * time.Hour)
	for _, record := range m.records {
		if record.CompletedAt.After(today) {
			stats.TodayRecs++
		}
	}
	return stats, nil
}

// ---------- OAuth（内存演示） ----------

func (m *Memory) FindUserByOpenID(_ context.Context, provider, openid string) (*model.User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	userID := m.oauth[provider+"|"+openid]
	if userID == "" {
		return nil, ErrUserNotFound
	}
	return cloneUser(m.byID[userID]), nil
}

func (m *Memory) FindUserByUnionID(_ context.Context, provider, unionid string) (*model.User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	userID := m.oauthUnion[provider+"|"+unionid]
	if userID == "" {
		return nil, ErrUserNotFound
	}
	return cloneUser(m.byID[userID]), nil
}

func (m *Memory) GetOrCreateByOAuth(_ context.Context, params OAuthParams) (*model.User, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if params.UnionID != nil && *params.UnionID != "" {
		if userID := m.oauthUnion[params.Provider+"|"+*params.UnionID]; userID != "" {
			return cloneUser(m.byID[userID]), false, nil
		}
	}
	if userID := m.oauth[params.Provider+"|"+params.OpenID]; userID != "" {
		return cloneUser(m.byID[userID]), false, nil
	}
	raw := sha256.Sum256([]byte(params.Provider + params.OpenID + time.Now().String()))
	user := &model.User{
		ID:           hex.EncodeToString(raw[:12]),
		PasswordHash: "",
		Nickname:     params.Nickname,
		Role:         "user",
		Status:       1,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	m.byID[user.ID] = user
	m.oauth[params.Provider+"|"+params.OpenID] = user.ID
	if params.UnionID != nil && *params.UnionID != "" {
		m.oauthUnion[params.Provider+"|"+*params.UnionID] = user.ID
	}
	return cloneUser(user), true, nil
}

func (m *Memory) BindPhoneToUser(_ context.Context, userID, phone string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, user := range m.users {
		if user.Phone == phone {
			return ErrPhoneExists
		}
	}
	user := m.byID[userID]
	if user == nil {
		return ErrUserNotFound
	}
	oldPhone := user.Phone
	user.Phone = phone
	user.UpdatedAt = time.Now()
	if oldPhone != "" {
		delete(m.users, oldPhone)
	}
	m.users[phone] = user
	return nil
}

var _ Combined = (*Memory)(nil)
