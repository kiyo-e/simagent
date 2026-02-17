package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type GlobalOptions struct {
	Target  string
	Timeout time.Duration
	JSON    bool
	Quiet   bool
}

type App struct {
	opts GlobalOptions
}

type AppError struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

func (e *AppError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

type ErrorEnvelope struct {
	OK    bool      `json:"ok"`
	Error *AppError `json:"error"`
}

type CommandResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

type SimTarget struct {
	Name      string `json:"name"`
	UDID      string `json:"udid"`
	Runtime   string `json:"runtime"`
	State     string `json:"state"`
	Available bool   `json:"available"`
}

type SavedTarget struct {
	Name    string `json:"name"`
	UDID    string `json:"udid"`
	Runtime string `json:"runtime"`
	State   string `json:"state"`
}

type Config struct {
	DefaultTarget *SavedTarget `json:"defaultTarget,omitempty"`
}

type LastFrame struct {
	OutDir     string            `json:"outDir"`
	Target     *SavedTarget      `json:"target,omitempty"`
	Artifacts  map[string]string `json:"artifacts"`
	CreatedAt  string            `json:"createdAt"`
	Elements   string            `json:"elements"`
	Transform  string            `json:"transform"`
	Screenshot string            `json:"screenshot,omitempty"`
	Annotated  string            `json:"annotated,omitempty"`
}

type FrameRect struct {
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	W    float64 `json:"w"`
	H    float64 `json:"h"`
	Unit string  `json:"unit"`
}

type FramePoint struct {
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	Unit string  `json:"unit"`
}

type ElementSource struct {
	Tool   string `json:"tool"`
	Method string `json:"method"`
}

type Element struct {
	Index   int           `json:"index"`
	ID      string        `json:"id"`
	Role    string        `json:"role,omitempty"`
	Label   string        `json:"label,omitempty"`
	Enabled bool          `json:"enabled"`
	Frame   FrameRect     `json:"frame"`
	Center  FramePoint    `json:"center"`
	Source  ElementSource `json:"source"`
	order   int
}

type Transform struct {
	Screen struct {
		W    float64 `json:"w"`
		H    float64 `json:"h"`
		Unit string  `json:"unit"`
	} `json:"screen"`
	Screenshot struct {
		W    int    `json:"w"`
		H    int    `json:"h"`
		Unit string `json:"unit"`
	} `json:"screenshot"`
	Scale    float64 `json:"scale"`
	SafeArea struct {
		Top    float64 `json:"top"`
		Bottom float64 `json:"bottom"`
		Left   float64 `json:"left"`
		Right  float64 `json:"right"`
		Unit   string  `json:"unit"`
	} `json:"safeArea"`
}

type FrameResult struct {
	Target    SimTarget `json:"target"`
	OutDir    string    `json:"outDir"`
	Artifacts struct {
		Screenshot string `json:"screenshot,omitempty"`
		Annotated  string `json:"annotated,omitempty"`
		UIRaw      string `json:"uiRaw,omitempty"`
		Elements   string `json:"elements,omitempty"`
		Transform  string `json:"transform,omitempty"`
	} `json:"artifacts"`
	Counts struct {
		All         int `json:"all"`
		Interactive int `json:"interactive"`
	} `json:"counts"`
}

type frameOptions struct {
	OutDir          string
	Screenshot      bool
	UI              bool
	Annotate        bool
	InteractiveOnly bool
	Order           string
	Format          string
	MinArea         float64
	IncludeRoles    map[string]bool
	ExcludeRoles    map[string]bool
	EmitJSON        bool
}

var interactiveRoleHints = []string{
	"button", "textfield", "securetextfield", "switch", "slider", "cell", "link",
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	opts, rest, err := parseGlobalOptions(args)
	if err != nil {
		return (&App{opts: opts}).fail(err, opts.JSON, stderr)
	}
	if len(rest) == 0 {
		printUsage(stderr)
		return 2
	}

	app := &App{
		opts: opts,
	}

	emitJSON, cmdErr := app.dispatch(rest)
	if cmdErr == nil {
		return 0
	}
	return app.fail(cmdErr, emitJSON, stderr)
}

func parseGlobalOptions(args []string) (GlobalOptions, []string, error) {
	opts := GlobalOptions{
		Timeout: 10 * time.Second,
	}
	rest := make([]string, 0, len(args))

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			rest = append(rest, args[i+1:]...)
			break
		}
		switch {
		case arg == "--target":
			if i+1 >= len(args) {
				return opts, rest, &AppError{Code: "USAGE", Message: "flag needs an argument: --target"}
			}
			opts.Target = args[i+1]
			i++
		case strings.HasPrefix(arg, "--target="):
			opts.Target = strings.TrimPrefix(arg, "--target=")
			if strings.TrimSpace(opts.Target) == "" {
				return opts, rest, &AppError{Code: "USAGE", Message: "flag needs an argument: --target"}
			}
		case arg == "--timeout":
			if i+1 >= len(args) {
				return opts, rest, &AppError{Code: "USAGE", Message: "flag needs an argument: --timeout"}
			}
			dur, err := time.ParseDuration(args[i+1])
			if err != nil {
				return opts, rest, &AppError{Code: "USAGE", Message: "invalid --timeout: " + err.Error()}
			}
			opts.Timeout = dur
			i++
		case strings.HasPrefix(arg, "--timeout="):
			value := strings.TrimPrefix(arg, "--timeout=")
			dur, err := time.ParseDuration(value)
			if err != nil {
				return opts, rest, &AppError{Code: "USAGE", Message: "invalid --timeout: " + err.Error()}
			}
			opts.Timeout = dur
		case arg == "--json":
			opts.JSON = true
		case arg == "--quiet":
			opts.Quiet = true
		default:
			rest = append(rest, arg)
		}
	}

	return opts, rest, nil
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: simagent [--target booted|<UDID>] [--timeout 10s] [--json] [--quiet] <command> [args]")
	fmt.Fprintln(w, "Commands: target, frame, ui, app, raw")
}

func (a *App) dispatch(args []string) (bool, error) {
	if len(args) == 0 {
		return a.opts.JSON, &AppError{Code: "USAGE", Message: "missing command"}
	}

	switch args[0] {
	case "target":
		return a.cmdTarget(args[1:])
	case "frame":
		return a.cmdFrame(args[1:])
	case "ui":
		return a.cmdUI(args[1:])
	case "app":
		return a.cmdApp(args[1:])
	case "raw":
		return a.cmdRaw(args[1:])
	default:
		return a.opts.JSON, &AppError{Code: "UNKNOWN_COMMAND", Message: "unknown command: " + args[0]}
	}
}

