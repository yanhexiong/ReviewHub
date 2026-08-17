package importprogress

import (
	"regexp"
	"sync"
	"time"
)

const expiry = 2 * time.Minute

var idPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

type State struct {
	ID        string `json:"id"`
	Progress  int    `json:"progress"`
	Stage     string `json:"stage"`
	Done      bool   `json:"done"`
	Error     string `json:"error,omitempty"`
	UpdatedAt int64  `json:"updatedAt"`
}

type storedState struct {
	State
	UserID    string
	ExpiresAt time.Time
}

type Service struct {
	mutex sync.Mutex
	items map[string]storedState
}

func New() *Service { return &Service{items: make(map[string]storedState)} }

func IDFromHeader(value string) string {
	if idPattern.MatchString(value) {
		return value
	}
	return ""
}

func (service *Service) Start(id, userID string) {
	if id == "" {
		return
	}
	service.update(id, userID, 0, "正在准备导入", false, "")
}

func (service *Service) Update(id, userID string, progress int, stage string) {
	if id == "" {
		return
	}
	service.update(id, userID, progress, stage, false, "")
}

func (service *Service) Complete(id, userID, stage string) {
	if id == "" {
		return
	}
	service.update(id, userID, 100, stage, true, "")
}

func (service *Service) Fail(id, userID, message string) {
	if id == "" {
		return
	}
	service.update(id, userID, 100, "导入失败", true, message)
}

func (service *Service) Get(id, userID string) (State, bool) {
	if !idPattern.MatchString(id) {
		return State{}, false
	}
	service.mutex.Lock()
	defer service.mutex.Unlock()
	service.cleanupLocked(time.Now())
	item, ok := service.items[id]
	if !ok || item.UserID != userID {
		return State{}, false
	}
	return item.State, true
}

func (service *Service) update(id, userID string, progress int, stage string, done bool, message string) {
	service.mutex.Lock()
	defer service.mutex.Unlock()
	service.cleanupLocked(time.Now())
	if existing, ok := service.items[id]; ok && existing.UserID != userID {
		return
	}
	if progress < 0 {
		progress = 0
	}
	if progress > 100 {
		progress = 100
	}
	now := time.Now()
	service.items[id] = storedState{
		State:  State{ID: id, Progress: progress, Stage: stage, Done: done, Error: message, UpdatedAt: now.UnixMilli()},
		UserID: userID, ExpiresAt: now.Add(expiry),
	}
}

func (service *Service) cleanupLocked(now time.Time) {
	for id, item := range service.items {
		if !item.ExpiresAt.After(now) {
			delete(service.items, id)
		}
	}
}
