package speaker

import (
	"errors"
	"testing"
	"time"
)

func TestPollThrottle(t *testing.T) {
	var th pollThrottle
	now := time.Now()
	e := errors.New("not logged in")
	if !th.shouldLog(now, e) {
		t.Fatal("first must log")
	}
	if th.shouldLog(now.Add(time.Minute), e) {
		t.Fatal("repeat must not log")
	}
	if !th.shouldLog(now.Add(time.Minute), errors.New("other")) {
		t.Fatal("changed error must log")
	}
	if !th.shouldLog(now.Add(40*time.Minute), errors.New("other")) {
		t.Fatal("after 30m must log")
	}
}
