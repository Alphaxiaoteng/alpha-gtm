package browser

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"sync"
	"time"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/errors"
)

const (
	PortMin = 15101
	PortMax = 15600
)

// PortAllocator 管理 CDP 端口分配，避免多实例端口冲突。
// ponytail: concrete struct，不建 interface。SQLite browser_runtime_instances 是 source of truth。
// 升级路径：多 Supervisor 进程时用 SQLite BEGIN EXCLUSIVE 替代 mutex。
type PortAllocator struct {
	mu sync.Mutex
	db *db.DB
}

// PortReservation holds the short-lived database reservation between port
// selection and browser_runtime_instances persistence. It is not a browser
// ownership record; Release must run after either runtime persistence or a
// launch failure.
type PortReservation struct {
	Port  int
	owner string
	db    *db.DB
	once  sync.Once
	err   error
}

func (r *PortReservation) Release() error {
	if r == nil {
		return nil
	}
	r.once.Do(func() {
		r.err = r.db.ReleaseCDPPort(r.Port, r.owner)
	})
	return r.err
}

func NewPortAllocator(database *db.DB) *PortAllocator {
	return &PortAllocator{db: database}
}

// Acquire 仅用于兼容旧的“预检可用端口”调用。它会立刻释放租约，不能
// 用于启动浏览器；启动路径必须调用 Reserve 并在写入 runtime 后释放。
func (p *PortAllocator) Acquire(profileID string, preferred int) (int, error) {
	reservation, err := p.Reserve(profileID, preferred)
	if err != nil {
		return 0, err
	}
	defer reservation.Release()
	return reservation.Port, nil
}

// Reserve 分配并持有 CDP 端口。SQLite 的唯一记录将多个应用进程的
// “查询空闲端口 -> 启动浏览器”窗口收敛为一次原子预约。
func (p *PortAllocator) Reserve(profileID string, preferred int) (*PortReservation, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	owner, err := newPortLeaseOwner()
	if err != nil {
		return nil, err
	}
	candidates := make([]int, 0, PortMax-PortMin+1)
	if preferred >= PortMin && preferred <= PortMax {
		candidates = append(candidates, preferred)
	}
	for port := PortMin; port <= PortMax; port++ {
		if port != preferred {
			candidates = append(candidates, port)
		}
	}

	for _, port := range candidates {
		reserved, reserveErr := p.db.ReserveCDPPort(profileID, owner, port, time.Now(), 45*time.Second)
		if reserveErr != nil {
			return nil, fmt.Errorf("reserve CDP port %d: %w", port, reserveErr)
		}
		if !reserved {
			continue
		}
		if isTCPFree(port) {
			return &PortReservation{Port: port, owner: owner, db: p.db}, nil
		}
		if releaseErr := p.db.ReleaseCDPPort(port, owner); releaseErr != nil {
			return nil, fmt.Errorf("release unavailable CDP port %d: %w", port, releaseErr)
		}
	}

	return nil, fmt.Errorf("%s: all ports %d-%d in use", errors.NO_PORT_AVAILABLE, PortMin, PortMax)
}

func newPortLeaseOwner() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate CDP port reservation owner: %w", err)
	}
	return "cdp-port:" + hex.EncodeToString(value), nil
}

// isTCPFree 检测端口是否真的可用（避免 TIME_WAIT 假空闲）。
func isTCPFree(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	ln.Close()
	return true
}
