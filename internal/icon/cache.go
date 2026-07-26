package icon

import (
	"container/list"
	"context"
	"sync"
	"time"
)

// ============================================================================
// 1. 缓存接口与数据结构定义
// ============================================================================

// cacheKey 复合结构体键，避免字符串拼接带来的 GC 堆分配压力
type cacheKey struct {
	query string   // 文件路径或文件扩展名
	size  IconSize // 图标尺寸
}

// entry 链表节点中存储的实际元素
type entry struct {
	key       cacheKey
	val       *IconResult
	expiresAt time.Time // 过期时间戳 (如果是零值则表示永不过期)
}

// ============================================================================
// 2. 线程安全的 LRU + TTL 缓存实现
// ============================================================================

type LRUCache struct {
	mu        sync.Mutex
	items     map[cacheKey]*list.Element
	evictList *list.List
	capacity  int
	ttl       time.Duration

	// 可选参数：淘汰元素时的回调函数，可用于监控指标埋点或深度调试
	onEvict func(query string, size IconSize, val *IconResult)
}

// CacheOption 允许使用函数式配置模式 (Functional Options) 初始化缓存
type CacheOption func(*LRUCache)

// WithTTL 设置缓存元素的生存时间 (默认永不过期，仅由 LRU 容量淘汰)
func WithTTL(ttl time.Duration) CacheOption {
	return func(c *LRUCache) {
		c.ttl = ttl
	}
}

// WithEvictCallback 设置元素被淘汰或过期时的回调函数
func WithEvictCallback(cb func(query string, size IconSize, val *IconResult)) CacheOption {
	return func(c *LRUCache) {
		c.onEvict = cb
	}
}

// NewLRUCache 创建一个线程安全的 LRU 内存缓存
// capacity: 最大允许缓存的图标数量 (<= 0 则自动默认设为 512)
func NewLRUCache(capacity int, opts ...CacheOption) *LRUCache {
	if capacity <= 0 {
		capacity = 512 // 默认合理的图标缓存上限
	}

	c := &LRUCache{
		items:     make(map[cacheKey]*list.Element, capacity),
		evictList: list.New(),
		capacity:  capacity,
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// ============================================================================
// 3. 核心功能实现: Get / Set / Delete / Clear
// ============================================================================

// Get 尝试从缓存中获取图标
func (c *LRUCache) Get(query string, size IconSize) (*IconResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := cacheKey{query: query, size: size}
	elem, exists := c.items[key]
	if !exists {
		return nil, false
	}

	ent := elem.Value.(*entry)

	// 惰性过期检查 (Lazy Eviction)
	if !ent.expiresAt.IsZero() && time.Now().After(ent.expiresAt) {
		c.removeElement(elem)
		return nil, false
	}

	// 命中缓存，将其移动到双向链表头部 (标记为最近使用)
	c.evictList.MoveToFront(elem)
	return ent.val, true
}

// Set 将图标结果写入缓存，若超出容量则自动淘汰最久未使用的节点
func (c *LRUCache) Set(query string, size IconSize, val *IconResult) {
	if val == nil {
		return // 拒绝缓存空对象，防止脏数据
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	key := cacheKey{query: query, size: size}
	var expiresAt time.Time
	if c.ttl > 0 {
		expiresAt = time.Now().Add(c.ttl)
	}

	// 1. 如果键已存在，则更新值、刷新过期时间并移到链表头部
	if elem, exists := c.items[key]; exists {
		c.evictList.MoveToFront(elem)
		ent := elem.Value.(*entry)
		ent.val = val
		ent.expiresAt = expiresAt
		return
	}

	// 2. 如果已达到或超过容量上限，先执行末尾淘汰 (Evict Oldest)
	for c.evictList.Len() >= c.capacity {
		c.evictOldest()
	}

	// 3. 插入新元素到头部并在 map 中建立索引
	ent := &entry{
		key:       key,
		val:       val,
		expiresAt: expiresAt,
	}
	elem := c.evictList.PushFront(ent)
	c.items[key] = elem
}

// Delete 主动删除指定的缓存项
func (c *LRUCache) Delete(query string, size IconSize) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := cacheKey{query: query, size: size}
	if elem, exists := c.items[key]; exists {
		c.removeElement(elem)
	}
}

// Clear 清空所有缓存项并释放内存映射
func (c *LRUCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 若存在回调函数，在清空前针对每个节点调用
	if c.onEvict != nil {
		for _, elem := range c.items {
			ent := elem.Value.(*entry)
			c.onEvict(ent.key.query, ent.key.size, ent.val)
		}
	}

	c.items = make(map[cacheKey]*list.Element, c.capacity)
	c.evictList.Init()
}

// Len 返回当前缓存中存在的真实元素数量
func (c *LRUCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.evictList.Len()
}

// evictOldest 淘汰链表尾端（最久未使用）的节点 [需要调用方持有锁 c.mu]
func (c *LRUCache) evictOldest() {
	elem := c.evictList.Back()
	if elem != nil {
		c.removeElement(elem)
	}
}

// removeElement 内部原子清理节点 [需要调用方持有锁 c.mu]
func (c *LRUCache) removeElement(elem *list.Element) {
	c.evictList.Remove(elem)
	ent := elem.Value.(*entry)
	delete(c.items, ent.key)

	if c.onEvict != nil {
		c.onEvict(ent.key.query, ent.key.size, ent.val)
	}
}

// ============================================================================
// 4. 装饰器模式: 自动带缓存的 CachedFetcher
// ============================================================================

// CachedFetcher 实现 Fetcher 接口，为其内部包装的真正的 Fetcher 自动添加缓存拦截层
type CachedFetcher struct {
	fetcher Fetcher
	cache   Cache
}

// NewCachedFetcher 构建一个带缓存能力的提取器包装实例
func NewCachedFetcher(fetcher Fetcher, cache Cache) Fetcher {
	if cache == nil {
		cache = NewLRUCache(512) // Fallback 默认配置
	}
	return &CachedFetcher{
		fetcher: fetcher,
		cache:   cache,
	}
}

// FetchByPath 带有缓存读取与自动回填策略的路径提取
func (cf *CachedFetcher) FetchByPath(ctx context.Context, path string, size IconSize) (*IconResult, error) {
	if res, hit := cf.cache.Get(path, size); hit {
		return res, nil
	}

	res, err := cf.fetcher.FetchByPath(ctx, path, size)
	if err == nil && res != nil {
		cf.cache.Set(path, size, res)
	}
	return res, err
}

// FetchByExtension 带有缓存读取与自动回填策略的扩展名提取
func (cf *CachedFetcher) FetchByExtension(ctx context.Context, ext string, size IconSize) (*IconResult, error) {
	if res, hit := cf.cache.Get(ext, size); hit {
		return res, nil
	}

	res, err := cf.fetcher.FetchByExtension(ctx, ext, size)
	if err == nil && res != nil {
		cf.cache.Set(ext, size, res)
	}
	return res, err
}
