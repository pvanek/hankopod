// Command hankopod is a terminal admin console for Hanko projects.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sync"
	"text/tabwriter"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/pvanek/hankopod/internal/config"
	"github.com/pvanek/hankopod/internal/core"
	"github.com/pvanek/hankopod/internal/platform"
	"github.com/pvanek/hankopod/internal/tui"
)

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

const usage = `Usage: hankopod [flags] [command]

Commands:
  (none)    start the dashboard
  status    print a one-shot summary of every project and exit

Flags:
`

func main() {
	defaultPath, _ := config.DefaultPath()
	configPath := flag.String("config", defaultPath, "configuration file")
	sessions := flag.Bool("sessions", false, "status: also scan sessions (one request per user)")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Usage = func() {
		fmt.Fprint(flag.CommandLine.Output(), usage)
		flag.PrintDefaults()
	}
	flag.Parse()

	if *showVersion {
		fmt.Println("hankopod", version)
		return
	}
	if err := run(*configPath, flag.Arg(0), *sessions); err != nil {
		fmt.Fprintln(os.Stderr, "hankopod:", err)
		os.Exit(1)
	}
}

func run(configPath, command string, sessions bool) error {
	platform.FixDNS()

	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	if cfg.Warning != "" {
		fmt.Fprintln(os.Stderr, "hankopod: warning:", cfg.Warning)
	}
	svc := core.NewService(cfg.Projects, "hankopod/"+version, cfg.SessionScanLimit)

	switch command {
	case "":
		_, err = tea.NewProgram(tui.New(svc)).Run()
		return err
	case "status":
		return printStatus(svc, sessions)
	default:
		flag.Usage()
		return fmt.Errorf("unknown command %q", command)
	}
}

// printStatus fetches every project once and prints a table.
func printStatus(svc *core.Service, withSessions bool) error {
	projects := svc.Projects()
	now := time.Now()
	health := make([]core.Health, len(projects))
	users := make([]core.UserList, len(projects))
	stats := make([]core.SessionStats, len(projects))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	var wg sync.WaitGroup
	for i := range projects {
		wg.Go(func() { health[i] = svc.Health(ctx, i, now) })
		wg.Go(func() { users[i] = svc.Users(ctx, i, now) })
		if withSessions {
			wg.Go(func() { stats[i] = svc.Sessions(ctx, i, now) })
		}
	}
	wg.Wait()

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PROJECT\tSTATUS\tLATENCY\tUSERS\tMAU\tNEW 24H\tNEW 7D\tNEW 30D\tLAST SIGNUP\tSESSIONS\tLOGINS 24H\tFAILED 24H")
	var problems []string
	for i, h := range health {
		u := users[i]
		newCount := func(d time.Duration) string {
			if u.Err != nil {
				return "-"
			}
			n, exact := u.NewSince(now.Add(-d))
			if !exact {
				return fmt.Sprintf(">=%d", n)
			}
			return fmt.Sprint(n)
		}
		last := "-"
		if t := u.LastSignup(); !t.IsZero() {
			last = t.Local().Format("2006-01-02 15:04")
		}
		sess := "-"
		if withSessions && stats[i].UsersScanned > 0 {
			sess = fmt.Sprint(len(stats[i].Active))
			if !stats[i].Complete() {
				sess = ">=" + sess
			}
		}
		mau := num(h.MAU)
		if h.MAU >= 0 && !h.MAUExact {
			mau = ">=" + mau
		}
		fmt.Fprintf(w, "%s\t%s\t%dms\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			h.Project, h.Status, h.Status.Latency.Milliseconds(), num(u.Total), mau,
			newCount(24*time.Hour), newCount(7*24*time.Hour), newCount(core.RecentWindow),
			last, sess, num(h.Logins), num(h.LoginFailures))

		for _, e := range []struct {
			what string
			err  error
		}{{"status", h.Status.Err}, {"users", u.Err}, {"activity counts", h.Err}, {"sessions", stats[i].Err}} {
			if e.err != nil {
				problems = append(problems, fmt.Sprintf("%s %s: %v", h.Project, e.what, e.err))
			}
		}
	}
	if err := w.Flush(); err != nil {
		return err
	}
	for _, p := range problems {
		fmt.Fprintln(os.Stderr, "warning:", p)
	}
	return nil
}

func num(n int) string {
	if n < 0 {
		return "-"
	}
	return fmt.Sprint(n)
}