func (a *App) cmdTarget(args []string) (bool, error) {
	if len(args) == 0 {
		return a.opts.JSON, &AppError{Code: "USAGE", Message: "target subcommand required: list|set|show"}
	}

	sub := args[0]
	subArgs := args[1:]
	emitJSON := a.opts.JSON || hasJSONFlag(subArgs)

	switch sub {
	case "list":
		targets, err := a.listTargets()
		if err != nil {
			return emitJSON, err
		}
		if emitJSON {
			a.printJSON(map[string]any{"ok": true, "targets": targets})
		} else {
			for _, t := range targets {
				fmt.Printf("%s\t%s\t%s\t%s\n", t.Name, t.UDID, t.Runtime, t.State)
			}
		}
		return emitJSON, nil
	case "set":
		fs := flag.NewFlagSet("target set", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		localJSON := fs.Bool("json", false, "")
		if err := fs.Parse(subArgs); err != nil {
			return emitJSON, &AppError{Code: "USAGE", Message: err.Error()}
		}
		emitJSON = emitJSON || *localJSON
		vals := fs.Args()
		if len(vals) != 1 {
			return emitJSON, &AppError{Code: "USAGE", Message: "usage: simagent target set booted|<UDID>"}
		}
		t, err := a.resolveTarget(vals[0])
		if err != nil {
			return emitJSON, err
		}
		cfg, err := loadConfig()
		if err != nil {
			return emitJSON, err
		}
		cfg.DefaultTarget = &SavedTarget{Name: t.Name, UDID: t.UDID, Runtime: t.Runtime, State: t.State}
		if err := saveConfig(cfg); err != nil {
			return emitJSON, err
		}
		if emitJSON {
			a.printJSON(map[string]any{"ok": true, "target": t})
		} else {
			fmt.Printf("default target set: %s (%s)\n", t.Name, t.UDID)
		}
		return emitJSON, nil
	case "show":
		cfg, err := loadConfig()
		if err != nil {
			return emitJSON, err
		}
		if cfg.DefaultTarget == nil {
			return emitJSON, &AppError{Code: "NO_DEFAULT_TARGET", Message: "default target is not set"}
		}
		if emitJSON {
			a.printJSON(map[string]any{"ok": true, "target": cfg.DefaultTarget})
		} else {
			fmt.Printf("%s\t%s\t%s\t%s\n", cfg.DefaultTarget.Name, cfg.DefaultTarget.UDID, cfg.DefaultTarget.Runtime, cfg.DefaultTarget.State)
		}
		return emitJSON, nil
	default:
		return emitJSON, &AppError{Code: "USAGE", Message: "unknown target subcommand: " + sub}
	}
}

func (a *App) cmdFrame(args []string) (bool, error) {
	args = normalizeNegatedBools(args)
	fs := flag.NewFlagSet("frame", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	opts := frameOptions{}
	fs.StringVar(&opts.OutDir, "o", "", "output directory")
	fs.StringVar(&opts.OutDir, "out", "", "output directory")
	fs.BoolVar(&opts.Screenshot, "screenshot", true, "capture screenshot")
	fs.BoolVar(&opts.UI, "ui", true, "capture ui tree")
	fs.BoolVar(&opts.Annotate, "annotate", true, "annotate screenshot")
	fs.BoolVar(&opts.InteractiveOnly, "interactive-only", true, "keep interactive elements only")
	fs.StringVar(&opts.Order, "order", "reading", "reading|z|stable")
	fs.StringVar(&opts.Format, "format", "png", "png|jpg")
	fs.Float64Var(&opts.MinArea, "min-area", 0, "minimum area in pt^2")
	includeRoles := fs.String("include-roles", "", "comma separated roles")
	excludeRoles := fs.String("exclude-roles", "", "comma separated roles")
	localJSON := fs.Bool("json", false, "")

	emitJSON := a.opts.JSON || hasJSONFlag(args)
	if err := fs.Parse(args); err != nil {
		return emitJSON, &AppError{Code: "USAGE", Message: err.Error()}
	}
	opts.EmitJSON = emitJSON || *localJSON
	opts.IncludeRoles = csvSet(*includeRoles)
	opts.ExcludeRoles = csvSet(*excludeRoles)

	if opts.Order != "reading" && opts.Order != "z" && opts.Order != "stable" {
		return opts.EmitJSON, &AppError{Code: "USAGE", Message: "--order must be reading|z|stable"}
	}
	if opts.Format != "png" && opts.Format != "jpg" {
		return opts.EmitJSON, &AppError{Code: "USAGE", Message: "--format must be png|jpg"}
	}

	target, err := a.resolveTarget(a.opts.Target)
	if err != nil {
		return opts.EmitJSON, err
	}

	if opts.OutDir == "" {
		opts.OutDir = filepath.Join(os.TempDir(), "simagent", time.Now().Format("2006-01-02T15-04-05"))
	}
	if err := os.MkdirAll(opts.OutDir, 0o755); err != nil {
		return opts.EmitJSON, wrapErr("IO_ERROR", "failed to create output directory", err)
	}

	result := FrameResult{Target: target, OutDir: opts.OutDir}
	artifacts := map[string]string{}

	screenshotPath := filepath.Join(opts.OutDir, "screen."+opts.Format)
	uiRawPath := filepath.Join(opts.OutDir, "ui.raw.json")
	elementsPath := filepath.Join(opts.OutDir, "elements.json")
	transformPath := filepath.Join(opts.OutDir, "transform.json")
	annotatedPath := filepath.Join(opts.OutDir, "annotated.png")

	var screenshotSize image.Point
	if opts.Screenshot {
		_, err := a.runSimctl("io", target.UDID, "screenshot", screenshotPath)
		if err != nil {
			return opts.EmitJSON, wrapAppErrCode(err, "SIMCTL_FAILED", "failed to capture screenshot")
		}
		screenshotSize, _ = imageSize(screenshotPath)
		artifacts["screenshot"] = screenshotPath
		result.Artifacts.Screenshot = filepath.Base(screenshotPath)
	}

	var rawUI any
	var allElements []Element
	allCount := 0
	interactiveCount := 0
	if opts.UI {
		if _, lookErr := exec.LookPath("idb"); lookErr != nil {
			return opts.EmitJSON, &AppError{Code: "IDB_NOT_FOUND", Message: "idb is not installed or not in PATH"}
		}
		cmd, runErr := a.runIDB(target.UDID, "ui", "describe-all", "--json")
		if runErr != nil {
			a.logf("idb ui describe-all failed: %v", runErr)
			rawUI = map[string]any{"error": renderError(runErr)}
			if writeErr := writeJSONFile(uiRawPath, rawUI); writeErr != nil {
				return opts.EmitJSON, writeErr
			}
		} else {
			parsed, parseErr := decodeJSONOrWrap(cmd.Stdout)
			if parseErr != nil {
				rawUI = map[string]any{"raw": cmd.Stdout}
			} else {
				rawUI = parsed
			}
			if writeErr := writeJSONFile(uiRawPath, rawUI); writeErr != nil {
				return opts.EmitJSON, writeErr
			}
			allElements, allCount, interactiveCount = normalizeElements(rawUI, opts)
		}
		artifacts["uiRaw"] = uiRawPath
		result.Artifacts.UIRaw = filepath.Base(uiRawPath)
	}

	if allElements == nil {
		allElements = []Element{}
	}

	transform := deriveTransform(rawUI, allElements, screenshotSize)
	if err := writeJSONFile(transformPath, transform); err != nil {
		return opts.EmitJSON, err
	}
	artifacts["transform"] = transformPath
	result.Artifacts.Transform = filepath.Base(transformPath)

	if err := writeJSONFile(elementsPath, allElements); err != nil {
		return opts.EmitJSON, err
	}
	artifacts["elements"] = elementsPath
	result.Artifacts.Elements = filepath.Base(elementsPath)

	if opts.Annotate && opts.Screenshot {
		if err := createAnnotatedImage(screenshotPath, annotatedPath, allElements, transform); err != nil {
			return opts.EmitJSON, wrapErr("ANNOTATE_FAILED", "failed to create annotated image", err)
		}
		artifacts["annotated"] = annotatedPath
		result.Artifacts.Annotated = filepath.Base(annotatedPath)
	}

	result.Counts.All = allCount
	result.Counts.Interactive = interactiveCount

	last := LastFrame{
		OutDir:    opts.OutDir,
		Target:    &SavedTarget{Name: target.Name, UDID: target.UDID, Runtime: target.Runtime, State: target.State},
		Artifacts: artifacts,
		CreatedAt: time.Now().Format(time.RFC3339),
		Elements:  elementsPath,
		Transform: transformPath,
	}
	if v, ok := artifacts["screenshot"]; ok {
		last.Screenshot = v
	}
	if v, ok := artifacts["annotated"]; ok {
		last.Annotated = v
	}
	if err := saveLastFrame(last); err != nil {
		return opts.EmitJSON, err
	}

	if opts.EmitJSON {
		a.printJSON(result)
	} else {
		fmt.Printf("frame created: %s\n", opts.OutDir)
		fmt.Printf("elements: %d\n", len(allElements))
	}

	return opts.EmitJSON, nil
}

func (a *App) cmdUI(args []string) (bool, error) {
	if len(args) == 0 {
		return a.opts.JSON, &AppError{Code: "USAGE", Message: "ui subcommand required: tap|type|swipe|button"}
	}
	sub := args[0]
	args = args[1:]
	emitJSON := a.opts.JSON || hasJSONFlag(args)

	target, err := a.resolveTarget(a.opts.Target)
	if err != nil {
		return emitJSON, err
	}

	if _, lookErr := exec.LookPath("idb"); lookErr != nil {
		return emitJSON, &AppError{Code: "IDB_NOT_FOUND", Message: "idb is not installed or not in PATH"}
	}

	switch sub {
	case "tap":
		fs := flag.NewFlagSet("ui tap", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		unit := fs.String("unit", "pt", "pt|px")
		index := fs.Int("index", -1, "element index")
		id := fs.String("id", "", "element id")
		from := fs.String("from", "", "path to elements.json")
		localJSON := fs.Bool("json", false, "")
		if err := fs.Parse(args); err != nil {
			return emitJSON, &AppError{Code: "USAGE", Message: err.Error()}
		}
		emitJSON = emitJSON || *localJSON

		var x float64
		var y float64
		by := "coord"
		usedIndex := -1
		usedID := ""

		if *index >= 0 || *id != "" {
			elements, transform, err := loadElementsAndTransform(*from)
			if err != nil {
				return emitJSON, err
			}
			_ = transform
			elem, err := pickElement(elements, *index, *id)
			if err != nil {
				return emitJSON, err
			}
			x = elem.Center.X
			y = elem.Center.Y
			if *index >= 0 {
				by = "index"
				usedIndex = *index
			}
			if *id != "" {
				by = "id"
				usedID = *id
			}
		} else {
			vals := fs.Args()
			if len(vals) != 2 {
				return emitJSON, &AppError{Code: "USAGE", Message: "usage: simagent ui tap <x> <y> [--unit pt|px]"}
			}
			var errX error
			var errY error
			x, errX = strconv.ParseFloat(vals[0], 64)
			y, errY = strconv.ParseFloat(vals[1], 64)
			if errX != nil || errY != nil {
				return emitJSON, &AppError{Code: "USAGE", Message: "x and y must be numbers"}
			}
			if *unit == "px" {
				_, transform, err := loadElementsAndTransform(*from)
				if err != nil {
					return emitJSON, err
				}
				if transform.Scale <= 0 {
					return emitJSON, &AppError{Code: "COORD_TRANSFORM_FAILED", Message: "invalid transform scale"}
				}
				x = x / transform.Scale
				y = y / transform.Scale
			}
		}

		_, err = a.runIDB(target.UDID, "ui", "tap", idbCoordArg(x), idbCoordArg(y))
		if err != nil {
			return emitJSON, wrapAppErrCode(err, "IDB_UI_FAILED", "tap failed")
		}

		resp := map[string]any{"ok": true, "action": "tap", "by": by, "targetPt": map[string]any{"x": x, "y": y}}
		if usedIndex >= 0 {
			resp["index"] = usedIndex
		}
		if usedID != "" {
			resp["id"] = usedID
		}
		if emitJSON {
			a.printJSON(resp)
		} else {
			fmt.Printf("tap %.2f %.2f\n", x, y)
		}
		return emitJSON, nil

	case "type":
		fs := flag.NewFlagSet("ui type", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		into := fs.Bool("into", false, "tap target before typing")
		index := fs.Int("index", -1, "element index")
		id := fs.String("id", "", "element id")
		from := fs.String("from", "", "path to elements.json")
		localJSON := fs.Bool("json", false, "")
		if err := fs.Parse(args); err != nil {
			return emitJSON, &AppError{Code: "USAGE", Message: err.Error()}
		}
		emitJSON = emitJSON || *localJSON
		text := strings.Join(fs.Args(), " ")
		if text == "" {
			return emitJSON, &AppError{Code: "USAGE", Message: "usage: simagent ui type \"text\" [--into --index <n>|--id <id>]"}
		}

		if *into {
			elements, _, err := loadElementsAndTransform(*from)
			if err != nil {
				return emitJSON, err
			}
			elem, err := pickElement(elements, *index, *id)
			if err != nil {
				return emitJSON, err
			}
			if _, err := a.runIDB(target.UDID, "ui", "tap", idbCoordArg(elem.Center.X), idbCoordArg(elem.Center.Y)); err != nil {
				return emitJSON, wrapAppErrCode(err, "IDB_UI_FAILED", "focus tap failed")
			}
		}

		if _, err := a.runIDB(target.UDID, "ui", "text", text); err != nil {
			return emitJSON, wrapAppErrCode(err, "IDB_UI_FAILED", "text input failed")
		}

		resp := map[string]any{"ok": true, "action": "type", "text": text}
		if emitJSON {
			a.printJSON(resp)
		} else {
			fmt.Printf("typed: %s\n", text)
		}
		return emitJSON, nil

	case "swipe":
		fs := flag.NewFlagSet("ui swipe", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		index := fs.Int("index", -1, "element index")
		id := fs.String("id", "", "element id")
		from := fs.String("from", "", "path to elements.json")
		distance := fs.Float64("distance", 220, "distance in pt")
		localJSON := fs.Bool("json", false, "")
		if err := fs.Parse(args); err != nil {
			return emitJSON, &AppError{Code: "USAGE", Message: err.Error()}
		}
		emitJSON = emitJSON || *localJSON
		vals := fs.Args()
		if len(vals) < 1 {
			return emitJSON, &AppError{Code: "USAGE", Message: "usage: simagent ui swipe up|down|left|right [--index <n>|--id <id>]"}
		}
		direction := strings.ToLower(vals[0])
		if direction != "up" && direction != "down" && direction != "left" && direction != "right" {
			return emitJSON, &AppError{Code: "USAGE", Message: "direction must be up|down|left|right"}
		}

		startX := 196.0
		startY := 426.0
		_, transform, err := loadElementsAndTransform(*from)
		if err == nil {
			if transform.Screen.W > 0 {
				startX = transform.Screen.W / 2
			}
			if transform.Screen.H > 0 {
				startY = transform.Screen.H / 2
			}
		}

		if *index >= 0 || *id != "" {
			elements, _, err := loadElementsAndTransform(*from)
			if err != nil {
				return emitJSON, err
			}
			elem, err := pickElement(elements, *index, *id)
			if err != nil {
				return emitJSON, err
			}
			startX = elem.Center.X
			startY = elem.Center.Y
		}

		endX := startX
		endY := startY
		switch direction {
		case "up":
			endY -= *distance
		case "down":
			endY += *distance
		case "left":
			endX -= *distance
		case "right":
			endX += *distance
		}

		if _, err := a.runIDB(target.UDID, "ui", "swipe", idbCoordArg(startX), idbCoordArg(startY), idbCoordArg(endX), idbCoordArg(endY)); err != nil {
			return emitJSON, wrapAppErrCode(err, "IDB_UI_FAILED", "swipe failed")
		}

		resp := map[string]any{
			"ok":        true,
			"action":    "swipe",
			"direction": direction,
			"fromPt":    map[string]any{"x": startX, "y": startY},
			"toPt":      map[string]any{"x": endX, "y": endY},
		}
		if emitJSON {
			a.printJSON(resp)
		} else {
			fmt.Printf("swipe %s\n", direction)
		}
		return emitJSON, nil

	case "button":
		fs := flag.NewFlagSet("ui button", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		localJSON := fs.Bool("json", false, "")
		if err := fs.Parse(args); err != nil {
			return emitJSON, &AppError{Code: "USAGE", Message: err.Error()}
		}
		emitJSON = emitJSON || *localJSON
		vals := fs.Args()
		if len(vals) != 1 {
			return emitJSON, &AppError{Code: "USAGE", Message: "usage: simagent ui button HOME|LOCK|SIRI"}
		}
		button := strings.ToUpper(vals[0])
		if _, err := a.runIDB(target.UDID, "ui", "button", button); err != nil {
			return emitJSON, wrapAppErrCode(err, "IDB_UI_FAILED", "button failed")
		}
		resp := map[string]any{"ok": true, "action": "button", "button": button}
		if emitJSON {
			a.printJSON(resp)
		} else {
			fmt.Printf("button %s\n", button)
		}
		return emitJSON, nil

	default:
		return emitJSON, &AppError{Code: "USAGE", Message: "unknown ui subcommand: " + sub}
	}
}

func (a *App) cmdApp(args []string) (bool, error) {
	if len(args) == 0 {
		return a.opts.JSON, &AppError{Code: "USAGE", Message: "app subcommand required: openurl|launch|terminate|list"}
	}
	emitJSON := a.opts.JSON || hasJSONFlag(args[1:])
	target, err := a.resolveTarget(a.opts.Target)
	if err != nil {
		return emitJSON, err
	}

	sub := args[0]
	subArgs := args[1:]
	if sub == "list" {
		cmd, err := a.runSimctl("listapps", target.UDID)
		if err != nil {
			return emitJSON, wrapAppErrCode(err, "SIMCTL_FAILED", "list apps failed")
		}
		if emitJSON {
			a.printJSON(map[string]any{"ok": true, "apps": cmd.Stdout})
		} else {
			fmt.Print(cmd.Stdout)
		}
		return emitJSON, nil
	}

	subArgsNoTail, tailArgs := splitArgsTail(subArgs, "--args")
	fs := flag.NewFlagSet("app", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	bundleID := fs.String("bundle-id", "", "bundle id")
	localJSON := fs.Bool("json", false, "")
	if err := fs.Parse(subArgsNoTail); err != nil {
		return emitJSON, &AppError{Code: "USAGE", Message: err.Error()}
	}
	emitJSON = emitJSON || *localJSON

	switch sub {
	case "openurl":
		vals := fs.Args()
		if len(vals) != 1 {
			return emitJSON, &AppError{Code: "USAGE", Message: "usage: simagent app openurl \"<url>\""}
		}
		if _, err := a.runSimctl("openurl", target.UDID, vals[0]); err != nil {
			return emitJSON, wrapAppErrCode(err, "SIMCTL_FAILED", "openurl failed")
		}
		resp := map[string]any{"ok": true, "action": "openurl", "url": vals[0]}
		if emitJSON {
			a.printJSON(resp)
		} else {
			fmt.Printf("openurl %s\n", vals[0])
		}
		return emitJSON, nil
	case "launch":
		if *bundleID == "" {
			return emitJSON, &AppError{Code: "USAGE", Message: "usage: simagent app launch --bundle-id <id> [--args ...]"}
		}
		args := []string{"launch", target.UDID, *bundleID}
		args = append(args, tailArgs...)
		if _, err := a.runSimctl(args...); err != nil {
			return emitJSON, wrapAppErrCode(err, "SIMCTL_FAILED", "launch failed")
		}
		resp := map[string]any{"ok": true, "action": "launch", "bundleId": *bundleID, "args": tailArgs}
		if emitJSON {
			a.printJSON(resp)
		} else {
			fmt.Printf("launch %s\n", *bundleID)
		}
		return emitJSON, nil
	case "terminate":
		if *bundleID == "" {
			return emitJSON, &AppError{Code: "USAGE", Message: "usage: simagent app terminate --bundle-id <id>"}
		}
		if _, err := a.runSimctl("terminate", target.UDID, *bundleID); err != nil {
			return emitJSON, wrapAppErrCode(err, "SIMCTL_FAILED", "terminate failed")
		}
		resp := map[string]any{"ok": true, "action": "terminate", "bundleId": *bundleID}
		if emitJSON {
			a.printJSON(resp)
		} else {
			fmt.Printf("terminate %s\n", *bundleID)
		}
		return emitJSON, nil
	default:
		return emitJSON, &AppError{Code: "USAGE", Message: "unknown app subcommand: " + sub}
	}
}

func (a *App) cmdRaw(args []string) (bool, error) {
	if len(args) == 0 {
		return a.opts.JSON, &AppError{Code: "USAGE", Message: "usage: simagent raw simctl <...>|idb <...>"}
	}
	emitJSON := a.opts.JSON || hasJSONFlag(args)

	sub := args[0]
	pass := stripFirstJSON(args[1:])
	var res CommandResult
	var err error

	switch sub {
	case "simctl":
		res, err = a.runCommand("xcrun", append([]string{"simctl"}, pass...)...)
	case "idb":
		res, err = a.runCommand("idb", pass...)
	default:
		return emitJSON, &AppError{Code: "USAGE", Message: "raw command must be simctl|idb"}
	}
	if err != nil {
		return emitJSON, wrapAppErrCode(err, "RAW_FAILED", "raw command failed")
	}

	if emitJSON {
		a.printJSON(map[string]any{"ok": true, "stdout": res.Stdout, "stderr": res.Stderr, "exitCode": res.ExitCode})
	} else {
		fmt.Print(res.Stdout)
		if strings.TrimSpace(res.Stderr) != "" {
			fmt.Fprint(os.Stderr, res.Stderr)
		}
	}
	return emitJSON, nil
}

func (a *App) listTargets() ([]SimTarget, error) {
	cmd, err := a.runSimctl("list", "devices", "--json")
	if err != nil {
		return nil, wrapAppErrCode(err, "SIMCTL_FAILED", "failed to list simulators")
	}

	var payload struct {
		Devices map[string][]struct {
			Name    string `json:"name"`
			UDID    string `json:"udid"`
			State   string `json:"state"`
			IsAvail bool   `json:"isAvailable"`
			Avail   bool   `json:"available"`
		} `json:"devices"`
	}
	if err := json.Unmarshal([]byte(cmd.Stdout), &payload); err != nil {
		return nil, wrapErr("SIMCTL_FAILED", "invalid simctl devices json", err)
	}

	list := make([]SimTarget, 0)
	for runtime, devices := range payload.Devices {
		for _, d := range devices {
			available := d.IsAvail || d.Avail
			list = append(list, SimTarget{
				Name:      d.Name,
				UDID:      d.UDID,
				Runtime:   runtime,
				State:     d.State,
				Available: available,
			})
		}
	}

	sort.Slice(list, func(i, j int) bool {
		if list[i].State != list[j].State {
			return list[i].State > list[j].State
		}
		if list[i].Runtime != list[j].Runtime {
			return list[i].Runtime < list[j].Runtime
		}
		if list[i].Name != list[j].Name {
			return list[i].Name < list[j].Name
		}
		return list[i].UDID < list[j].UDID
	})
	return list, nil
}

func (a *App) resolveTarget(spec string) (SimTarget, error) {
	s := strings.TrimSpace(spec)
	if s == "" {
		cfg, err := loadConfig()
		if err == nil && cfg.DefaultTarget != nil && cfg.DefaultTarget.UDID != "" {
			s = cfg.DefaultTarget.UDID
		}
	}
	if s == "" {
		s = "booted"
	}

	targets, err := a.listTargets()
	if err != nil {
		return SimTarget{}, err
	}

	if s == "booted" {
		for _, t := range targets {
			if strings.EqualFold(t.State, "booted") {
				return t, nil
			}
		}
		return SimTarget{}, &AppError{Code: "NO_BOOTED_DEVICE", Message: "no booted simulator found"}
	}

	for _, t := range targets {
		if strings.EqualFold(t.UDID, s) {
			return t, nil
		}
	}
	return SimTarget{}, &AppError{Code: "TARGET_NOT_FOUND", Message: "target not found: " + s}
}

func (a *App) runSimctl(args ...string) (CommandResult, error) {
	return a.runCommand("xcrun", append([]string{"simctl"}, args...)...)
}

func (a *App) runIDB(udid string, args ...string) (CommandResult, error) {
	full := append([]string{}, args...)
	full = append(full, "--udid", udid)
	return a.runCommand("idb", full...)
}

func (a *App) runCommand(name string, args ...string) (CommandResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), a.opts.Timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	res := CommandResult{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: 0}
	if err == nil {
		return res, nil
	}

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return res, &AppError{Code: "TIMEOUT", Message: fmt.Sprintf("command timed out: %s %s", name, strings.Join(args, " ")), Details: map[string]any{"stderr": res.Stderr}}
	}

	if ee := (*exec.ExitError)(nil); errors.As(err, &ee) {
		res.ExitCode = ee.ExitCode()
		return res, &AppError{Code: "COMMAND_FAILED", Message: fmt.Sprintf("command failed: %s", name), Details: map[string]any{"stderr": res.Stderr, "stdout": res.Stdout, "exitCode": res.ExitCode}}
	}

	if errors.Is(err, exec.ErrNotFound) {
		return res, &AppError{Code: "COMMAND_NOT_FOUND", Message: fmt.Sprintf("command not found: %s", name)}
	}

	return res, wrapErr("COMMAND_FAILED", fmt.Sprintf("failed to run: %s", name), err)
}

func normalizeElements(rawUI any, opts frameOptions) ([]Element, int, int) {
	nodes := make([]candidateNode, 0, 64)
	order := 0
	walkCandidates(rawUI, "", &order, &nodes)
	allCandidates := make([]Element, 0, len(nodes))
	allCount := 0
	interactiveCount := 0

	for _, node := range nodes {
		elem, ok := elementFromCandidate(node)
		if !ok {
			continue
		}
		if elem.Frame.W*elem.Frame.H < opts.MinArea {
			continue
		}
		roleKey := strings.ToLower(strings.TrimSpace(elem.Role))
		if len(opts.IncludeRoles) > 0 && !opts.IncludeRoles[roleKey] {
			continue
		}
		if opts.ExcludeRoles[roleKey] {
			continue
		}
		if !elem.Enabled {
			continue
		}
		allCount++
		isInteractive := isInteractiveRole(elem.Role)
		if isInteractive {
			interactiveCount++
		}
		if opts.InteractiveOnly && !isInteractive {
			continue
		}
		elem.order = node.Order
		allCandidates = append(allCandidates, elem)
	}

	switch opts.Order {
	case "reading":
		sort.Slice(allCandidates, func(i, j int) bool {
			if allCandidates[i].Center.Y != allCandidates[j].Center.Y {
				return allCandidates[i].Center.Y < allCandidates[j].Center.Y
			}
			if allCandidates[i].Center.X != allCandidates[j].Center.X {
				return allCandidates[i].Center.X < allCandidates[j].Center.X
			}
			return allCandidates[i].ID < allCandidates[j].ID
		})
	case "stable":
		sort.Slice(allCandidates, func(i, j int) bool {
			return allCandidates[i].ID < allCandidates[j].ID
		})
	case "z":
		sort.Slice(allCandidates, func(i, j int) bool {
			return allCandidates[i].order < allCandidates[j].order
		})
	}

	for i := range allCandidates {
		allCandidates[i].Index = i + 1
	}

	return allCandidates, allCount, interactiveCount
}

type candidateNode struct {
	Path  string
	Order int
	Map   map[string]any
}

func walkCandidates(v any, path string, order *int, out *[]candidateNode) {
	switch typed := v.(type) {
	case map[string]any:
		*order++
		*out = append(*out, candidateNode{Path: path, Order: *order, Map: typed})
		for k, child := range typed {
			next := path + "/" + k
			walkCandidates(child, next, order, out)
		}
	case []any:
		for i, child := range typed {
			next := fmt.Sprintf("%s/%d", path, i)
			walkCandidates(child, next, order, out)
		}
	}
}

func elementFromCandidate(node candidateNode) (Element, bool) {
	rect, ok := findRect(node.Map)
	if !ok || rect.W <= 0 || rect.H <= 0 {
		return Element{}, false
	}

	id := firstString(node.Map, []string{"id", "identifier", "uid", "axPath", "path", "accessibilityIdentifier"})
	if id == "" {
		id = "axpath:" + node.Path
	}
	role := firstString(node.Map, []string{"role", "type", "elementType", "axRole", "roleDescription"})
	label := firstString(node.Map, []string{"label", "name", "title", "value", "placeholder"})
	enabled := firstBoolWithDefault(node.Map, []string{"enabled", "isEnabled"}, true)

	return Element{
		ID:      id,
		Role:    role,
		Label:   label,
		Enabled: enabled,
		Frame:   rect,
		Center:  FramePoint{X: rect.X + rect.W/2, Y: rect.Y + rect.H/2, Unit: "pt"},
		Source:  ElementSource{Tool: "idb", Method: "describe-all"},
	}, true
}

func findRect(m map[string]any) (FrameRect, bool) {
	if rect, ok := rectFromAny(m["frame"]); ok {
		return rect, true
	}
	if rect, ok := rectFromAny(m["bounds"]); ok {
		return rect, true
	}
	if rect, ok := rectFromAny(m["rect"]); ok {
		return rect, true
	}
	if rect, ok := rectFromAny(m); ok {
		return rect, true
	}
	return FrameRect{}, false
}

func rectFromAny(v any) (FrameRect, bool) {
	mapVal, ok := v.(map[string]any)
	if !ok {
		return FrameRect{}, false
	}
	x, okX := getFloatAny(mapVal, []string{"x", "left", "originX"})
	y, okY := getFloatAny(mapVal, []string{"y", "top", "originY"})
	w, okW := getFloatAny(mapVal, []string{"w", "width"})
	h, okH := getFloatAny(mapVal, []string{"h", "height"})
	if okX && okY && okW && okH {
		return FrameRect{X: x, Y: y, W: w, H: h, Unit: "pt"}, true
	}
	return FrameRect{}, false
}

func deriveTransform(rawUI any, elements []Element, screenshotSize image.Point) Transform {
	var t Transform
	t.Screen.Unit = "pt"
	t.Screenshot.Unit = "px"
	t.SafeArea.Unit = "pt"
	t.Scale = 1
	if screenshotSize.X > 0 {
		t.Screenshot.W = screenshotSize.X
	}
	if screenshotSize.Y > 0 {
		t.Screenshot.H = screenshotSize.Y
	}

	maxW := 0.0
	maxH := 0.0
	for _, e := range elements {
		if e.Frame.X+e.Frame.W > maxW {
			maxW = e.Frame.X + e.Frame.W
		}
		if e.Frame.Y+e.Frame.H > maxH {
			maxH = e.Frame.Y + e.Frame.H
		}
	}
	if maxW == 0 || maxH == 0 {
		if rootMap, ok := rawUI.(map[string]any); ok {
			if rect, ok := findRect(rootMap); ok {
				if maxW == 0 {
					maxW = rect.W
				}
				if maxH == 0 {
					maxH = rect.H
				}
			}
		}
	}

	if maxW == 0 && t.Screenshot.W > 0 {
		maxW = float64(t.Screenshot.W)
	}
	if maxH == 0 && t.Screenshot.H > 0 {
		maxH = float64(t.Screenshot.H)
	}

	t.Screen.W = maxW
	t.Screen.H = maxH

	if t.Screen.W > 0 && t.Screenshot.W > 0 {
		t.Scale = float64(t.Screenshot.W) / t.Screen.W
	} else if t.Screen.H > 0 && t.Screenshot.H > 0 {
		t.Scale = float64(t.Screenshot.H) / t.Screen.H
	}

	return t
}

func createAnnotatedImage(srcPath, dstPath string, elements []Element, transform Transform) error {
	src, format, err := decodeImage(srcPath)
	if err != nil {
		return err
	}

	rgba := image.NewRGBA(src.Bounds())
	draw.Draw(rgba, rgba.Bounds(), src, src.Bounds().Min, draw.Src)

	scale := transform.Scale
	if scale <= 0 {
		scale = 1
	}

	for _, e := range elements {
		r := image.Rect(
			int(math.Round(e.Frame.X*scale)),
			int(math.Round(e.Frame.Y*scale)),
			int(math.Round((e.Frame.X+e.Frame.W)*scale)),
			int(math.Round((e.Frame.Y+e.Frame.H)*scale)),
		)
		if r.Dx() <= 1 || r.Dy() <= 1 {
			continue
		}

		stroke := annotationStrokeColor(e.Index)
		strokeThickness := maxInt(3, int(math.Round(scale*1.5)))
		strokeRect(rgba, r, stroke, strokeThickness)

		label := strconv.Itoa(e.Index)
		digitScale := maxInt(4, int(math.Round(scale*2)))
		digitWidth := 8 * digitScale
		digitHeight := 8 * digitScale
		labelPaddingX := maxInt(4, digitScale)
		labelPaddingY := maxInt(3, digitScale/2)
		labelW := labelPaddingX*2 + len(label)*digitWidth
		labelH := labelPaddingY*2 + digitHeight
		labelTop := maxInt(0, r.Min.Y-labelH-2)
		labelRect := image.Rect(r.Min.X, labelTop, r.Min.X+labelW, labelTop+labelH)
		labelRect = clampRectToBounds(labelRect, rgba.Bounds())
		labelBG := color.RGBA{R: stroke.R, G: stroke.G, B: stroke.B, A: 220}
		fillRect(rgba, labelRect, labelBG)
		strokeRect(rgba, labelRect, color.RGBA{R: 255, G: 255, B: 255, A: 230}, maxInt(1, digitScale/4))
		drawDigits(rgba, labelRect.Min.X+labelPaddingX, labelRect.Min.Y+labelPaddingY, label, annotationTextColor(stroke), digitScale)
	}

	f, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	defer f.Close()

	if format == "jpeg" {
		return png.Encode(f, rgba)
	}
	return png.Encode(f, rgba)
}

func decodeImage(path string) (image.Image, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	img, format, err := image.Decode(f)
	return img, format, err
}

func strokeRect(img *image.RGBA, r image.Rectangle, c color.Color, thickness int) {
	for i := 0; i < thickness; i++ {
		top := image.Rect(r.Min.X, r.Min.Y+i, r.Max.X, r.Min.Y+i+1)
		bottom := image.Rect(r.Min.X, r.Max.Y-1-i, r.Max.X, r.Max.Y-i)
		left := image.Rect(r.Min.X+i, r.Min.Y, r.Min.X+i+1, r.Max.Y)
		right := image.Rect(r.Max.X-1-i, r.Min.Y, r.Max.X-i, r.Max.Y)
		fillRect(img, top, c)
		fillRect(img, bottom, c)
		fillRect(img, left, c)
		fillRect(img, right, c)
	}
}

func fillRect(img draw.Image, r image.Rectangle, c color.Color) {
	draw.Draw(img, r, &image.Uniform{C: c}, image.Point{}, draw.Src)
}

func clampRectToBounds(r image.Rectangle, bounds image.Rectangle) image.Rectangle {
	if r.Dx() >= bounds.Dx() {
		r.Min.X = bounds.Min.X
		r.Max.X = bounds.Max.X
	} else {
		if r.Min.X < bounds.Min.X {
			r = r.Add(image.Pt(bounds.Min.X-r.Min.X, 0))
		}
		if r.Max.X > bounds.Max.X {
			r = r.Add(image.Pt(bounds.Max.X-r.Max.X, 0))
		}
	}

	if r.Dy() >= bounds.Dy() {
		r.Min.Y = bounds.Min.Y
		r.Max.Y = bounds.Max.Y
	} else {
		if r.Min.Y < bounds.Min.Y {
			r = r.Add(image.Pt(0, bounds.Min.Y-r.Min.Y))
		}
		if r.Max.Y > bounds.Max.Y {
			r = r.Add(image.Pt(0, bounds.Max.Y-r.Max.Y))
		}
	}

	return r
}

func drawDigits(img *image.RGBA, x int, y int, text string, c color.Color, scale int) {
	if scale <= 0 {
		scale = 1
	}
	offset := 0
	for _, ch := range text {
		if ch >= '0' && ch <= '9' {
			drawDigit(img, x+offset, y, int(ch-'0'), c, scale)
			offset += 8 * scale
		}
	}
}

func drawDigit(img *image.RGBA, x int, y int, digit int, c color.Color, scale int) {
	segments := [10][7]bool{
		{true, true, true, true, true, true, false},
		{false, true, true, false, false, false, false},
		{true, true, false, true, true, false, true},
		{true, true, true, true, false, false, true},
		{false, true, true, false, false, true, true},
		{true, false, true, true, false, true, true},
		{true, false, true, true, true, true, true},
		{true, true, true, false, false, false, false},
		{true, true, true, true, true, true, true},
		{true, true, true, true, false, true, true},
	}
	if digit < 0 || digit > 9 {
		return
	}
	if scale <= 0 {
		scale = 1
	}
	scaleRect := func(x0 int, y0 int, x1 int, y1 int) image.Rectangle {
		return image.Rect(x+x0*scale, y+y0*scale, x+x1*scale, y+y1*scale)
	}
	on := segments[digit]
	if on[0] {
		fillRect(img, scaleRect(1, 0, 6, 1), c)
	}
	if on[1] {
		fillRect(img, scaleRect(6, 1, 7, 4), c)
	}
	if on[2] {
		fillRect(img, scaleRect(6, 4, 7, 7), c)
	}
	if on[3] {
		fillRect(img, scaleRect(1, 7, 6, 8), c)
	}
	if on[4] {
		fillRect(img, scaleRect(0, 4, 1, 7), c)
	}
	if on[5] {
		fillRect(img, scaleRect(0, 1, 1, 4), c)
	}
	if on[6] {
		fillRect(img, scaleRect(1, 3, 6, 4), c)
	}
}

func annotationStrokeColor(index int) color.RGBA {
	palette := []color.RGBA{
		{R: 220, G: 53, B: 69, A: 255},
		{R: 13, G: 110, B: 253, A: 255},
		{R: 25, G: 135, B: 84, A: 255},
		{R: 255, G: 140, B: 0, A: 255},
		{R: 111, G: 66, B: 193, A: 255},
		{R: 32, G: 201, B: 151, A: 255},
		{R: 214, G: 51, B: 132, A: 255},
		{R: 13, G: 202, B: 240, A: 255},
		{R: 102, G: 16, B: 242, A: 255},
		{R: 108, G: 117, B: 125, A: 255},
	}
	if len(palette) == 0 {
		return color.RGBA{R: 255, G: 64, B: 64, A: 255}
	}
	if index < 0 {
		index = -index
	}
	return palette[index%len(palette)]
}

func annotationTextColor(bg color.RGBA) color.RGBA {
	brightness := int(bg.R)*299 + int(bg.G)*587 + int(bg.B)*114
	if brightness >= 140000 {
		return color.RGBA{R: 0, G: 0, B: 0, A: 255}
	}
	return color.RGBA{R: 255, G: 255, B: 255, A: 255}
}

func imageSize(path string) (image.Point, error) {
	f, err := os.Open(path)
	if err != nil {
		return image.Point{}, err
	}
	defer f.Close()
	cfg, format, err := image.DecodeConfig(f)
	if err != nil {
		return image.Point{}, err
	}
	_ = format
	return image.Pt(cfg.Width, cfg.Height), nil
}

func (a *App) fail(err error, emitJSON bool, stderr io.Writer) int {
	appErr := toAppError(err)
	if emitJSON {
		a.printJSON(ErrorEnvelope{OK: false, Error: appErr})
		return 1
	}
	fmt.Fprintf(stderr, "error [%s]: %s\n", appErr.Code, appErr.Message)
	if len(appErr.Details) > 0 {
		if b, marshalErr := json.MarshalIndent(appErr.Details, "", "  "); marshalErr == nil {
			fmt.Fprintf(stderr, "%s\n", string(b))
		}
	}
	return 1
}

func (a *App) printJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func (a *App) logf(format string, args ...any) {
	if a.opts.Quiet {
		return
	}
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}

func configDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", wrapErr("IO_ERROR", "failed to resolve home directory", err)
	}
	dir := filepath.Join(home, ".config", "simagent")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", wrapErr("IO_ERROR", "failed to create config directory", err)
	}
	return dir, nil
}

func configPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

func lastFramePath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "last_frame.json"), nil
}

