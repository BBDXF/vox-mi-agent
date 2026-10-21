package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func open(t *testing.T) (*Store, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "sub dir", "app.db")
	// parent must exist
	mk(t, filepath.Dir(p))
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, p
}

func TestMigrationsIdempotent(t *testing.T) {
	ctx := context.Background()
	s, p := open(t)
	v1, err := s.SchemaVersion(ctx)
	if err != nil || v1 < 1 {
		t.Fatalf("version %d %v", v1, err)
	}
	if _, err := s.CreateConversation(ctx, "keep", ChannelCLI); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s2, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	v2, _ := s2.SchemaVersion(ctx)
	if v2 != v1 {
		t.Errorf("version changed %d -> %d", v1, v2)
	}
	var n int
	s2.DB().QueryRow(`SELECT COUNT(*) FROM schema_version`).Scan(&n)
	if n != v1 {
		t.Errorf("schema_version rows=%d", n)
	}
	cs, _ := s2.ListConversations(ctx, 10, time.Time{})
	if len(cs) != 1 {
		t.Errorf("data lost: %v", cs)
	}
	var wal string
	s2.DB().QueryRow(`PRAGMA journal_mode`).Scan(&wal)
	var fk int
	s2.DB().QueryRow(`PRAGMA foreign_keys`).Scan(&fk)
	if wal != "wal" || fk != 1 {
		t.Errorf("wal=%q fk=%d", wal, fk)
	}
}

