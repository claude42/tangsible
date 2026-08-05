package main

import (
	"fmt"
	"io"
)

// Render prints a plain-text dump of the current play/task/host tree. This
// is not the TUI — it always shows every host line under every task, since
// there's no navigation/expand state yet to decide what to hide.
func Render(w io.Writer, s *playbookState) {
	for _, play := range s.Plays {
		fmt.Fprintf(w, "PLAY: %s\n", play.Name)
		for _, task := range play.Tasks {
			ok, changed, skipped, failed := task.counts()
			fmt.Fprintf(w, "  TASK: %-40s OK: %d, Chgd: %d, Skip: %d, Fail: %d\n",
				task.Name, ok, changed, skipped, failed)
			for _, host := range task.HostOrder {
				fmt.Fprintf(w, "    %s: %s\n", host, task.Hosts[host])
			}
		}
	}
}
