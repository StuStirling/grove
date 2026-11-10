package main

import (
	"context"
	"strconv"
	"sync"
	"sync/atomic"

	wr "github.com/wailsapp/wails/v2/pkg/runtime"
)

// attention tells you when a Claude wants you while grove is in the
// background: a notification per tab (clicking it goes to the tab) and a count
// on the Dock icon. The sidebar's marks cover the window itself.
type attention struct {
	focused  atomic.Bool
	mu       sync.Mutex
	notified map[string]bool // pane ids with a notification up
	badge    int             // last count set on the Dock
}

func (a *App) startAttention(ctx context.Context) {
	a.att.notified = map[string]bool{}
	a.att.badge = -1
	a.sess.alert = a.alerted
	a.sess.change = a.attend
	if wr.InitializeNotifications(ctx) != nil {
		return // no notification service (a Linux desktop without one): the badge still works
	}
	wr.OnNotificationResponse(ctx, func(r wr.NotificationResult) {
		dir, _ := r.Response.UserInfo["dir"].(string)
		pane, _ := r.Response.UserInfo["pane"].(string)
		if r.Error != nil || dir == "" {
			return
		}
		a.emit("goto-tab", dir, pane)
		a.show()
	})
}

// SetFocused is the frontend reporting whether the window has focus; only a
// background grove notifies.
func (a *App) SetFocused(f bool) { a.att.focused.Store(f) }

// alerted notifies that pane p's Claude finished or is waiting.
func (a *App) alerted(p Pane, ws, dir string) {
	if a.att.focused.Load() || !wr.IsNotificationAvailable(a.ctx) {
		return
	}
	body := "Claude finished"
	if p.Claude == "waiting" {
		body = "Claude is waiting for you"
	}
	go func() {
		// Asks the first time only; macOS remembers the answer.
		if ok, err := wr.RequestNotificationAuthorization(a.ctx); !ok || err != nil {
			return
		}
		// Keyed by pane, so a newer one replaces the tab's last.
		err := wr.SendNotification(a.ctx, wr.NotificationOptions{
			ID: p.ID, Title: ws, Body: body,
			Data: map[string]any{"dir": dir, "pane": p.ID},
		})
		if err == nil {
			a.att.mu.Lock()
			a.att.notified[p.ID] = true
			a.att.mu.Unlock()
		}
	}()
}

// attend follows every session change: a tab seen, closed or back to work takes
// its notification down, and the Dock shows how many tabs want you.
func (a *App) attend() {
	a.att.mu.Lock()
	var stale []string
	for id := range a.att.notified {
		if !a.sess.marked(id) {
			stale = append(stale, id)
			delete(a.att.notified, id)
		}
	}
	n := a.sess.attention()
	same := n == a.att.badge
	a.att.badge = n
	a.att.mu.Unlock()
	for _, id := range stale {
		_ = wr.RemoveDeliveredNotification(a.ctx, id)
	}
	if !same {
		label := ""
		if n > 0 {
			label = strconv.Itoa(n)
		}
		setDockBadge(label)
	}
}