func loadConfig() (Config, error) {
	path, err := configPath()
	if err != nil {
		return Config{}, err
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, wrapErr("IO_ERROR", "failed to read config", err)
	}
	var cfg Config
	if err := json.Unmarshal(b, &cfg); err != nil {
		return Config{}, wrapErr("IO_ERROR", "failed to parse config", err)
	}
	return cfg, nil
}

func saveConfig(cfg Config) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return wrapErr("IO_ERROR", "failed to encode config", err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return wrapErr("IO_ERROR", "failed to write config", err)
	}
	return nil
}

func saveLastFrame(frame LastFrame) error {
	path, err := lastFramePath()
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(frame, "", "  ")
	if err != nil {
		return wrapErr("IO_ERROR", "failed to encode last_frame", err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return wrapErr("IO_ERROR", "failed to write last_frame", err)
	}
	return nil
}

func loadLastFrame() (LastFrame, error) {
	path, err := lastFramePath()
	if err != nil {
		return LastFrame{}, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return LastFrame{}, &AppError{Code: "NO_LAST_FRAME", Message: "no last frame found; run `simagent frame` first"}
		}
		return LastFrame{}, wrapErr("IO_ERROR", "failed to read last_frame", err)
	}
	var frame LastFrame
	if err := json.Unmarshal(b, &frame); err != nil {
		return LastFrame{}, wrapErr("IO_ERROR", "failed to parse last_frame", err)
	}
	return frame, nil
}

