package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"shelby/internal/config"
	"shelby/internal/engine"
	"shelby/internal/lint"
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
	case "ls":
		return cmdList(rest)
	case "show":
		return cmdShow(rest)
	case "run":
		return cmdRun(rest)
	case "logs":
		return cmdLogs(rest)
	case "log":
		return cmdLog(rest)
	case "add":
		return cmdAdd(rest)
	case "update":
		return cmdUpdate(rest)
	case "rm":
		return cmdRm(rest)
	case "validate":
		return cmdValidate(rest)
	case "lint":
		return cmdLint(rest)
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
  update <name|slug> <file.yaml>
                        repoint registration at a new YAML (keeps run history)
  list|ls               list registered pipelines with last-run status
  show <name|slug>      show pipeline YAML + last run summary
  rm <name|slug>        unregister pipeline (drops run history)
  run [-v|--debug] <name|file.yaml>
                        execute pipeline; registered runs are recorded
                        -v        print each step's status/duration/data as it runs
                        --debug   like -v plus resolved input and full data JSON
  logs <name|slug>      show recent run history
  log <name|slug> [run-id]
                        show full detail of a run (defaults to last run)
  validate <name|file>  check YAML for structural/semantic errors
  lint <name|file>      report style and best-practice warnings
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

func cmdUpdate(args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "update: missing <name|slug> <file.yaml>")
		return 1
	}
	p, err := config.Load(args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "load:", err)
		return 1
	}
	st, err := openStore()
	if err != nil {
		fmt.Fprintln(os.Stderr, "store:", err)
		return 1
	}
	reg, err := st.Update(args[0], p, args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "update:", err)
		return 1
	}
	fmt.Printf("updated: %s  (slug: %s)\n  path: %s\n", reg.Name, reg.Slug, reg.Path)
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

func cmdLog(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "log: missing <name|slug> [run-id]")
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
	var rec *store.RunRecord
	if len(args) >= 2 {
		rec, err = st.Run(reg.Slug, args[1])
		if err != nil {
			fmt.Fprintln(os.Stderr, "log:", err)
			return 1
		}
		if rec == nil {
			fmt.Fprintf(os.Stderr, "log: run %q not found for %s\n", args[1], reg.Slug)
			return 1
		}
	} else {
		rec, err = st.LastRun(reg.Slug)
		if err != nil {
			fmt.Fprintln(os.Stderr, "log:", err)
			return 1
		}
		if rec == nil {
			fmt.Println("no runs yet")
			return 0
		}
	}
	printRunDetail(reg, rec)
	return 0
}

func printRunDetail(reg store.Registration, r *store.RunRecord) {
	fmt.Printf("pipeline: %s  (slug: %s)\n", reg.Name, reg.Slug)
	fmt.Printf("run:      %s\n", r.RunID)
	fmt.Printf("status:   %s  duration: %s\n", r.Status, r.Duration)
	fmt.Printf("started:  %s\n", r.StartedAt.Local().Format(time.RFC3339))
	fmt.Printf("finished: %s\n", r.FinishedAt.Local().Format(time.RFC3339))
	if r.Error != "" {
		fmt.Printf("error:    %s\n", r.Error)
	}
	fmt.Println()
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "  STEP\tTYPE\tOK\tDURATION\tERROR")
	for _, s := range r.Steps {
		ok := "yes"
		if !s.OK {
			ok = "no"
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\n", s.ID, s.Type, ok, s.Duration, truncErr(s.Error, 120))
	}
	tw.Flush()
	if len(r.Output) > 0 {
		fmt.Println("\noutput:")
		b, _ := json.MarshalIndent(r.Output, "", "  ")
		fmt.Println(string(b))
	}
}

