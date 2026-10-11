package tui

import (
	"fmt"
	"time"
)

func num(n int) string {
	if n < 0 {
		return "–"
	}
	return fmt.Sprint(n)
}

// fmtCount marks lower bounds with "≥".
func fmtCount(n int, exact bool) string {
	if exact {
		return fmt.Sprint(n)
	}
	return fmt.Sprintf("≥%d", n)
}

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return "–"
	}
	return t.Local().Format("2006-01-02 15:04")
}

func fmtLatency(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

// ago renders t relative to now, e.g. "5m ago".
func ago(t, now time.Time) string {
	if t.IsZero() {
		return "–"
	}
	d := now.Sub(t)
	if d < time.Minute {
		return "just now"
	}
	return fmtAge(d) + " ago"
}

// fmtAge renders a duration in its largest whole unit.
func fmtAge(d time.Duration) string {
	switch {
	case d < 0:
		return "0m"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

func fmtDuration(d time.Duration) string {
	switch {
	case d%time.Hour == 0:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d%time.Minute == 0:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return d.String()
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func truncate(s string, width int) string {
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	if width <= 1 {
		return string(r[:width])
	}
	return string(r[:width-1]) + "…"
}