func loadElementsAndTransform(from string) ([]Element, Transform, error) {
	elementsPath := from
	transformPath := ""
	if strings.TrimSpace(elementsPath) == "" {
		last, err := loadLastFrame()
		if err != nil {
			return nil, Transform{}, err
		}
		elementsPath = last.Elements
		transformPath = last.Transform
	} else {
		transformPath = filepath.Join(filepath.Dir(elementsPath), "transform.json")
	}

	elemBytes, err := os.ReadFile(elementsPath)
	if err != nil {
		return nil, Transform{}, wrapErr("IO_ERROR", "failed to read elements json", err)
	}
	var elements []Element
	if err := json.Unmarshal(elemBytes, &elements); err != nil {
		return nil, Transform{}, wrapErr("IO_ERROR", "failed to parse elements json", err)
	}

	var transform Transform
	if transformBytes, err := os.ReadFile(transformPath); err == nil {
		_ = json.Unmarshal(transformBytes, &transform)
	}
	return elements, transform, nil
}

func pickElement(elements []Element, index int, id string) (Element, error) {
	if index >= 0 {
		for _, e := range elements {
			if e.Index == index {
				return e, nil
			}
		}
		return Element{}, &AppError{Code: "ELEMENT_NOT_FOUND", Message: fmt.Sprintf("element index not found: %d", index)}
	}
	if id != "" {
		for _, e := range elements {
			if e.ID == id {
				return e, nil
			}
		}
		return Element{}, &AppError{Code: "ELEMENT_NOT_FOUND", Message: "element id not found: " + id}
	}
	return Element{}, &AppError{Code: "USAGE", Message: "index or id is required"}
}

