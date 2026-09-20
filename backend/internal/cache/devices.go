package cache

import (
	"context"
	"sort"
	"strings"
	"time"
)

// defaultDeviceTTL 设备映射/白名单默认缓存时长。
const defaultDeviceTTL = 5 * time.Minute

// Devices 提供设备相关的读缓存：
//
//  1. 设备标识 → 设备型号（modelId）映射：蓝牙名称/地址到型号的解析结果，
//     避免每次扫描连接都回源 PostgreSQL；
//  2. 设备白名单：允许接入的设备标识集合，PostgreSQL 为准，缓存加速；
//
// 两者都是「缓存加速、PG 为准」的读穿透结构，写路径请显式失效缓存。
type Devices struct {
	store Store
	ttl   time.Duration
}

func NewDevices(store Store, ttl time.Duration) *Devices {
	if ttl <= 0 {
		ttl = defaultDeviceTTL
	}
	return &Devices{store: store, ttl: ttl}
}

// normalizeDeviceKey 统一设备标识大小写与空白，避免同一设备命中不同缓存键。
func normalizeDeviceKey(deviceKey string) string {
	return strings.ToLower(strings.TrimSpace(deviceKey))
}

func mappingKey(deviceKey string) string {
	return Key("device", "mapping", normalizeDeviceKey(deviceKey))
}

const whitelistKeyName = "whitelist"

func whitelistKey() string {
	return Key("device", whitelistKeyName)
}

// Mapping 解析设备标识对应的型号：命中缓存直接返回，未命中调用 load 回源并写入。
// 返回 ok=false 表示「没有映射关系」（不会写空值缓存，避免脏命中）。
func (d *Devices) Mapping(ctx context.Context, deviceKey string, load func(context.Context) (string, error)) (string, bool, error) {
	key := normalizeDeviceKey(deviceKey)
	if key == "" {
		return "", false, nil
	}
	value, err := GetOrLoad(ctx, d.store, mappingKey(key), d.ttl, load)
	if err != nil {
		return "", false, err
	}
	return value, value != "", nil
}

// SaveMapping 写入映射缓存（后台维护映射关系后调用，随即生效）。
func (d *Devices) SaveMapping(ctx context.Context, deviceKey, modelID string) error {
	key := normalizeDeviceKey(deviceKey)
	if key == "" || modelID == "" {
		return nil
	}
	return d.store.Set(ctx, mappingKey(key), modelID, d.ttl)
}

// ForgetMapping 失效单个设备映射。
func (d *Devices) ForgetMapping(ctx context.Context, deviceKey string) error {
	return d.store.Del(ctx, mappingKey(deviceKey))
}

// Whitelist 返回白名单集合；未命中时调用 load 回源并缓存。
func (d *Devices) Whitelist(ctx context.Context, load func(context.Context) ([]string, error)) (map[string]struct{}, error) {
	var cached []string
	if ok, err := GetJSON(ctx, d.store, whitelistKey(), &cached); err == nil && ok {
		return toSet(cached), nil
	}
	items, err := load(ctx)
	if err != nil {
		return nil, err
	}
	// 排序后写缓存，便于排查与比对。
	sorted := make([]string, 0, len(items))
	for _, item := range items {
		if normalized := normalizeDeviceKey(item); normalized != "" {
			sorted = append(sorted, normalized)
		}
	}
	sort.Strings(sorted)
	if err := SetJSON(ctx, d.store, whitelistKey(), sorted, d.ttl); err != nil {
		// 缓存写失败不影响本次结果。
		return toSet(sorted), nil
	}
	return toSet(sorted), nil
}

// IsWhitelisted 判断设备是否在白名单内（白名单为空表示不限制）。
func (d *Devices) IsWhitelisted(ctx context.Context, deviceKey string, load func(context.Context) ([]string, error)) (bool, error) {
	set, err := d.Whitelist(ctx, load)
	if err != nil {
		return false, err
	}
	if len(set) == 0 {
		return true, nil
	}
	_, ok := set[normalizeDeviceKey(deviceKey)]
	return ok, nil
}

// ForgetWhitelist 失效白名单缓存（运营增删白名单后调用）。
func (d *Devices) ForgetWhitelist(ctx context.Context) error {
	return d.store.Del(ctx, whitelistKey())
}

func toSet(items []string) map[string]struct{} {
	out := make(map[string]struct{}, len(items))
	for _, item := range items {
		out[normalizeDeviceKey(item)] = struct{}{}
	}
	return out
}

// Kind 便于启动日志与自检输出当前后端。
func (d *Devices) Kind() string { return d.store.Kind() }
