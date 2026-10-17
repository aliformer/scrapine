package cookiepool

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPool_GetAllCookies(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	content := "auth_token=tok1; ct0=cs1\n# comment\nauth_token=tok2; ct0=cs2\n"
	err := os.WriteFile(filepath.Join(dir, "twitter.txt"), []byte(content), 0o600)
	if err != nil {
		t.Fatalf("failed to write test cookie file: %v", err)
	}

	p := New(dir)
	cookies := p.GetAllCookies("twitter")
	if len(cookies) != 2 {
		t.Fatalf("expected 2 cookies, got %d", len(cookies))
	}
	if cookies[0] != "auth_token=tok1; ct0=cs1" || cookies[1] != "auth_token=tok2; ct0=cs2" {
		t.Errorf("unexpected cookies: %v", cookies)
	}

	c1 := p.GetCookie("twitter")
	c2 := p.GetCookie("twitter")
	c3 := p.GetCookie("twitter")
	if c1 != cookies[0] || c2 != cookies[1] || c3 != cookies[0] {
		t.Errorf("unexpected round-robin: %s, %s, %s", c1, c2, c3)
	}
}