func hasJSONFlag(args []string) bool {
	for _, arg := range args {
		if arg == "--json" {
			return true
		}
	}
	return false
}

func stripFirstJSON(args []string) []string {
	out := make([]string, 0, len(args))
	stripped := false
	for _, a := range args {
		if !stripped && a == "--json" {
			stripped = true
			continue
		}
		out = append(out, a)
	}
	return out
}

func splitArgsTail(args []string, marker string) ([]string, []string) {
	for i, arg := range args {
		if arg == marker {
			return args[:i], args[i+1:]
		}
	}
	return args, nil
}

func csvSet(s string) map[string]bool {
	out := map[string]bool{}
	for _, part := range strings.Split(s, ",") {
		v := strings.TrimSpace(strings.ToLower(part))
		if v != "" {
			out[v] = true
		}
	}
	return out
}

func normalizeNegatedBools(args []string) []string {
	repl := map[string]string{
		"--no-screenshot":       "--screenshot=false",
		"--no-ui":               "--ui=false",
		"--no-annotate":         "--annotate=false",
		"--no-interactive-only": "--interactive-only=false",
	}
	out := make([]string, 0, len(args))
	for _, arg := range args {
		if v, ok := repl[arg]; ok {
			out = append(out, v)
		} else {
			out = append(out, arg)
		}
	}
	return out
}

