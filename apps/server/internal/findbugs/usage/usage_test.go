package usage

import (
	"context"
	"testing"
	"time"
)

const sample = `{"type":"system","subtype":"init"}
{"type":"rate_limit_event","rate_limit_info":{"status":"allowed","resetsAt":1791411000,"rateLimitType":"five_hour","unifiedWindows":{"five_hour":{"utilization":0.08,"resetsAt":1791411000},"seven_day":{"utilization":0.17,"resetsAt":1791892800}}}}
{"type":"result","is_error":false,"result":"ok"}
`

func TestParse(t *testing.T) {
	s, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if s.Status != "allowed" || s.FiveHour == nil || s.SevenDay == nil {
		t.Fatalf("got %+v", s)
	}
	if s.FiveHour.Percent != 8 || !s.FiveHour.ResetsAt.Equal(time.Unix(1791411000, 0)) {
		t.Fatalf("five hour %+v", s.FiveHour)
	}
	if s.SevenDay.Percent != 17 {
		t.Fatalf("seven day %+v", s.SevenDay)
	}
	if _, err := Parse([]byte(`{"type":"result"}`)); err == nil {
		t.Fatal("want error without a rate_limit_event")
	}
}

func TestGetCaches(t *testing.T) {
	calls := 0
	p := &Prober{MaxAge: time.Hour, MinGap: time.Hour, run: func(context.Context) ([]byte, error) {
		calls++
		return []byte(sample), nil
	}}
	for range 3 {
		if _, err := p.Get(context.Background(), true); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("probed %d times, want 1 within MinGap", calls)
	}
	p.MinGap = 0
	if _, err := p.Get(context.Background(), false); err != nil || calls != 1 {
		t.Fatalf("unforced get within MaxAge probed: calls=%d err=%v", calls, err)
	}
	if _, err := p.Get(context.Background(), true); err != nil || calls != 2 {
		t.Fatalf("forced get did not probe: calls=%d err=%v", calls, err)
	}
}
