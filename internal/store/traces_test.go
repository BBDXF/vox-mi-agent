package store

import (
	"context"
	"strings"
	"testing"
	"time"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir() + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestTracesInsertQueryPrune(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	now := time.Now()
	var rows []TraceRow
	for i := 0; i < 10; i++ {
		rows = append(rows, TraceRow{
			TS: now.Add(time.Duration(i-10) * 24 * time.Hour), ConversationID: "c1", TurnID: "t1", Step: i,
			Kind: TraceLLM, Name: "m", Status: TraceOK, Args: strings.Repeat("好", 2000),
		})
	}
	rows[3].Status, rows[3].Kind = TraceError, TraceTool
	if err := s.InsertTraces(ctx, rows); err != nil {
		t.Fatal(err)
	}
	all, err := s.QueryTraces(ctx, TraceFilter{})
	if err != nil || len(all) != 10 || all[0].ID < all[9].ID {
		t.Fatalf("%v %d", err, len(all))
	}
	if n := len(all[0].Args); n > TraceTextMax+len("…") || !strings.HasSuffix(all[0].Args, "…") {
		t.Fatalf("args not truncated: %d", n)
	}
	if got, _ := s.QueryTraces(ctx, TraceFilter{Status: TraceError}); len(got) != 1 || got[0].Kind != TraceTool {
		t.Fatalf("status filter %+v", got)
	}
	if got, _ := s.QueryTraces(ctx, TraceFilter{Kind: TraceTool, Name: "m"}); len(got) != 1 {
		t.Fatalf("kind+name filter %+v", got)
	}
	if got, _ := s.QueryTraces(ctx, TraceFilter{Since: now.Add(-3*24*time.Hour - time.Hour)}); len(got) != 3 {
		t.Fatalf("since filter %d", len(got))
	}
	if got, _ := s.QueryTraces(ctx, TraceFilter{BeforeID: all[2].ID, Limit: 2}); len(got) != 2 || got[0].ID != all[3].ID {
		t.Fatalf("before_id %+v", got)
	}

	// age pruning: rows older than 5.5 days (6 of them: i=0..4 are 10..6 days old... i<=4)
	n, err := s.PruneTraces(ctx, now.Add(-5*24*time.Hour-12*time.Hour), 0)
	if err != nil || n != 5 {
		t.Fatalf("age prune %d %v", n, err)
	}
	// row cap: keep the newest 2
	n, err = s.PruneTraces(ctx, time.Time{}, 2)
	if err != nil || n != 3 {
		t.Fatalf("cap prune %d %v", n, err)
	}
	left, _ := s.QueryTraces(ctx, TraceFilter{})
	if len(left) != 2 || left[0].ID != all[0].ID || left[1].ID != all[1].ID {
		t.Fatalf("left %+v", left)
	}
	if c, _ := s.CountTraces(ctx); c != 2 {
		t.Fatalf("count %d", c)
	}
}

func TestAliasItems(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	items := []AliasItem{
		{Title: "晴天", Artist: "周杰伦", Aliases: []string{"qingtian", "sunny"}},
		{Title: "晴天", Artist: "", Aliases: []string{"sunny", "fine day"}},
		{Title: "夜曲", Aliases: nil},
	}
	if err := s.ReplaceAliasItems(ctx, items); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListAliasItems(ctx)
	if err != nil || len(got) != 3 {
		t.Fatalf("%v %+v", err, got)
	}
	m, _ := s.Aliases(ctx)
	if len(m) != 1 || len(m["晴天"]) != 3 {
		t.Fatalf("merged aliases %+v", m) // empty-alias titles are dropped, duplicates merged
	}
	if err := s.ReplaceAliasItems(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.ListAliasItems(ctx); len(got) != 0 {
		t.Fatalf("not cleared: %+v", got)
	}
}