func isInteractiveRole(role string) bool {
	r := strings.ToLower(strings.TrimSpace(role))
	if r == "" {
		return false
	}
	for _, hint := range interactiveRoleHints {
		if strings.Contains(r, hint) {
			return true
		}
	}
	return false
}

func firstString(m map[string]any, keys []string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			s := strings.TrimSpace(fmt.Sprint(v))
			if s != "" && s != "<nil>" {
				return s
			}
		}
	}
	return ""
}

func firstBoolWithDefault(m map[string]any, keys []string, fallback bool) bool {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			switch typed := v.(type) {
			case bool:
				return typed
			case string:
				if parsed, err := strconv.ParseBool(strings.TrimSpace(typed)); err == nil {
					return parsed
				}
			}
		}
	}
	return fallback
}

func getFloatAny(m map[string]any, keys []string) (float64, bool) {
	for _, k := range keys {
		v, ok := m[k]
		if !ok {
			continue
		}
		switch typed := v.(type) {
		case float64:
			return typed, true
		case float32:
			return float64(typed), true
		case int:
			return float64(typed), true
		case int64:
			return float64(typed), true
		case json.Number:
			if f, err := typed.Float64(); err == nil {
				return f, true
			}
		case string:
			if f, err := strconv.ParseFloat(strings.TrimSpace(typed), 64); err == nil {
				return f, true
			}
		}
	}
	return 0, false
}