func cmdRun(args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	verbose := fs.Bool("v", false, "verbose: print each step's status, duration, and data as it finishes")
	fs.BoolVar(verbose, "verbose", false, "alias for -v")
	debug := fs.Bool("debug", false, "debug: verbose + dump resolved input and full data/error for each step")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	rest := fs.Args()
	if len(rest) < 1 {
		fmt.Fprintln(os.Stderr, "run: missing <name|file.yaml>")
		return 1
	}
	target := rest[0]

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

	var obs engine.StepObserver
	if *verbose || *debug {
		fmt.Printf("pipeline: %s\nsource:   %s\ninterval: %s\n\nexecuting steps:\n", p.Name, shortPath(loaded), p.Interval)
		obs = stepPrinter(*debug)
	}
	res := runner.ExecuteWithObserver(context.Background(), p, recStore, slug, obs)

	if *verbose || *debug {
		fmt.Printf("\nrun: %s  duration: %s\n\n", res.RunID, res.Finished.Sub(res.Started))
	} else {
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
	}

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

// loadTarget resolves a CLI arg to a pipeline + source path. Accepts a YAML
// file path or a registered name/slug.
func loadTarget(target string) (*engine.Pipeline, string, error) {
	if looksLikeFile(target) {
		p, err := config.Load(target)
		if err != nil {
			return nil, target, err
		}
		abs, _ := filepath.Abs(target)
		return p, abs, nil
	}
	st, err := openStore()
	if err != nil {
		return nil, "", fmt.Errorf("store: %w", err)
	}
	reg, err := st.Get(target)
	if err != nil {
		return nil, "", err
	}
	p, err := config.Load(reg.Path)
	if err != nil {
		return nil, reg.Path, err
	}
	return p, reg.Path, nil
}

func cmdValidate(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "validate: missing <name|file.yaml>")
		return 1
	}
	p, src, err := loadTarget(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, "load:", err)
		return 1
	}
	issues := lint.Validate(p)
	lint.Sort(issues)
	fmt.Printf("validate: %s\n", shortPath(src))
	if len(issues) == 0 {
		fmt.Println("ok")
		return 0
	}
	for _, i := range issues {
		fmt.Println(i)
	}
	fmt.Printf("\n%d error(s)\n", len(issues))
	return 1
}

func cmdLint(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "lint: missing <name|file.yaml>")
		return 1
	}
	p, src, err := loadTarget(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, "load:", err)
		return 1
	}
	issues := lint.Lint(p)
	lint.Sort(issues)
	fmt.Printf("lint: %s\n", shortPath(src))
	if len(issues) == 0 {
		fmt.Println("no warnings")
		return 0
	}
	for _, i := range issues {
		fmt.Println(i)
	}
	fmt.Printf("\n%d warning(s)\n", len(issues))
	return 0
}

func shortPath(p string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, home) {
		return "~" + p[len(home):]
	}
	return p
}

// stepPrinter returns a StepObserver that prints per-step progress to stdout.
// If debug is true, it also dumps resolved input and full data/error JSON.
func stepPrinter(debug bool) engine.StepObserver {
	return func(s engine.Step, resolvedInput map[string]any, out engine.Output, err error) {
		status := "ok"
		if !out.OK {
			status = "fail"
		}
		fmt.Printf("  - %-14s %-12s %-6s %s\n", s.ID, s.Type, status, out.Duration)
		if debug && resolvedInput != nil {
			if b, jerr := json.MarshalIndent(resolvedInput, "      ", "  "); jerr == nil {
				fmt.Printf("      input:\n      %s\n", string(b))
			}
		}
		if len(out.Data) > 0 {
			if debug {
				if b, jerr := json.MarshalIndent(out.Data, "      ", "  "); jerr == nil {
					fmt.Printf("      data:\n      %s\n", string(b))
				}
			} else {
				fmt.Printf("      data: %s\n", summarizeData(out.Data))
			}
		}
		if out.Error != "" {
			limit := 200
			if debug {
				limit = 4000
			}
			fmt.Printf("      error: %s\n", truncErr(out.Error, limit))
		}
		if err != nil && out.Error == "" {
			fmt.Printf("      err: %v\n", err)
		}
	}
}

// summarizeData renders a one-line view of step data. Primitive values print
// inline; nested maps/arrays collapse to a size hint so long bodies don't
// dominate the verbose stream.
func summarizeData(m map[string]any) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, summarizeValue(m[k])))
	}
	return strings.Join(parts, " ")
}

func summarizeValue(v any) string {
	switch x := v.(type) {
	case nil:
		return "nil"
	case string:
		if len(x) > 60 {
			return fmt.Sprintf("%q", x[:57]+"...")
		}
		return fmt.Sprintf("%q", x)
	case map[string]any:
		return fmt.Sprintf("{%d keys}", len(x))
	case []any:
		return fmt.Sprintf("[%d items]", len(x))
	default:
		return fmt.Sprintf("%v", v)
	}
}

func truncErr(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
