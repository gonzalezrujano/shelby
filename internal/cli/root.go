package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"shelby/internal/config"
	"shelby/internal/engine"
	"shelby/internal/runner"
	"shelby/internal/server"
	"shelby/internal/store"
	"shelby/internal/tui"
)

func Run(args []string) int {
	if len(args) < 1 {
		printUsage()
		return 1
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "list":
		return cmdList(rest)
	case "show":
		return cmdShow(rest)
	case "run":
		return cmdRun(rest)
	case "logs":
		return cmdLogs(rest)
	case "add":
		return cmdAdd(rest)
	case "rm":
		return cmdRm(rest)
	case "tui":
		return cmdTUI(rest)
	case "serve":
		return cmdServe(rest)
	case "-h", "--help", "help":
		printUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		printUsage()
		return 1
	}
}

func printUsage() {
	fmt.Println(`shelby - metrics engine

usage: shelby <command> [args]

commands:
  add <file.yaml>       register pipeline (stores abs path; edits live)
  list                  list registered pipelines with last-run status
  show <name|slug>      show pipeline YAML + last run summary
  rm <name|slug>        unregister pipeline (drops run history)
  run <name|file.yaml>  execute pipeline; registered runs are recorded
  logs <name|slug>      show recent run history
  tui                   interactive dashboard
  serve [-addr :8080]   run scheduler daemon + web dashboard

Environment:
  SHELBY_HOME           override ~/.shelby`)
}

func openStore() (*store.Store, error) {
	return store.New()
}

func looksLikeFile(s string) bool {
	if strings.Contains(s, "/") || strings.HasSuffix(s, ".yaml") || strings.HasSuffix(s, ".yml") {
		_, err := os.Stat(s)
		return err == nil
	}
	return false
}

func cmdAdd(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "add: missing <file.yaml>")
		return 1
	}
	p, err := config.Load(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, "load:", err)
		return 1
	}
	st, err := openStore()
	if err != nil {
		fmt.Fprintln(os.Stderr, "store:", err)
		return 1
	}
	reg, err := st.Add(p, args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, "add:", err)
		return 1
	}
	fmt.Printf("registered: %s  (slug: %s)\n  path: %s\n", reg.Name, reg.Slug, reg.Path)
	return 0
}

func cmdList(_ []string) int {
	st, err := openStore()
	if err != nil {
		fmt.Fprintln(os.Stderr, "store:", err)
		return 1
	}
	regs, err := st.List()
	if err != nil {
		fmt.Fprintln(os.Stderr, "list:", err)
		return 1
	}
	if len(regs) == 0 {
		fmt.Println("no pipelines registered. use: shelby add <file.yaml>")
		return 0
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "SLUG\tNAME\tINTERVAL\tLAST RUN\tSTATUS\tPATH")
	for _, r := range regs {
		interval := "?"
		if p, err := config.Load(r.Path); err == nil {
			interval = p.Interval.String()
		}
		status := "-"
		lastRun := "-"
		if last, _ := st.LastRun(r.Slug); last != nil {
			status = last.Status
			lastRun = last.StartedAt.Local().Format("2006-01-02 15:04:05")
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", r.Slug, r.Name, interval, lastRun, status, shortPath(r.Path))
	}
	tw.Flush()
	return 0
}

func cmdShow(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "show: missing <name|slug>")
		return 1
	}
	st, err := openStore()
	if err != nil {
		fmt.Fprintln(os.Stderr, "store:", err)
		return 1
	}
	reg, err := st.Get(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("name: %s\nslug: %s\npath: %s\nregistered: %s\n\n", reg.Name, reg.Slug, reg.Path, reg.RegisteredAt.Local().Format(time.RFC3339))

	if b, err := os.ReadFile(reg.Path); err == nil {
		fmt.Println("--- yaml ---")
		os.Stdout.Write(b)
		if len(b) > 0 && b[len(b)-1] != '\n' {
			fmt.Println()
		}
	} else {
		fmt.Fprintf(os.Stderr, "warning: could not read %s: %v\n", reg.Path, err)
	}

	last, _ := st.LastRun(reg.Slug)
	if last == nil {
		fmt.Println("\n(no runs yet)")
		return 0
	}
	fmt.Printf("\n--- last run (%s) ---\n", last.RunID)
	fmt.Printf("status: %s  duration: %s  started: %s\n", last.Status, last.Duration, last.StartedAt.Local().Format(time.RFC3339))
	if last.Error != "" {
		fmt.Printf("error: %s\n", last.Error)
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "  STEP\tTYPE\tOK\tDURATION\tERROR")
	for _, s := range last.Steps {
		ok := "yes"
		if !s.OK {
			ok = "no"
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\n", s.ID, s.Type, ok, s.Duration, truncErr(s.Error, 60))
	}
	tw.Flush()
	return 0
}

func cmdRm(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "rm: missing <name|slug>")
		return 1
	}
	st, err := openStore()
	if err != nil {
		fmt.Fprintln(os.Stderr, "store:", err)
		return 1
	}
	if err := st.Remove(args[0]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("removed: %s\n", args[0])
	return 0
}

