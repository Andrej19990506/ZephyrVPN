package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisClient обертка над Redis клиентом для удобной работы
type RedisClient struct {
	client *redis.Client
	ctx    context.Context
}

// NewRedisClient создает новый Redis клиент
func NewRedisClient(client *redis.Client) *RedisClient {
	return &RedisClient{
		client: client,
		ctx:    context.Background(),
	}
}

// Set сохраняет значение с TTL
func (r *RedisClient) Set(key string, value interface{}, ttl time.Duration) error {
	var data string
	switch v := value.(type) {
	case string:
		data = v
	default:
		jsonData, err := json.Marshal(value)
		if err != nil {
			return err
		}
		data = string(jsonData)
	}

	return r.client.Set(r.ctx, key, data, ttl).Err()
}

// Get получает значение
func (r *RedisClient) Get(key string) (string, error) {
	return r.client.Get(r.ctx, key).Result()
}

// GetJSON получает и парсит JSON значение
func (r *RedisClient) GetJSON(key string, dest interface{}) error {
	data, err := r.client.Get(r.ctx, key).Result()
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(data), dest)
}

// Delete удаляет ключ
func (r *RedisClient) Delete(key string) error {
	return r.client.Del(r.ctx, key).Err()
}

// Exists проверяет существование ключа
func (r *RedisClient) Exists(key string) (bool, error) {
	count, err := r.client.Exists(r.ctx, key).Result()
	return count > 0, err
}

// Increment увеличивает значение на 1
func (r *RedisClient) Increment(key string) (int64, error) {
	return r.client.Incr(r.ctx, key).Result()
}

func (r *RedisClient) Decrement(key string) (int64, error) {
	return r.client.Decr(r.ctx, key).Result()
}

// SetNX устанавливает значение только если ключ не существует
func (r *RedisClient) SetNX(key string, value interface{}, ttl time.Duration) (bool, error) {
	var data string
	switch v := value.(type) {
	case string:
		data = v
	default:
		jsonData, err := json.Marshal(value)
		if err != nil {
			return false, err
		}
		data = string(jsonData)
	}

	return r.client.SetNX(r.ctx, key, data, ttl).Result()
}

// Expire устанавливает TTL для существующего ключа
func (r *RedisClient) Expire(key string, ttl time.Duration) error {
	return r.client.Expire(r.ctx, key, ttl).Err()
}

// LPush добавляет элемент в начало списка
func (r *RedisClient) LPush(key string, value interface{}) error {
	var data string
	switch v := value.(type) {
	case string:
		data = v
	default:
		jsonData, err := json.Marshal(value)
		if err != nil {
			return err
		}
		data = string(jsonData)
	}
	return r.client.LPush(r.ctx, key, data).Err()
}

// LRange получает элементы списка
func (r *RedisClient) LRange(key string, start, stop int64) ([]string, error) {
	return r.client.LRange(r.ctx, key, start, stop).Result()
}

// Keys получает все ключи по паттерну
func (r *RedisClient) Keys(pattern string) ([]string, error) {
	return r.client.Keys(r.ctx, pattern).Result()
}

// SAdd добавляет элемент в множество
func (r *RedisClient) SAdd(key string, members ...interface{}) error {
	return r.client.SAdd(r.ctx, key, members...).Err()
}

// SCard получает количество элементов в множестве
func (r *RedisClient) SCard(key string) (int64, error) {
	return r.client.SCard(r.ctx, key).Result()
}

// SIsMember проверяет принадлежность элемента множеству
func (r *RedisClient) SIsMember(key string, member interface{}) (bool, error) {
	return r.client.SIsMember(r.ctx, key, member).Result()
}

// SRem удаляет элемент из множества
func (r *RedisClient) SRem(key string, members ...interface{}) error {
	return r.client.SRem(r.ctx, key, members...).Err()
}

// SMembers получает все элементы множества
func (r *RedisClient) SMembers(key string) ([]string, error) {
	return r.client.SMembers(r.ctx, key).Result()
}

// RPush добавляет элемент в конец списка
func (r *RedisClient) RPush(key string, value interface{}) error {
	var data string
	switch v := value.(type) {
	case string:
		data = v
	default:
		jsonData, err := json.Marshal(value)
		if err != nil {
			return err
		}
		data = string(jsonData)
	}
	return r.client.RPush(r.ctx, key, data).Err()
}

// LLen получает длину списка
func (r *RedisClient) LLen(key string) (int64, error) {
	return r.client.LLen(r.ctx, key).Result()
}

// BRPop блокирующее получение элемента из конца списка (BRPOP)
// timeout в секундах, 0 = блокировать бесконечно
func (r *RedisClient) BRPop(key string, timeout time.Duration) (string, error) {
	result, err := r.client.BRPop(r.ctx, timeout, key).Result()
	if err != nil {
		return "", err
	}
	if len(result) < 2 {
		return "", fmt.Errorf("invalid BRPop result")
	}
	return result[1], nil
}

// Pipeline возвращает Pipeline для батчевого выполнения команд
func (r *RedisClient) Pipeline() redis.Pipeliner {
	return r.client.Pipeline()
}

// Context возвращает контекст для использования в Pipeline
func (r *RedisClient) Context() context.Context {
	return r.ctx
}

// GetClient возвращает прямой доступ к redis.Client (для Lua scripts и специальных операций)
func (r *RedisClient) GetClient() *redis.Client {
	return r.client
}

// SetBytes сохраняет бинарные данные (для Protobuf)
func (r *RedisClient) SetBytes(key string, value []byte, ttl time.Duration) error {
	return r.client.Set(r.ctx, key, value, ttl).Err()
}

// GetBytes получает бинарные данные (для Protobuf)
func (r *RedisClient) GetBytes(key string) ([]byte, error) {
	return r.client.Get(r.ctx, key).Bytes()
}

// Publish публикует сообщение в канал (Pub/Sub)
func (r *RedisClient) Publish(channel string, message string) error {
	return r.client.Publish(r.ctx, channel, message).Err()
}

// Subscribe подписывается на канал и возвращает канал сообщений
func (r *RedisClient) Subscribe(channel string) (<-chan *redis.Message, func() error) {
	pubsub := r.client.Subscribe(r.ctx, channel)
	ch := pubsub.Channel()
	
	// Функция для закрытия подписки
	closeFn := func() error {
		return pubsub.Close()
	}
	
	return ch, closeFn
}

