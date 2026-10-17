package cookiepool

import (
	"bufio"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Pool struct {
	mu      sync.RWMutex
	dir     string
	cookies map[string][]string
	indices map[string]int
	rng     *rand.Rand
}

var (
	defaultPool *Pool
	once        sync.Once
)

func GetDefaultPool() *Pool {
	once.Do(func() {
		// Checks data/cookies then cookies
		dir := "data/cookies"
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			dir = "cookies"
		}
		_ = os.MkdirAll(dir, 0o755)
		defaultPool = New(dir)
	})
	return defaultPool
}

func New(dir string) *Pool {
	return &Pool{
		dir:     dir,
		cookies: make(map[string][]string),
		indices: make(map[string]int),
		rng:     rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (p *Pool) Load(platform string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	platform = strings.ToLower(strings.TrimSpace(platform))
	filename := filepath.Join(p.dir, platform+".txt")

	file, err := os.Open(filename)
	if err != nil {
		if os.IsNotExist(err) {
			// check alternate location (e.g. data/cookies vs cookies)
			altFile := filepath.Join("cookies", platform+".txt")
			if alt, altErr := os.Open(altFile); altErr == nil {
				file = alt
			} else {
				p.cookies[platform] = nil
				return nil
			}
		} else {
			return err
		}
	}
	defer file.Close()

	var list []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			list = append(list, line)
		}
	}

	p.cookies[platform] = list
	return scanner.Err()
}

func (p *Pool) GetCookie(platform string) string {
	platform = strings.ToLower(strings.TrimSpace(platform))

	p.mu.Lock()
	defer p.mu.Unlock()

	list, ok := p.cookies[platform]
	if !ok || len(list) == 0 {
		// Try loading from file
		p.mu.Unlock()
		_ = p.Load(platform)
		p.mu.Lock()
		list = p.cookies[platform]
	}

	if len(list) == 0 {
		return ""
	}

	idx := p.indices[platform]
	cookie := list[idx%len(list)]
	p.indices[platform] = (idx + 1) % len(list)

	return cookie
}

func (p *Pool) GetAllCookies(platform string) []string {
	platform = strings.ToLower(strings.TrimSpace(platform))

	p.mu.Lock()
	defer p.mu.Unlock()

	list, ok := p.cookies[platform]
	if !ok || len(list) == 0 {
		p.mu.Unlock()
		_ = p.Load(platform)
		p.mu.Lock()
		list = p.cookies[platform]
	}

	if len(list) == 0 {
		return nil
	}

	res := make([]string, len(list))
	copy(res, list)

	return res
}