func cmdLogs(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "logs: missing <name|slug>")
		return 1
	}
	st, err := openStore()
	if err != nil {
		fmt.Fprintln(os.Stderr, "store:", err)
		return 1
	}
	reg, err := st.Get(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	runs, err := st.Runs(reg.Slug, 20)
	if err != nil {
		fmt.Fprintln(os.Stderr, "logs:", err)
		return 1
	}
	if len(runs) == 0 {
		fmt.Println("no runs yet")
		return 0
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "WHEN\tRUN ID\tSTATUS\tDURATION\tSTEPS\tERROR")
	for _, r := range runs {
		okCount := 0
		for _, s := range r.Steps {
			if s.OK {
				okCount++
			}
		}
		stepCol := fmt.Sprintf("%d/%d", okCount, len(r.Steps))
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			r.StartedAt.Local().Format("2006-01-02 15:04:05"),
			r.RunID, r.Status, r.Duration, stepCol, truncErr(r.Error, 60))
	}
	tw.Flush()
	return 0
}

func cmdRun(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "run: missing <name|file.yaml>")
		return 1
	}
	target := args[0]

	st, storeErr := openStore()
	var (
		p      *engine.Pipeline
		reg    *store.Registration
		loaded string
	)
	if looksLikeFile(target) {
		pp, err := config.Load(target)
		if err != nil {
			fmt.Fprintln(os.Stderr, "load:", err)
			return 1
		}
		p = pp
		abs, _ := filepath.Abs(target)
		loaded = abs
	} else {
		if storeErr != nil {
			fmt.Fprintln(os.Stderr, "store:", storeErr)
			return 1
		}
		r, err := st.Get(target)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		pp, err := config.Load(r.Path)
		if err != nil {
			fmt.Fprintln(os.Stderr, "load:", err)
			return 1
		}
		p = pp
		reg = &r
		loaded = r.Path
	}

	var recStore *store.Store
	var slug string
	if reg != nil && storeErr == nil {
		recStore = st
		slug = reg.Slug
	}
	res := runner.Execute(context.Background(), p, recStore, slug)

	fmt.Printf("pipeline: %s  (run: %s)\n", p.Name, res.RunID)
	fmt.Printf("source:   %s\n", shortPath(loaded))
	fmt.Printf("interval: %s\n\n", p.Interval)
	fmt.Println("steps:")
	for _, s := range p.Steps {
		out := res.RC.Steps[s.ID]
		status := "ok"
		if !out.OK {
			status = "fail"
		}
		fmt.Printf("  - %-14s %-12s %-6s %s\n", s.ID, s.Type, status, out.Duration)
		if out.Error != "" {
			fmt.Printf("      error: %s\n", truncErr(out.Error, 200))
		}
	}
	fmt.Println()

	if res.Output != nil {
		b, _ := json.MarshalIndent(res.Output, "", "  ")
		fmt.Printf("output:\n%s\n", string(b))
	}

	if res.Err != nil {
		fmt.Fprintln(os.Stderr, "\nrun error:", res.Err)
		return 1
	}
	return 0
}

func cmdTUI(_ []string) int {
	st, err := openStore()
	if err != nil {
		fmt.Fprintln(os.Stderr, "store:", err)
		return 1
	}
	if err := tui.Run(st); err != nil {
		fmt.Fprintln(os.Stderr, "tui:", err)
		return 1
	}
	return 0
}

func cmdServe(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	addr := fs.String("addr", ":8080", "listen address (e.g. :8080 or 127.0.0.1:8080)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	st, err := openStore()
	if err != nil {
		fmt.Fprintln(os.Stderr, "store:", err)
		return 1
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sched := server.NewScheduler(st)
	if err := sched.Start(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "scheduler:", err)
		return 1
	}

	srv := server.NewServer(sched, st, *addr)
	hs := srv.ListenAndServe(ctx)
	fmt.Printf("shelby serve: http://%s (store: %s)\n", *addr, st.Root)

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM)
	<-sigc
	fmt.Println("shutting down...")

	shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutCancel()
	_ = hs.Shutdown(shutCtx)
	cancel()
	sched.Wait()
	return 0
}

func shortPath(p string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, home) {
		return "~" + p[len(home):]
	}
	return p
}

func truncErr(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