func TestConversationsAndMessages(t *testing.T) {
	ctx := context.Background()
	s, _ := open(t)

	if _, err := s.CreateConversation(ctx, "x", "bogus"); err == nil {
		t.Error("bad channel accepted")
	}
	a, err := s.CreateConversation(ctx, "first", ChannelWeb)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetConversation(ctx, a.ID)
	if err != nil || got.Title != "first" || got.Channel != "web" || !got.CreatedAt.Equal(a.CreatedAt) {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := s.GetConversation(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err=%v", err)
	}

	calls := json.RawMessage(`[{"id":"c1","type":"function","function":{"name":"search_audio","arguments":"{}"}}]`)
	msgs := []Message{
		{Role: "user", Content: "播放起风了"},
		{Role: "assistant", ToolCalls: calls},
		{Role: "tool", Content: `{"hits":[]}`, ToolCallID: "c1", Name: "search_audio"},
	}
	for _, m := range msgs {
		time.Sleep(2 * time.Millisecond)
		if _, err := s.AppendMessage(ctx, a.ID, m); err != nil {
			t.Fatal(err)
		}
	}
	list, err := s.ListMessages(ctx, a.ID)
	if err != nil || len(list) != 3 {
		t.Fatalf("%v %v", list, err)
	}
	if list[0].Content != "播放起风了" || list[0].ToolCalls != nil {
		t.Errorf("m0=%+v", list[0])
	}
	if string(list[1].ToolCalls) != string(calls) {
		t.Errorf("tool_calls=%s", list[1].ToolCalls)
	}
	if list[2].ToolCallID != "c1" || list[2].Name != "search_audio" || list[2].ID <= list[1].ID {
		t.Errorf("m2=%+v", list[2])
	}
	if _, err := s.AppendMessage(ctx, a.ID, Message{Role: "assistant", ToolCalls: json.RawMessage(`{bad`)}); err == nil {
		t.Error("invalid json accepted")
	}
	if _, err := s.AppendMessage(ctx, "missing", Message{Role: "user"}); err == nil {
		t.Error("FK not enforced")
	}

	// updated_at bumped; ordering and pagination.
	time.Sleep(3 * time.Millisecond)
	b, _ := s.CreateConversation(ctx, "second", ChannelSpeaker)
	cs, _ := s.ListConversations(ctx, 10, time.Time{})
	if len(cs) != 2 || cs[0].ID != b.ID {
		t.Fatalf("order: %+v", cs)
	}
	time.Sleep(3 * time.Millisecond)
	s.AppendMessage(ctx, a.ID, Message{Role: "user", Content: "bump"})
	cs, _ = s.ListConversations(ctx, 10, time.Time{})
	if cs[0].ID != a.ID {
		t.Errorf("a should be newest: %+v", cs)
	}
	page, _ := s.ListConversations(ctx, 1, cs[0].UpdatedAt)
	if len(page) != 1 || page[0].ID != b.ID {
		t.Errorf("page: %+v", page)
	}
	if one, _ := s.ListConversations(ctx, 1, time.Time{}); len(one) != 1 {
		t.Error("limit")
	}
}

func TestSettingsAndAudit(t *testing.T) {
	ctx := context.Background()
	s, _ := open(t)
	if _, err := s.GetSetting(ctx, "k"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err=%v", err)
	}
	for _, v := range []string{"v1", "v2"} {
		if err := s.SetSetting(ctx, "k", v); err != nil {
			t.Fatal(err)
		}
	}
	if v, err := s.GetSetting(ctx, "k"); err != nil || v != "v2" {
		t.Errorf("%q %v", v, err)
	}

	if err := s.Audit(ctx, "media.sync", "system", map[string]any{"tracks": 3}); err != nil {
		t.Fatal(err)
	}
	if err := s.Audit(ctx, "shell", "admin", nil); err != nil {
		t.Fatal(err)
	}
	all, _ := s.ListAudit(ctx, "", 10)
	if len(all) != 2 || all[0].Kind != "shell" || all[0].Detail != nil {
		t.Fatalf("%+v", all)
	}
	one, _ := s.ListAudit(ctx, "media.sync", 10)
	var d map[string]int
	if len(one) != 1 || json.Unmarshal(one[0].Detail, &d) != nil || d["tracks"] != 3 || one[0].Actor != "system" {
		t.Errorf("%+v", one)
	}
}

func mk(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteConversation(t *testing.T) {
	ctx := context.Background()
	s, _ := open(t)
	a, _ := s.CreateConversation(ctx, "a", ChannelWeb)
	b, _ := s.CreateConversation(ctx, "b", ChannelCLI)
	for _, c := range []*Conversation{a, b} {
		if _, err := s.AppendMessage(ctx, c.ID, Message{Role: "user", Content: "hi"}); err != nil {
			t.Fatal(err)
		}
		if err := s.InsertTraces(ctx, []TraceRow{{TS: time.Now(), ConversationID: c.ID, TurnID: "t", Kind: TraceTurn, Status: TraceOK}}); err != nil {
			t.Fatal(err)
		}
	}
	// a trace without a conversation must survive both deletes
	if err := s.InsertTraces(ctx, []TraceRow{{TS: time.Now(), TurnID: "x", Kind: TraceTool, Name: "media_sync", Status: TraceOK}}); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteConversation(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteConversation(ctx, a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
	if err := s.DeleteConversation(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown: %v", err)
	}
	if _, err := s.GetConversation(ctx, a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("still there: %v", err)
	}
	if m, _ := s.ListMessages(ctx, a.ID); len(m) != 0 {
		t.Fatalf("messages left: %v", m)
	}
	if tr, _ := s.QueryTraces(ctx, TraceFilter{ConversationID: a.ID}); len(tr) != 0 {
		t.Fatalf("traces left: %v", tr)
	}
	// the other conversation is untouched
	if m, _ := s.ListMessages(ctx, b.ID); len(m) != 1 {
		t.Fatalf("b messages: %v", m)
	}
	if tr, _ := s.QueryTraces(ctx, TraceFilter{ConversationID: b.ID}); len(tr) != 1 {
		t.Fatalf("b traces: %v", tr)
	}

	n, err := s.DeleteAllConversations(ctx)
	if err != nil || n != 1 {
		t.Fatalf("delete all: %d %v", n, err)
	}
	if cs, _ := s.ListConversations(ctx, 10, time.Time{}); len(cs) != 0 {
		t.Fatalf("conversations left: %v", cs)
	}
	if tr, _ := s.QueryTraces(ctx, TraceFilter{}); len(tr) != 1 || tr[0].ConversationID != "" {
		t.Fatalf("expected only the conversation-less trace: %v", tr)
	}
	if n, err := s.DeleteAllConversations(ctx); err != nil || n != 0 {
		t.Fatalf("empty delete all: %d %v", n, err)
	}
}