func writeJSONFile(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return wrapErr("IO_ERROR", "failed to encode json", err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return wrapErr("IO_ERROR", "failed to write json file", err)
	}
	return nil
}

func decodeJSONOrWrap(s string) (any, error) {
	var out any
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	if err := dec.Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

func toAppError(err error) *AppError {
	if err == nil {
		return &AppError{Code: "UNKNOWN", Message: "unknown error"}
	}
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr
	}
	return &AppError{Code: "UNKNOWN", Message: err.Error()}
}

func wrapErr(code, msg string, err error) *AppError {
	if err == nil {
		return &AppError{Code: code, Message: msg}
	}
	return &AppError{Code: code, Message: msg, Details: map[string]any{"cause": err.Error()}}
}

func wrapAppErrCode(err error, code, msg string) *AppError {
	appErr := toAppError(err)
	if appErr.Code == code {
		return appErr
	}
	details := map[string]any{}
	if len(appErr.Details) > 0 {
		for k, v := range appErr.Details {
			details[k] = v
		}
	}
	details["causeCode"] = appErr.Code
	details["causeMessage"] = appErr.Message
	return &AppError{Code: code, Message: msg, Details: details}
}

func renderError(err error) map[string]any {
	appErr := toAppError(err)
	out := map[string]any{"code": appErr.Code, "message": appErr.Message}
	if len(appErr.Details) > 0 {
		out["details"] = appErr.Details
	}
	return out
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func idbCoordArg(v float64) string {
	return strconv.Itoa(int(math.Round(v)))
}

func init() {
	image.RegisterFormat("jpeg", "jpeg", jpeg.Decode, jpeg.DecodeConfig)
	image.RegisterFormat("jpg", "jpg", jpeg.Decode, jpeg.DecodeConfig)
	image.RegisterFormat("png", "png", png.Decode, png.DecodeConfig)
}
