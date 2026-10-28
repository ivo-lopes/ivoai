package cli

import (
	"fmt"
	"github.com/ivo-lopes/ivoai/internal/admin"
	"github.com/ivo-lopes/ivoai/internal/app"
	"sync"
)

func newAdminAudit(a *app.App) func(admin.Audit) {
	var mu sync.Mutex
	return func(event admin.Audit) {
		mu.Lock()
		defer mu.Unlock()
		fmt.Fprintf(a.Err, "ivoai admin: action=%s verdict=%s actor=%s\n", event.Action, event.Verdict, event.Actor)
	}
}
