package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"shelby/internal/viz"
)

func cmdViz(args []string) int {
	if len(args) < 1 {
		printVizUsage()
		return 1
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "serve":
		return cmdVizServe(rest)
	case "sandbox":
		return cmdVizSandbox(rest)
	case "init":
		return cmdVizInit(rest)
	case "build":
		return cmdVizBuild(rest)
	case "validate":
		return cmdVizValidate(rest)
	case "-h", "--help", "help":
		printVizUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "viz: unknown subcommand: %s\n", sub)
		printVizUsage()
		return 1
	}
}

func printVizUsage() {
	fmt.Print(`shelby viz - dashboard visualization engine

usage: shelby viz <subcommand> [args]

subcommands:
  serve <dashboard>     start dashboard server backed by Shelby API
                        --shelby  Shelby server URL (default: http://localhost:8080)
                        --port    port to listen on  (default: 5000)
                        --watch   reload browser on file changes
  sandbox <dashboard>   serve dashboard with mock data (no Shelby server needed)
                        --port    port to listen on  (default: 5000)
                        --watch   reload browser on file changes
  init <name>           scaffold a new dashboard project folder
                        --out     parent directory   (default: .)
  build <dashboard>     render static HTML bundle
                        --shelby  Shelby server URL  (default: http://localhost:8080)
                        --out     output directory   (default: dist)
  validate <dashboard>  check dashboard.yml and widget templates
`)
}

func cmdVizServe(args []string) int {
	fs := flag.NewFlagSet("viz serve", flag.ContinueOnError)
	shelby := fs.String("shelby", "http://localhost:8080", "Shelby server URL")
	port := fs.Int("port", 5000, "port to serve on")
	watch := fs.Bool("watch", false, "reload browser on template/config changes")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "viz serve: missing <dashboard>")
		return 1
	}
	dashPath := fs.Arg(0)
	if fs.NArg() > 1 {
		_ = fs.Parse(fs.Args()[1:])
	}

	fmt.Printf("  shelby-viz · serving %s\n", dashPath)
	fmt.Printf("  shelby API → %s\n", *shelby)
	fmt.Printf("  open       → http://localhost:%d\n", *port)
	if *watch {
		fmt.Printf("  live reload → watching %s\n", dashPath)
	}

	if err := viz.Serve(dashPath, *shelby, *port, *watch); err != nil {
		fmt.Fprintln(os.Stderr, "viz serve:", err)
		return 1
	}
	return 0
}

func cmdVizSandbox(args []string) int {
	fs := flag.NewFlagSet("viz sandbox", flag.ContinueOnError)
	port := fs.Int("port", 5000, "port to serve on")
	watch := fs.Bool("watch", false, "reload browser on template/config changes")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "viz sandbox: missing <dashboard>")
		return 1
	}
	dashPath := fs.Arg(0)
	if fs.NArg() > 1 {
		_ = fs.Parse(fs.Args()[1:])
	}

	fmt.Printf("  shelby-viz · sandbox %s\n", dashPath)
	fmt.Printf("  mock data  → %s/sandbox.yml\n", dashPath)
	fmt.Printf("  open       → http://localhost:%d\n", *port)
	if *watch {
		fmt.Printf("  live reload → watching %s\n", dashPath)
	}

	if err := viz.ServeSandbox(dashPath, *port, *watch); err != nil {
		fmt.Fprintln(os.Stderr, "viz sandbox:", err)
		return 1
	}
	return 0
}

func cmdVizInit(args []string) int {
	fs := flag.NewFlagSet("viz init", flag.ContinueOnError)
	out := fs.String("out", ".", "parent directory for the new project")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "viz init: missing <name>")
		return 1
	}
	name := fs.Arg(0)
	target := filepath.Join(*out, name)

	if _, err := os.Stat(target); err == nil {
		fmt.Fprintf(os.Stderr, "viz init: %s already exists\n", target)
		return 1
	}

	for _, sub := range []string{"widgets", "assets", "vars"} {
		if err := os.MkdirAll(filepath.Join(target, sub), 0o755); err != nil {
			fmt.Fprintln(os.Stderr, "viz init:", err)
			return 1
		}
	}

	dashYML := fmt.Sprintf("name: %s\ndescription: ''\nrefresh: 30s\n\nlayout:\n  columns: 12\n\nvars:\n  shelby_url: http://localhost:8080\n\nwidgets: []\n", name)
	if err := os.WriteFile(filepath.Join(target, "dashboard.yml"), []byte(dashYML), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "viz init:", err)
		return 1
	}

	sandboxYML := fmt.Sprintf("# Mock data for: shelby viz sandbox %s\n# Each field is a list of values (oldest → newest).\n# Timestamps are generated automatically.\n\npipelines:\n  %s:\n    my.field: [10, 20, 30, 40, 50]\n", target, name)
	if err := os.WriteFile(filepath.Join(target, "sandbox.yml"), []byte(sandboxYML), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "viz init:", err)
		return 1
	}

	readme := fmt.Sprintf("# %s\n\nA shelby-viz dashboard.\n", name)
	if err := os.WriteFile(filepath.Join(target, "README.md"), []byte(readme), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "viz init:", err)
		return 1
	}

	fmt.Printf("  created %s/\n", target)
	fmt.Printf("  → edit dashboard.yml to add widgets\n")
	fmt.Printf("  → edit sandbox.yml to add mock data\n")
	fmt.Printf("  → shelby viz sandbox %s   (no Shelby server needed)\n", target)
	fmt.Printf("  → shelby viz serve %s --shelby http://localhost:8080\n", target)
	return 0
}

func cmdVizBuild(args []string) int {
	fs := flag.NewFlagSet("viz build", flag.ContinueOnError)
	shelby := fs.String("shelby", "http://localhost:8080", "Shelby server URL")
	out := fs.String("out", "dist", "output directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "viz build: missing <dashboard>")
		return 1
	}
	dashPath := fs.Arg(0)
	if fs.NArg() > 1 {
		_ = fs.Parse(fs.Args()[1:])
	}

	dest, err := viz.BuildStatic(dashPath, *shelby, *out)
	if err != nil {
		fmt.Fprintln(os.Stderr, "viz build:", err)
		return 1
	}
	fmt.Printf("  built → %s\n", dest)
	return 0
}

func cmdVizValidate(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "viz validate: missing <dashboard>")
		return 1
	}
	dashPath := args[0]

	dashboard, err := viz.LoadDashboard(dashPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  dashboard.yml: %v\n", err)
		return 1
	}
	fmt.Printf("  dashboard.yml — %d widget(s)\n", len(dashboard.Widgets))

	r := viz.NewRenderer(dashPath, "")
	errs := r.ValidateTemplates(dashboard)
	if len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintln(os.Stderr, e)
		}
		return 1
	}

	for _, w := range dashboard.Widgets {
		tpath := w.Template
		if w.Type != "custom" {
			tpath = "widgets/" + w.Type + ".html.j2"
		}
		fmt.Printf("  %s (%s) → %s\n", w.ID, w.Type, tpath)
	}
	fmt.Println("  validation passed")
	return 0
}
