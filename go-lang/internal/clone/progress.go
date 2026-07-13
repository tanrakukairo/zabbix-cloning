package clone

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tanrakukairo/zabbix-cloning/internal/logx"
)

type applyProgress struct {
	log      *logx.Logger
	quiet    bool
	label    string
	total    int
	order    []string
	counts   map[string]int
	done     int
	failures []string
}

func newApplyProgress(log *logx.Logger, quiet bool, label string, total int, actions ...string) *applyProgress {
	return &applyProgress{
		log: log, quiet: quiet, label: label, total: total, order: actions, counts: map[string]int{},
	}
}

func (p *applyProgress) record(action string) {
	p.counts[action]++
	p.done++
	p.report(false)
}

func (p *applyProgress) fail(target string, err error) {
	p.counts["failed"]++
	p.done++
	p.failures = append(p.failures, target)
	p.log.Failuref("%s [%s]: %v", p.label, target, err)
	p.report(false)
}

func (p *applyProgress) finish() {
	p.report(true)
	if p.quiet {
		return
	}
	p.log.Progress("\n")
	if len(p.failures) == 0 {
		return
	}
	sort.Strings(p.failures)
	p.log.Progress("    Failed %s:\n", p.label)
	for _, target := range p.failures {
		p.log.Progress("      %s\n", target)
	}
}

func (p *applyProgress) report(final bool) {
	message := p.summary()
	if p.quiet {
		if p.done > 0 && p.done%50 == 0 || final && (p.done == 0 || p.done%50 != 0) {
			p.log.Resultf("%s", message)
		}
		return
	}
	p.log.Progress("\r    %s", message)
}

func (p *applyProgress) summary() string {
	parts := make([]string, 0, len(p.order)+1)
	for _, action := range p.order {
		parts = append(parts, fmt.Sprintf("%s:%d", action, p.counts[action]))
	}
	parts = append(parts, fmt.Sprintf("failed:%d", p.counts["failed"]))
	return fmt.Sprintf("%s: %d/%d (%s)", p.label, p.done, p.total, strings.Join(parts, "/"))
}
