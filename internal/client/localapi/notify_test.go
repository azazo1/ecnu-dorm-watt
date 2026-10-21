package localapi

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/azazo1/ecnu-dorm-watt/internal/config"
)

type fakeNotifier struct {
	titles []string
	bodies []string
}

func (f *fakeNotifier) Notify(title, body string) error {
	f.titles = append(f.titles, title)
	f.bodies = append(f.bodies, body)
	return nil
}

func newNotifyTestTracker(t *testing.T, now *time.Time) (*tracker, *fakeNotifier) {
	t.Helper()
	notifier := &fakeNotifier{}
	cfg := config.DefaultClient()
	srv := &Server{
		log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		now:      func() time.Time { return *now },
		notifier: notifier,
	}
	srv.cfg.Store(&cfg)
	tr := newTracker(srv)
	tr.srv.now = func() time.Time { return *now }
	return tr, notifier
}

func TestNotifyHealthTransition(t *testing.T) {
	now := time.Date(2026, 1, 24, 12, 0, 0, 0, time.Local)
	tr, notifier := newNotifyTestTracker(t, &now)

	tr.notify(Snapshot{Health: HealthOK}, Snapshot{Health: HealthNotLoggedIn, Detail: "会话失效"})
	if len(notifier.titles) != 1 || notifier.titles[0] != "ECNU 登录已失效" {
		t.Fatalf("unexpected notifications: %#v", notifier.titles)
	}

	tr.notify(Snapshot{Health: HealthServerDown}, Snapshot{Health: HealthOK})
	if len(notifier.titles) != 2 || notifier.titles[1] != "DormWatt 已恢复" {
		t.Fatalf("recovery not notified: %#v", notifier.titles)
	}
}

func TestNotifyLowBatteryThrottle(t *testing.T) {
	now := time.Date(2026, 1, 24, 12, 0, 0, 0, time.Local)
	tr, notifier := newNotifyTestTracker(t, &now)
	threshold := 10.0
	low := 8.0
	high := 12.0

	tr.notify(Snapshot{Health: HealthOK, Degree: &high, Threshold: threshold}, Snapshot{Health: HealthOK, Degree: &low, Threshold: threshold})
	if len(notifier.titles) != 1 {
		t.Fatalf("low battery not notified: %#v", notifier.titles)
	}

	now = now.Add(10 * time.Minute)
	tr.notify(Snapshot{Health: HealthOK, Degree: &low, Threshold: threshold}, Snapshot{Health: HealthOK, Degree: &low, Threshold: threshold})
	if len(notifier.titles) != 1 {
		t.Fatalf("low battery throttle failed: %#v", notifier.titles)
	}

	now = now.Add(31 * time.Minute)
	tr.notify(Snapshot{Health: HealthOK, Degree: &low, Threshold: threshold}, Snapshot{Health: HealthOK, Degree: &low, Threshold: threshold})
	if len(notifier.titles) != 2 {
		t.Fatalf("low battery reminder failed: %#v", notifier.titles)
	}
}

func TestNotifySkipsStartup(t *testing.T) {
	now := time.Date(2026, 1, 24, 12, 0, 0, 0, time.Local)
	tr, notifier := newNotifyTestTracker(t, &now)
	tr.notify(Snapshot{Health: HealthStarting}, Snapshot{Health: HealthServerDown})
	if len(notifier.titles) != 0 {
		t.Fatalf("startup transition notified: %#v", notifier.titles)
	}
}
