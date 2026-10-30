// Package repo 是界面设置的持久化层。只有一个聚合：本应用自己的偏好。不用数据库：存的全是
// 「删了不丢任何对话」的东西（主题、语言、字号、上次工作目录、当前引擎、可执行文件路径），二三十个键、无查询无关联无并发写者，
// 一个 JSON 文件足够，用户出问题时能直接打开看、改、备份；读走内存（启动时一次性载入，之后只读），写走「改内存 → 立刻落盘」，落盘用临时文件 + Rename 避免断电留下半个 JSON——文件坏了应用下次就起不来。
package repo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"OhMyPiDesktop/backend/pkg"
)

// Store 是设置 KV 的仓库。零值不可用，用 Open。
type Store struct {
	mu   sync.Mutex
	path string
	kv   map[string]string
}

// Open 读入设置文件；不存在就当作空设置（首次启动是正常状态，不是错误）。
func Open(path string) (*Store, error) {
	s := &Store{path: path, kv: map[string]string{}}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, pkg.Wrap(pkg.CodeStartup, "读不到设置文件："+path, err)
	}
	if len(raw) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(raw, &s.kv); err != nil {
		// 文件坏了不能让应用起不来：改名留档，用空设置继续，用户能在日志里看到原因。
		broken := path + ".broken"
		_ = os.Rename(path, broken)
		pkg.Warnf("设置文件无法解析，已改名为 %s：%v", broken, err)
		s.kv = map[string]string{}
		return s, nil
	}
	return s, nil
}

// Get 读一个键，不存在返回空串（设置项缺失是正常状态，不是错误）。
func (s *Store) Get(key string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.kv[key]
}

// Set 写一个键。空值等于删除，避免文件里堆一串 "key": ""。
func (s *Store) Set(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if value == "" {
		delete(s.kv, key)
	} else {
		s.kv[key] = value
	}
	return s.flushLocked()
}

// All 读全部设置。返回副本，调用方随便改。
func (s *Store) All() (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]string, len(s.kv))
	for k, v := range s.kv {
		out[k] = v
	}
	return out, nil
}

// SetMany 批量写，只落盘一次。
func (s *Store) SetMany(kv map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range kv {
		if v == "" {
			delete(s.kv, k)
		} else {
			s.kv[k] = v
		}
	}
	return s.flushLocked()
}

// Close 保留给调用方统一收尾；内存存储没有需要释放的东西。
func (s *Store) Close() error { return nil }

// flushLocked 把内存写到磁盘。调用方必须持锁。
// 键排序后再序列化：让文件对人可读、对 diff 友好，也让「同一次改动」在两次
// 运行间产生稳定字节，出问题时能直接比对。
func (s *Store) flushLocked() error {
	keys := make([]string, 0, len(s.kv))
	for k := range s.kv {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	ordered := make(map[string]string, len(keys))
	for _, k := range keys {
		ordered[k] = s.kv[k]
	}
	raw, err := json.MarshalIndent(ordered, "", "  ")
	if err != nil {
		return pkg.Wrap(pkg.CodeInternal, "设置无法序列化", err)
	}
	raw = append(raw, '\n')

	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return pkg.Wrap(pkg.CodeInternal, "设置目录不可写："+filepath.Dir(s.path), err)
	}
	// 先写临时文件再改名：直接覆盖时断电会留下半个 JSON。
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return pkg.Wrap(pkg.CodeInternal, "保存设置失败", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return pkg.Wrap(pkg.CodeInternal, "保存设置失败", err)
	}
	return nil
}
