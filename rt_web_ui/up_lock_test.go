package main

import (
	"strings"
	"testing"
)

// A second make up / make down waits its turn: while one holds the lock the
// other fails with who holds it, and after unlock it gets the lock.
func TestLockUpDown(t *testing.T) {
	dir := t.TempDir()
	unlock, err := lockUpDown(dir, "up")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lockUpDown(dir, "down"); err == nil || !strings.Contains(err.Error(), "make up · pid") {
		t.Fatalf("second lock while held: err = %v, want 'another make up / make down is running (make up · pid …)'", err)
	}
	unlock()
	unlock2, err := lockUpDown(dir, "down")
	if err != nil {
		t.Fatalf("lock after unlock: %v", err)
	}
	unlock2()
}
