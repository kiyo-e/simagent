package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"hash/fnv"
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
	"unicode"
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
	Index       int           `json:"index"`
	ID          string        `json:"id"`
	Role        string        `json:"role,omitempty"`
	Label       string        `json:"label,omitempty"`
	Value       string        `json:"value,omitempty"`
	NearbyLabel string        `json:"nearbyLabel,omitempty"`
	Enabled     bool          `json:"enabled"`
	Focused     bool          `json:"focused,omitempty"`
	Visible     bool          `json:"visible"`
	Offscreen   bool          `json:"offscreen"`
	Frame       FrameRect     `json:"frame"`
	Center      FramePoint    `json:"center"`
	Source      ElementSource `json:"source"`
	order       int
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
	Stable          bool
	StableSamples   int
	StableInterval  time.Duration
	Order           string
	Format          string
	MinArea         float64
	IncludeRoles    map[string]bool
	ExcludeRoles    map[string]bool
	EmitJSON        bool
}

type uiFlowFile struct {
	Name  string       `json:"name,omitempty"`
	Steps []uiFlowStep `json:"steps"`
}

type uiFlowStep struct {
	Name           string          `json:"name,omitempty"`
	Action         string          `json:"action"`
	Selectors      uiFlowSelectors `json:"selectors,omitempty"`
	Text           string          `json:"text,omitempty"`
	Into           *bool           `json:"into,omitempty"`
	Verify         bool            `json:"verify,omitempty"`
	Replace        bool            `json:"replace,omitempty"`
	ASCII          bool            `json:"ascii,omitempty"`
	Paste          bool            `json:"paste,omitempty"`
	Direction      string          `json:"direction,omitempty"`
	Distance       float64         `json:"distance,omitempty"`
	Unit           string          `json:"unit,omitempty"`
	X              *float64        `json:"x,omitempty"`
	Y              *float64        `json:"y,omitempty"`
	HasText        string          `json:"hasText,omitempty"`
	InteractiveMin *int            `json:"interactiveMin,omitempty"`
	Timeout        string          `json:"timeout,omitempty"`
	Interval       string          `json:"interval,omitempty"`
	Wait           uiFlowWait      `json:"wait,omitempty"`
}

type uiFlowSelectors struct {
	Index    *int   `json:"index,omitempty"`
	ID       string `json:"id,omitempty"`
	Label    string `json:"label,omitempty"`
	Contains string `json:"contains,omitempty"`
}

type uiFlowWait struct {
	HasText        string `json:"hasText,omitempty"`
	InteractiveMin *int   `json:"interactiveMin,omitempty"`
	Timeout        string `json:"timeout,omitempty"`
	Interval       string `json:"interval,omitempty"`
}

var interactiveRoleHints = []string{
	"button", "textfield", "securetextfield", "switch", "slider", "cell", "link",
}

var textInputRoleHints = []string{
	"textfield", "securetextfield", "searchfield", "textarea", "textview",
}

const (
	backspaceKeyCode = "42"
	defaultClearKeys = 72
)

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
	fs.BoolVar(&opts.Stable, "stable", false, "require stable ui tree before accepting frame")
	fs.IntVar(&opts.StableSamples, "stable-samples", 3, "number of ui samples for stability check")
	fs.DurationVar(&opts.StableInterval, "stable-interval", 250*time.Millisecond, "interval between stable ui samples")
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
	if opts.Stable && !opts.UI {
		return opts.EmitJSON, &AppError{Code: "USAGE", Message: "--stable requires --ui"}
	}
	if opts.StableSamples < 2 {
		return opts.EmitJSON, &AppError{Code: "USAGE", Message: "--stable-samples must be >= 2"}
	}
	if opts.StableInterval < 0 {
		return opts.EmitJSON, &AppError{Code: "USAGE", Message: "--stable-interval must be >= 0"}
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
		if opts.Stable {
			samples, stableErr := a.captureStableUISamples(target.UDID, opts)
			if stableErr != nil {
				return opts.EmitJSON, stableErr
			}
			lastSample := samples[len(samples)-1]
			rawUI = lastSample.Raw
			allElements = lastSample.Elements
			allCount = lastSample.AllCount
			interactiveCount = lastSample.InteractiveCount
			for i, sample := range samples {
				samplePath := filepath.Join(opts.OutDir, fmt.Sprintf("ui.sample-%02d.raw.json", i+1))
				if writeErr := writeJSONFile(samplePath, sample.Raw); writeErr == nil {
					artifacts[fmt.Sprintf("uiSample%02d", i+1)] = samplePath
				}
			}
			if writeErr := writeJSONFile(uiRawPath, rawUI); writeErr != nil {
				return opts.EmitJSON, writeErr
			}
		} else {
			sample, sampleErr := a.captureUISample(target.UDID, opts)
			if sampleErr != nil {
				a.logf("idb ui describe-all failed: %v", sampleErr)
				rawUI = map[string]any{"error": renderError(sampleErr)}
				if writeErr := writeJSONFile(uiRawPath, rawUI); writeErr != nil {
					return opts.EmitJSON, writeErr
				}
			} else {
				rawUI = sample.Raw
				allElements = sample.Elements
				allCount = sample.AllCount
				interactiveCount = sample.InteractiveCount
				if writeErr := writeJSONFile(uiRawPath, rawUI); writeErr != nil {
					return opts.EmitJSON, writeErr
				}
			}
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
		return a.opts.JSON, &AppError{Code: "USAGE", Message: "ui subcommand required: tap|type|clear|swipe|wait|button|flow"}
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
		label := fs.String("label", "", "tap by exact label")
		contains := fs.String("contains", "", "tap by partial label/value")
		from := fs.String("from", "", "path to elements.json")
		localJSON := fs.Bool("json", false, "")
		if err := fs.Parse(args); err != nil {
			return emitJSON, &AppError{Code: "USAGE", Message: err.Error()}
		}
		emitJSON = emitJSON || *localJSON
		if *unit != "pt" && *unit != "px" {
			return emitJSON, &AppError{Code: "USAGE", Message: "--unit must be pt|px"}
		}

		selectorCount := 0
		if *index >= 0 {
			selectorCount++
		}
		if strings.TrimSpace(*id) != "" {
			selectorCount++
		}
		if strings.TrimSpace(*label) != "" {
			selectorCount++
		}
		if strings.TrimSpace(*contains) != "" {
			selectorCount++
		}
		if selectorCount > 1 {
			return emitJSON, &AppError{Code: "USAGE", Message: "choose only one selector: --index|--id|--label|--contains"}
		}

		var x float64
		var y float64
		by := "coord"
		fallbackUsed := false
		usedIndex := -1
		usedID := ""
		usedLabel := strings.TrimSpace(*label)
		usedContains := strings.TrimSpace(*contains)

		if selectorCount > 0 {
			elements := []Element{}
			if strings.TrimSpace(*from) != "" || *index >= 0 || strings.TrimSpace(*id) != "" {
				loadedElements, _, loadErr := loadElementsAndTransform(*from)
				if loadErr != nil {
					return emitJSON, loadErr
				}
				elements = loadedElements
			} else if loadedElements, _, loadErr := loadElementsAndTransform(*from); loadErr == nil {
				elements = loadedElements
			}
			elem, err := pickElementBySelectors(elements, *index, *id, usedLabel, usedContains)
			if err != nil {
				snapshot, snapErr := a.captureElements(target.UDID)
				if snapErr == nil {
					elem, err = pickElementBySelectors(snapshot.Elements, *index, *id, usedLabel, usedContains)
					if err == nil {
						by = "live-scan"
					}
					if err != nil && (usedLabel != "" || usedContains != "") {
						if fallback, ok := pickIntentFallbackElement(snapshot.Elements, usedLabel, usedContains); ok {
							elem = fallback
							err = nil
							by = "intent-fallback"
							fallbackUsed = true
						}
					}
					if err != nil && (usedLabel != "" || usedContains != "") {
						if fallback, ok := pickSystemFallbackElement(snapshot.Elements, usedLabel, usedContains); ok {
							elem = fallback
							err = nil
							by = "system-fallback"
							fallbackUsed = true
						}
					}
				}
				if err != nil {
					return emitJSON, err
				}
			}
			tapPoint := elem.Center
			if isTextInputRole(elem.Role) {
				tapPoint = focusPointForElement(elem)
			}
			x = tapPoint.X
			y = tapPoint.Y
			if *index >= 0 {
				by = "index"
				usedIndex = *index
			}
			if *id != "" {
				by = "id"
				usedID = *id
			}
			if usedLabel != "" && !fallbackUsed {
				by = "label"
			}
			if usedContains != "" && !fallbackUsed {
				by = "contains"
			}
		} else {
			vals := fs.Args()
			if len(vals) != 2 {
				return emitJSON, &AppError{Code: "USAGE", Message: "usage: simagent ui tap <x> <y> [--unit pt|px] | --index <n> | --id <id> | --label <text> | --contains <text>"}
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
		if usedLabel != "" {
			resp["label"] = usedLabel
		}
		if usedContains != "" {
			resp["contains"] = usedContains
		}
		if fallbackUsed {
			resp["fallback"] = "system-ui"
		}
		if emitJSON {
			a.printJSON(resp)
		} else {
			fmt.Printf("tap %.2f %.2f\n", x, y)
		}
		return emitJSON, nil

	case "type":
		opts, err := parseUITypeArgs(args)
		if err != nil {
			return emitJSON, err
		}
		emitJSON = emitJSON || opts.JSON
		text := strings.TrimSpace(opts.Text)
		if text == "" {
			return emitJSON, &AppError{Code: "USAGE", Message: "usage: simagent ui type --text \"...\" [--into --index <n>|--id <id>|--label <text>|--contains <text>] [--replace] [--ascii|--paste] [--verify]"}
		}
		if opts.Replace && !opts.Into {
			return emitJSON, &AppError{Code: "USAGE", Message: "--replace requires --into"}
		}
		if opts.Paste && opts.LegacyTypeParsing {
			return emitJSON, &AppError{Code: "USAGE", Message: "--paste is not supported with --legacy-type-parsing"}
		}

		selectorCount := countElementSelectors(opts.Index, opts.ID, opts.Label, opts.Contains)
		if opts.Into && selectorCount != 1 {
			return emitJSON, &AppError{Code: "USAGE", Message: "--into requires exactly one selector: --index|--id|--label|--contains"}
		}
		if !opts.Into && selectorCount > 0 {
			return emitJSON, &AppError{Code: "USAGE", Message: "selector flags require --into"}
		}

		prepared, inputMode, err := prepareTypedText(text, opts.ASCII, opts.Paste)
		if err != nil {
			return emitJSON, err
		}

		var focused *Element
		if opts.Into {
			elem, resolveErr := a.resolveElementForInput(target.UDID, opts.From, opts.Index, opts.ID, opts.Label, opts.Contains)
			if resolveErr != nil {
				return emitJSON, resolveErr
			}
			focusedElem, focusErr := a.focusElementWithRetry(target.UDID, elem, opts.FocusRetries)
			if focusErr != nil {
				return emitJSON, focusErr
			}
			focused = &focusedElem
			if opts.Replace {
				clearPoint := clearPointForElement(focusedElem)
				if _, err := a.runIDB(target.UDID, "ui", "tap", idbCoordArg(clearPoint.X), idbCoordArg(clearPoint.Y)); err != nil {
					return emitJSON, wrapAppErrCode(err, "IDB_UI_FAILED", "focus tap failed before replace")
				}
				estimate := estimateClearBackspaces(focusedElem)
				if clearErr := a.clearFocusedInput(target.UDID, estimate); clearErr != nil {
					return emitJSON, clearErr
				}
			}
		}

		if err := a.submitTextInput(target.UDID, prepared, opts.Paste, focused); err != nil {
			return emitJSON, err
		}

		resp := map[string]any{"ok": true, "action": "type", "text": prepared, "inputMode": inputMode}
		if opts.ASCII {
			resp["ascii"] = true
		}
		if opts.Paste {
			resp["paste"] = true
		}
		if opts.Replace {
			resp["replace"] = true
		}
		if opts.Verify {
			verify, err := a.verifyTypeResult(target.UDID, prepared, focused)
			if err != nil {
				return emitJSON, err
			}
			resp["verified"] = true
			resp["verify"] = verify
		}
		if emitJSON {
			a.printJSON(resp)
		} else {
			fmt.Printf("typed: %s\n", prepared)
		}
		return emitJSON, nil

	case "clear":
		fs := flag.NewFlagSet("ui clear", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		index := fs.Int("index", -1, "element index")
		id := fs.String("id", "", "element id")
		label := fs.String("label", "", "clear by exact label")
		contains := fs.String("contains", "", "clear by partial label/value")
		from := fs.String("from", "", "path to elements.json")
		backspaces := fs.Int("max-backspaces", defaultClearKeys, "maximum backspaces to send")
		localJSON := fs.Bool("json", false, "")
		if err := fs.Parse(args); err != nil {
			return emitJSON, &AppError{Code: "USAGE", Message: err.Error()}
		}
		emitJSON = emitJSON || *localJSON
		if fs.NArg() != 0 {
			return emitJSON, &AppError{Code: "USAGE", Message: "ui clear does not accept positional args"}
		}
		if *backspaces <= 0 {
			return emitJSON, &AppError{Code: "USAGE", Message: "--max-backspaces must be > 0"}
		}
		if countElementSelectors(*index, *id, *label, *contains) != 1 {
			return emitJSON, &AppError{Code: "USAGE", Message: "ui clear requires exactly one selector: --index|--id|--label|--contains"}
		}

		elem, err := a.resolveElementForInput(target.UDID, *from, *index, *id, *label, *contains)
		if err != nil {
			return emitJSON, err
		}
		if _, err := a.focusElementWithRetry(target.UDID, elem, 2); err != nil {
			return emitJSON, err
		}
		clearPoint := clearPointForElement(elem)
		if _, err := a.runIDB(target.UDID, "ui", "tap", idbCoordArg(clearPoint.X), idbCoordArg(clearPoint.Y)); err != nil {
			return emitJSON, wrapAppErrCode(err, "IDB_UI_FAILED", "focus tap failed before clear")
		}
		estimate := *backspaces
		auto := estimateClearBackspaces(elem)
		if auto > estimate {
			estimate = auto
		}
		if err := a.clearFocusedInput(target.UDID, estimate); err != nil {
			return emitJSON, err
		}

		resp := map[string]any{
			"ok":         true,
			"action":     "clear",
			"backspaces": estimate,
			"selector": map[string]any{
				"index":    *index,
				"id":       strings.TrimSpace(*id),
				"label":    strings.TrimSpace(*label),
				"contains": strings.TrimSpace(*contains),
			},
		}
		if emitJSON {
			a.printJSON(resp)
		} else {
			fmt.Printf("cleared (%d backspaces)\n", estimate)
		}
		return emitJSON, nil

	case "flow":
		return a.cmdUIFlow(target, args, emitJSON)

	case "wait":
		fs := flag.NewFlagSet("ui wait", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		hasText := fs.String("has-text", "", "substring to wait for (label/value)")
		interactiveMin := fs.Int("interactive-min", -1, "minimum interactive count")
		timeout := fs.Duration("timeout", 20*time.Second, "maximum wait duration")
		interval := fs.Duration("interval", 700*time.Millisecond, "poll interval")
		localJSON := fs.Bool("json", false, "")
		if err := fs.Parse(args); err != nil {
			return emitJSON, &AppError{Code: "USAGE", Message: err.Error()}
		}
		emitJSON = emitJSON || *localJSON
		if fs.NArg() != 0 {
			return emitJSON, &AppError{Code: "USAGE", Message: "ui wait does not accept positional args"}
		}
		if strings.TrimSpace(*hasText) == "" && *interactiveMin < 0 {
			return emitJSON, &AppError{Code: "USAGE", Message: "ui wait requires --has-text and/or --interactive-min"}
		}
		if *timeout <= 0 {
			return emitJSON, &AppError{Code: "USAGE", Message: "--timeout must be > 0"}
		}
		if *interval <= 0 {
			return emitJSON, &AppError{Code: "USAGE", Message: "--interval must be > 0"}
		}

		waitResp, err := a.waitForCondition(target.UDID, strings.TrimSpace(*hasText), *interactiveMin, *timeout, *interval)
		if err != nil {
			return emitJSON, err
		}
		if emitJSON {
			a.printJSON(waitResp)
		} else {
			fmt.Printf("wait ok (%v attempts)\n", waitResp["attempts"])
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

type frameUISample struct {
	Raw              any
	Elements         []Element
	AllCount         int
	InteractiveCount int
	Hash             string
}

func (a *App) captureUISample(udid string, opts frameOptions) (frameUISample, error) {
	cmd, runErr := a.runIDB(udid, "ui", "describe-all", "--json")
	if runErr != nil {
		return frameUISample{}, wrapAppErrCode(runErr, "IDB_UI_FAILED", "failed to capture ui tree")
	}
	parsed, parseErr := decodeJSONOrWrap(cmd.Stdout)
	if parseErr != nil {
		parsed = map[string]any{"raw": cmd.Stdout}
	}
	elements, allCount, interactiveCount := normalizeElements(parsed, opts)
	return frameUISample{
		Raw:              parsed,
		Elements:         elements,
		AllCount:         allCount,
		InteractiveCount: interactiveCount,
		Hash:             hashElementSet(elements),
	}, nil
}

func (a *App) captureStableUISamples(udid string, opts frameOptions) ([]frameUISample, error) {
	samples := make([]frameUISample, 0, opts.StableSamples)
	hashes := make([]string, 0, opts.StableSamples)
	for i := 0; i < opts.StableSamples; i++ {
		sample, err := a.captureUISample(udid, opts)
		if err != nil {
			return nil, err
		}
		samples = append(samples, sample)
		hashes = append(hashes, sample.Hash)
		if i+1 < opts.StableSamples && opts.StableInterval > 0 {
			time.Sleep(opts.StableInterval)
		}
	}
	if !allStringsEqual(hashes) {
		return nil, &AppError{
			Code:    "FRAME_UNSTABLE",
			Message: "ui tree changed during stable sampling",
			Details: map[string]any{
				"hashes":   hashes,
				"samples":  opts.StableSamples,
				"interval": opts.StableInterval.String(),
			},
		}
	}
	return samples, nil
}

func hashElementSet(elements []Element) string {
	h := fnv.New64a()
	for _, elem := range elements {
		_, _ = fmt.Fprintf(h, "%s|%s|%s|%s|%.1f|%.1f|%.1f|%.1f|%t|%t|%t\n",
			elem.ID,
			strings.ToLower(strings.TrimSpace(elem.Role)),
			strings.TrimSpace(elem.Label),
			strings.TrimSpace(elem.Value),
			elem.Frame.X,
			elem.Frame.Y,
			elem.Frame.W,
			elem.Frame.H,
			elem.Enabled,
			elem.Visible,
			elem.Offscreen,
		)
	}
	return strconv.FormatUint(h.Sum64(), 16)
}

func allStringsEqual(values []string) bool {
	if len(values) <= 1 {
		return true
	}
	base := values[0]
	for _, v := range values[1:] {
		if v != base {
			return false
		}
	}
	return true
}

func (a *App) cmdUIFlow(target SimTarget, args []string, emitJSON bool) (bool, error) {
	if len(args) == 0 {
		return emitJSON, &AppError{Code: "USAGE", Message: "usage: simagent ui flow run --file <path> [--resume-from <step>]"}
	}
	sub := args[0]
	if sub != "run" {
		return emitJSON, &AppError{Code: "USAGE", Message: "unknown ui flow subcommand: " + sub}
	}

	fs := flag.NewFlagSet("ui flow run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	file := fs.String("file", "", "path to flow json")
	resumeFrom := fs.Int("resume-from", 1, "1-based step index to resume from")
	localJSON := fs.Bool("json", false, "")
	if err := fs.Parse(args[1:]); err != nil {
		return emitJSON, &AppError{Code: "USAGE", Message: err.Error()}
	}
	emitJSON = emitJSON || *localJSON
	if strings.TrimSpace(*file) == "" {
		return emitJSON, &AppError{Code: "USAGE", Message: "--file is required"}
	}
	if *resumeFrom <= 0 {
		return emitJSON, &AppError{Code: "USAGE", Message: "--resume-from must be >= 1"}
	}
	if fs.NArg() != 0 {
		return emitJSON, &AppError{Code: "USAGE", Message: "ui flow run does not accept positional args"}
	}

	b, err := os.ReadFile(*file)
	if err != nil {
		return emitJSON, wrapErr("IO_ERROR", "failed to read flow file", err)
	}
	var flow uiFlowFile
	if err := json.Unmarshal(b, &flow); err != nil {
		return emitJSON, wrapErr("IO_ERROR", "failed to parse flow file json", err)
	}
	if len(flow.Steps) == 0 {
		return emitJSON, &AppError{Code: "USAGE", Message: "flow file must include at least one step"}
	}
	if *resumeFrom > len(flow.Steps) {
		return emitJSON, &AppError{Code: "USAGE", Message: "--resume-from exceeds number of steps"}
	}

	results := make([]map[string]any, 0, len(flow.Steps)-(*resumeFrom-1))
	for i := *resumeFrom - 1; i < len(flow.Steps); i++ {
		step := flow.Steps[i]
		stepResult, stepErr := a.executeFlowStep(target, step)
		if stepErr != nil {
			outDir := filepath.Join(os.TempDir(), "simagent", fmt.Sprintf("flow-failure-%s-step-%02d", time.Now().Format("2006-01-02T15-04-05"), i+1))
			artifacts := a.captureFailureArtifacts(target.UDID, outDir)
			return emitJSON, &AppError{
				Code:    "FLOW_STEP_FAILED",
				Message: fmt.Sprintf("flow step %d failed", i+1),
				Details: map[string]any{
					"step":       i + 1,
					"name":       strings.TrimSpace(step.Name),
					"action":     strings.TrimSpace(step.Action),
					"resumeFrom": i + 1,
					"error":      renderError(stepErr),
					"artifacts":  artifacts,
				},
			}
		}
		stepResult["step"] = i + 1
		stepResult["name"] = strings.TrimSpace(step.Name)
		results = append(results, stepResult)
	}

	resp := map[string]any{
		"ok":         true,
		"action":     "flow-run",
		"name":       flow.Name,
		"file":       *file,
		"resumeFrom": *resumeFrom,
		"steps":      results,
	}
	if emitJSON {
		a.printJSON(resp)
	} else {
		fmt.Printf("flow completed: %d steps\n", len(results))
	}
	return emitJSON, nil
}

func (a *App) executeFlowStep(target SimTarget, step uiFlowStep) (map[string]any, error) {
	action := strings.ToLower(strings.TrimSpace(step.Action))
	switch action {
	case "tap":
		if step.X != nil && step.Y != nil {
			if _, err := a.runIDB(target.UDID, "ui", "tap", idbCoordArg(*step.X), idbCoordArg(*step.Y)); err != nil {
				return nil, wrapAppErrCode(err, "IDB_UI_FAILED", "tap failed")
			}
			return map[string]any{"action": "tap", "by": "coord", "targetPt": map[string]any{"x": *step.X, "y": *step.Y}}, nil
		}
		index, id, label, contains := selectorsFromFlowStep(step)
		if countElementSelectors(index, id, label, contains) != 1 {
			return nil, &AppError{Code: "USAGE", Message: "flow tap requires x/y or exactly one selector"}
		}
		elem, err := a.resolveElementForInput(target.UDID, "", index, id, label, contains)
		if err != nil {
			return nil, err
		}
		tapPoint := elem.Center
		if isTextInputRole(elem.Role) {
			tapPoint = focusPointForElement(elem)
		}
		if _, err := a.runIDB(target.UDID, "ui", "tap", idbCoordArg(tapPoint.X), idbCoordArg(tapPoint.Y)); err != nil {
			return nil, wrapAppErrCode(err, "IDB_UI_FAILED", "tap failed")
		}
		return map[string]any{
			"action":   "tap",
			"by":       "selector",
			"selector": map[string]any{"index": index, "id": id, "label": label, "contains": contains},
			"targetPt": map[string]any{"x": tapPoint.X, "y": tapPoint.Y},
		}, nil
	case "type":
		text := strings.TrimSpace(step.Text)
		if text == "" {
			return nil, &AppError{Code: "USAGE", Message: "flow type requires text"}
		}
		index, id, label, contains := selectorsFromFlowStep(step)
		into := step.Into != nil && *step.Into
		if step.Into == nil && countElementSelectors(index, id, label, contains) == 1 {
			into = true
		}
		if into && countElementSelectors(index, id, label, contains) != 1 {
			return nil, &AppError{Code: "USAGE", Message: "flow type --into requires exactly one selector"}
		}

		prepared, inputMode, err := prepareTypedText(text, step.ASCII, step.Paste)
		if err != nil {
			return nil, err
		}

		var focused *Element
		if into {
			elem, err := a.resolveElementForInput(target.UDID, "", index, id, label, contains)
			if err != nil {
				return nil, err
			}
			focusedElem, err := a.focusElementWithRetry(target.UDID, elem, 2)
			if err != nil {
				return nil, err
			}
			focused = &focusedElem
			if step.Replace {
				clearPoint := clearPointForElement(focusedElem)
				if _, err := a.runIDB(target.UDID, "ui", "tap", idbCoordArg(clearPoint.X), idbCoordArg(clearPoint.Y)); err != nil {
					return nil, wrapAppErrCode(err, "IDB_UI_FAILED", "focus tap failed before replace")
				}
				if err := a.clearFocusedInput(target.UDID, estimateClearBackspaces(focusedElem)); err != nil {
					return nil, err
				}
			}
		}
		if err := a.submitTextInput(target.UDID, prepared, step.Paste, focused); err != nil {
			return nil, err
		}
		result := map[string]any{"action": "type", "text": prepared, "inputMode": inputMode}
		if step.Verify {
			verify, err := a.verifyTypeResult(target.UDID, prepared, focused)
			if err != nil {
				return nil, err
			}
			result["verify"] = verify
		}
		return result, nil
	case "clear":
		index, id, label, contains := selectorsFromFlowStep(step)
		if countElementSelectors(index, id, label, contains) != 1 {
			return nil, &AppError{Code: "USAGE", Message: "flow clear requires exactly one selector"}
		}
		elem, err := a.resolveElementForInput(target.UDID, "", index, id, label, contains)
		if err != nil {
			return nil, err
		}
		if _, err := a.focusElementWithRetry(target.UDID, elem, 2); err != nil {
			return nil, err
		}
		clearPoint := clearPointForElement(elem)
		if _, err := a.runIDB(target.UDID, "ui", "tap", idbCoordArg(clearPoint.X), idbCoordArg(clearPoint.Y)); err != nil {
			return nil, wrapAppErrCode(err, "IDB_UI_FAILED", "focus tap failed before clear")
		}
		count := estimateClearBackspaces(elem)
		if err := a.clearFocusedInput(target.UDID, count); err != nil {
			return nil, err
		}
		return map[string]any{"action": "clear", "backspaces": count}, nil
	case "swipe":
		direction := strings.ToLower(strings.TrimSpace(step.Direction))
		if direction == "" {
			direction = "up"
		}
		if direction != "up" && direction != "down" && direction != "left" && direction != "right" {
			return nil, &AppError{Code: "USAGE", Message: "flow swipe direction must be up|down|left|right"}
		}
		distance := step.Distance
		if distance <= 0 {
			distance = 220
		}
		startX := 196.0
		startY := 426.0
		index, id, _, _ := selectorsFromFlowStep(step)
		if countElementSelectors(index, id, "", "") == 1 {
			elem, err := a.resolveElementForInput(target.UDID, "", index, id, "", "")
			if err != nil {
				return nil, err
			}
			startX = elem.Center.X
			startY = elem.Center.Y
		}
		endX := startX
		endY := startY
		switch direction {
		case "up":
			endY -= distance
		case "down":
			endY += distance
		case "left":
			endX -= distance
		case "right":
			endX += distance
		}
		if _, err := a.runIDB(target.UDID, "ui", "swipe", idbCoordArg(startX), idbCoordArg(startY), idbCoordArg(endX), idbCoordArg(endY)); err != nil {
			return nil, wrapAppErrCode(err, "IDB_UI_FAILED", "swipe failed")
		}
		return map[string]any{"action": "swipe", "direction": direction}, nil
	case "wait":
		hasText := strings.TrimSpace(step.HasText)
		interactiveMin := -1
		if step.InteractiveMin != nil {
			interactiveMin = *step.InteractiveMin
		}
		timeoutRaw := strings.TrimSpace(step.Timeout)
		intervalRaw := strings.TrimSpace(step.Interval)
		if strings.TrimSpace(step.Wait.HasText) != "" {
			hasText = strings.TrimSpace(step.Wait.HasText)
		}
		if step.Wait.InteractiveMin != nil {
			interactiveMin = *step.Wait.InteractiveMin
		}
		if strings.TrimSpace(step.Wait.Timeout) != "" {
			timeoutRaw = strings.TrimSpace(step.Wait.Timeout)
		}
		if strings.TrimSpace(step.Wait.Interval) != "" {
			intervalRaw = strings.TrimSpace(step.Wait.Interval)
		}
		timeout := 20 * time.Second
		interval := 700 * time.Millisecond
		var err error
		if timeoutRaw != "" {
			timeout, err = time.ParseDuration(timeoutRaw)
			if err != nil {
				return nil, &AppError{Code: "USAGE", Message: "invalid flow wait timeout: " + timeoutRaw}
			}
		}
		if intervalRaw != "" {
			interval, err = time.ParseDuration(intervalRaw)
			if err != nil {
				return nil, &AppError{Code: "USAGE", Message: "invalid flow wait interval: " + intervalRaw}
			}
		}
		return a.waitForCondition(target.UDID, hasText, interactiveMin, timeout, interval)
	default:
		return nil, &AppError{Code: "USAGE", Message: "unsupported flow action: " + action}
	}
}

func selectorsFromFlowStep(step uiFlowStep) (int, string, string, string) {
	index := -1
	if step.Selectors.Index != nil {
		index = *step.Selectors.Index
	}
	return index, strings.TrimSpace(step.Selectors.ID), strings.TrimSpace(step.Selectors.Label), strings.TrimSpace(step.Selectors.Contains)
}

func (a *App) captureFailureArtifacts(udid, outDir string) map[string]any {
	out := map[string]any{"outDir": outDir}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		out["error"] = err.Error()
		return out
	}
	screenshotPath := filepath.Join(outDir, "screen.png")
	if _, err := a.runSimctl("io", udid, "screenshot", screenshotPath); err == nil {
		out["screenshot"] = screenshotPath
	}
	cmd, err := a.runIDB(udid, "ui", "describe-all", "--json")
	if err == nil {
		rawPath := filepath.Join(outDir, "ui.raw.json")
		if parsed, parseErr := decodeJSONOrWrap(cmd.Stdout); parseErr == nil {
			_ = writeJSONFile(rawPath, parsed)
			out["uiRaw"] = rawPath
		}
	}
	return out
}

func (a *App) resolveElementForInput(udid, from string, index int, id, label, contains string) (Element, error) {
	elements := []Element{}
	if strings.TrimSpace(from) != "" || index >= 0 || strings.TrimSpace(id) != "" {
		loadedElements, _, loadErr := loadElementsAndTransform(from)
		if loadErr != nil {
			return Element{}, loadErr
		}
		elements = loadedElements
	} else if loadedElements, _, loadErr := loadElementsAndTransform(from); loadErr == nil {
		elements = loadedElements
	}
	elem, err := pickElementBySelectors(elements, index, id, label, contains)
	if err == nil {
		return elem, nil
	}

	snapshot, snapErr := a.captureElements(udid)
	if snapErr != nil {
		return Element{}, err
	}
	elem, err = pickElementBySelectors(snapshot.Elements, index, id, label, contains)
	if err == nil {
		return elem, nil
	}
	if fallback, ok := pickIntentFallbackElement(snapshot.Elements, label, contains); ok {
		return fallback, nil
	}
	if fallback, ok := pickSystemFallbackElement(snapshot.Elements, label, contains); ok {
		return fallback, nil
	}
	return Element{}, err
}

func (a *App) focusElementWithRetry(udid string, elem Element, retries int) (Element, error) {
	if retries <= 0 {
		retries = 1
	}
	target := elem
	focusPoint := focusPointForElement(target)
	var lastReason string
	var lastSnapshot *elementSnapshot
	var lastErr error

	for attempt := 1; attempt <= retries; attempt++ {
		if _, err := a.runIDB(udid, "ui", "tap", idbCoordArg(focusPoint.X), idbCoordArg(focusPoint.Y)); err != nil {
			lastErr = wrapAppErrCode(err, "IDB_UI_FAILED", "focus tap failed")
			continue
		}
		time.Sleep(120 * time.Millisecond)
		snapshot, err := a.captureElements(udid)
		if err != nil {
			lastErr = err
			continue
		}
		lastSnapshot = &snapshot
		matched, ok := findBestVerificationTarget(snapshot.Elements, target)
		if !ok {
			lastReason = "target element not found after tap"
			continue
		}
		target = matched
		focusPoint = focusPointForElement(target)
		if canTrustFocus(snapshot.Elements, matched) {
			target.Center = focusPoint
			return target, nil
		}
		lastReason = "focus moved to different element"
	}

	details := map[string]any{
		"selectorId": strings.TrimSpace(elem.ID),
		"attempts":   retries,
	}
	if strings.TrimSpace(lastReason) != "" {
		details["reason"] = lastReason
	}
	if lastErr != nil {
		details["lastError"] = renderError(lastErr)
	}
	if lastSnapshot != nil {
		details["interactive"] = lastSnapshot.InteractiveCount
	}
	return Element{}, &AppError{Code: "TYPE_FOCUS_FAILED", Message: "failed to verify focus target after retries", Details: details}
}

func canTrustFocus(elements []Element, target Element) bool {
	hasFocused := false
	for _, elem := range elements {
		if !elem.Focused {
			continue
		}
		hasFocused = true
		if strings.TrimSpace(target.ID) == "" {
			dx := elem.Center.X - target.Center.X
			dy := elem.Center.Y - target.Center.Y
			if math.Hypot(dx, dy) <= 24 {
				return true
			}
		}
		if elem.ID == target.ID {
			return true
		}
	}
	return !hasFocused
}

func estimateClearBackspaces(elem Element) int {
	runes := len([]rune(strings.TrimSpace(elem.Value)))
	switch {
	case runes <= 0:
		return defaultClearKeys
	case runes < 16:
		return 24
	case runes < 40:
		return runes + 12
	default:
		return minInt(runes+16, 220)
	}
}

func (a *App) clearFocusedInput(udid string, count int) error {
	if count <= 0 {
		count = defaultClearKeys
	}
	args := make([]string, 0, count+2)
	args = append(args, "ui", "key-sequence")
	for i := 0; i < count; i++ {
		args = append(args, backspaceKeyCode)
	}
	if _, err := a.runIDB(udid, args...); err == nil {
		return nil
	}
	for i := 0; i < count; i++ {
		if _, err := a.runIDB(udid, "ui", "key", backspaceKeyCode); err != nil {
			return wrapAppErrCode(err, "IDB_UI_FAILED", "clear text failed")
		}
	}
	return nil
}

func prepareTypedText(text string, asciiMode, pasteMode bool) (string, string, error) {
	out := strings.TrimSpace(text)
	if asciiMode {
		out = asciiOnly(out)
		if strings.TrimSpace(out) == "" {
			return "", "", &AppError{Code: "TYPE_ASCII_EMPTY", Message: "text became empty after ASCII normalization"}
		}
	}
	mode := "type"
	if pasteMode && asciiMode {
		mode = "paste-ascii"
	} else if pasteMode {
		mode = "paste"
	} else if asciiMode {
		mode = "ascii"
	}
	return out, mode, nil
}

func asciiOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 32 && r <= 126 {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (a *App) submitTextInput(udid, text string, pasteMode bool, focused *Element) error {
	_ = pasteMode
	if strings.TrimSpace(text) == "" {
		return nil
	}
	if err := a.typeTextInChunks(udid, text, 4); err != nil {
		return err
	}

	target := focused
	if target == nil {
		if snapshot, err := a.captureElements(udid); err == nil {
			if focusedElem, ok := findFocusedTextInput(snapshot.Elements); ok {
				target = &focusedElem
			}
		}
	}
	if target == nil || isSecureTextInputRole(target.Role) {
		return nil
	}

	lastObserved := ""
	for attempt := 1; attempt <= 4; attempt++ {
		time.Sleep(90 * time.Millisecond)
		snapshot, err := a.captureElements(udid)
		if err != nil {
			return err
		}
		matched, ok := findBestVerificationTarget(snapshot.Elements, *target)
		if !ok {
			if focusedElem, hasFocus := findFocusedTextInput(snapshot.Elements); hasFocus {
				matched = focusedElem
				ok = true
			}
		}
		if !ok {
			return &AppError{
				Code:    "TYPE_VERIFY_FAILED",
				Message: "typed text verification target not found",
				Details: map[string]any{"intended": text, "attempt": attempt},
			}
		}
		target = &matched
		if isSecureTextInputRole(matched.Role) {
			return nil
		}
		observed := strings.TrimSpace(matched.Value)
		if observed == "" {
			observed = strings.TrimSpace(matched.Label)
		}
		lastObserved = observed
		missing, comparable := typedMissingSuffix(text, observed)
		if !comparable {
			return &AppError{
				Code:    "TYPE_INCOMPLETE",
				Message: "typed text does not match target value prefix",
				Details: map[string]any{"intended": text, "observed": observed, "attempt": attempt, "elementId": matched.ID},
			}
		}
		if missing == "" {
			return nil
		}
		if err := a.typeTextInChunks(udid, missing, 4); err != nil {
			return err
		}
	}

	if missing, _ := typedMissingSuffix(text, lastObserved); missing != "" {
		return &AppError{
			Code:    "TYPE_INCOMPLETE",
			Message: "typed text remains incomplete after retries",
			Details: map[string]any{"intended": text, "observed": lastObserved, "missing": missing},
		}
	}
	return nil
}

func (a *App) typeTextInChunks(udid, text string, chunkRunes int) error {
	chunks := splitIntoInputChunks(text, chunkRunes)
	for _, chunk := range chunks {
		if chunk == "" {
			continue
		}
		if _, err := a.runIDB(udid, "ui", "text", chunk); err != nil {
			return wrapAppErrCode(err, "IDB_UI_FAILED", "text input failed")
		}
		if len(chunks) > 1 {
			time.Sleep(60 * time.Millisecond)
		}
	}
	return nil
}

func splitIntoInputChunks(text string, chunkRunes int) []string {
	runes := []rune(text)
	if len(runes) == 0 {
		return nil
	}
	if chunkRunes <= 0 || len(runes) <= chunkRunes {
		return []string{text}
	}
	chunks := make([]string, 0, (len(runes)+chunkRunes-1)/chunkRunes)
	for i := 0; i < len(runes); i += chunkRunes {
		end := i + chunkRunes
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[i:end]))
	}
	return chunks
}

func findFocusedTextInput(elements []Element) (Element, bool) {
	for _, elem := range elements {
		if elem.Enabled && elem.Focused && isTextInputRole(elem.Role) {
			return elem, true
		}
	}
	return Element{}, false
}

func typedMissingSuffix(intended, observed string) (string, bool) {
	intendedRunes := []rune(intended)
	observedRunes := []rune(observed)
	intendedComparable, intendedMap := comparableRunesWithMap(intendedRunes)
	observedComparable, _ := comparableRunesWithMap(observedRunes)
	if len(observedComparable) > len(intendedComparable) {
		return "", false
	}
	for i := 0; i < len(observedComparable); i++ {
		if observedComparable[i] != intendedComparable[i] {
			return "", false
		}
	}
	if len(observedComparable) == len(intendedComparable) {
		return "", true
	}
	start := intendedMap[len(observedComparable)]
	if start < 0 || start > len(intendedRunes) {
		return "", false
	}
	return string(intendedRunes[start:]), true
}

func comparableRunesWithMap(runes []rune) ([]rune, []int) {
	comparable := make([]rune, 0, len(runes))
	indexMap := make([]int, 0, len(runes))
	for i, r := range runes {
		if isIgnoredCompareRune(r) {
			continue
		}
		comparable = append(comparable, r)
		indexMap = append(indexMap, i)
	}
	return comparable, indexMap
}

func isIgnoredCompareRune(r rune) bool {
	return unicode.IsSpace(r)
}

func (a *App) waitForCondition(udid, hasText string, interactiveMin int, timeout, interval time.Duration) (map[string]any, error) {
	started := time.Now()
	attempts := 0
	lastInteractive := 0
	lastMatches := []string{}
	var lastErr error
	targetText := strings.ToLower(strings.TrimSpace(hasText))

	for {
		attempts++
		snapshot, snapErr := a.captureElements(udid)
		if snapErr != nil {
			lastErr = snapErr
		} else {
			lastErr = nil
			lastInteractive = snapshot.InteractiveCount
			textMatches := matchingTextSamples(snapshot.Elements, targetText)
			lastMatches = textMatches
			interactiveOK := interactiveMin < 0 || snapshot.InteractiveCount >= interactiveMin
			textOK := targetText == "" || len(textMatches) > 0
			if interactiveOK && textOK {
				resp := map[string]any{
					"ok":          true,
					"action":      "wait",
					"attempts":    attempts,
					"elapsedMs":   time.Since(started).Milliseconds(),
					"interactive": snapshot.InteractiveCount,
				}
				if targetText != "" {
					resp["hasText"] = hasText
					resp["matches"] = textMatches
				}
				return resp, nil
			}
		}

		if time.Since(started) >= timeout {
			details := map[string]any{
				"attempts":       attempts,
				"elapsedMs":      time.Since(started).Milliseconds(),
				"interactive":    lastInteractive,
				"interactiveMin": interactiveMin,
			}
			if targetText != "" {
				details["hasText"] = hasText
				details["lastMatches"] = lastMatches
			}
			if lastErr != nil {
				details["lastError"] = renderError(lastErr)
			}
			return nil, &AppError{Code: "WAIT_TIMEOUT", Message: "wait condition not met before timeout", Details: details}
		}
		time.Sleep(interval)
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

	screenRect := inferScreenRect(rawUI, allCandidates)
	for i := range allCandidates {
		allCandidates[i].Visible, allCandidates[i].Offscreen = classifyVisibility(allCandidates[i].Frame, screenRect)
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
	allCandidates = addNearbyLabels(allCandidates)

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
	role := firstString(node.Map, []string{
		"role", "type", "elementType", "axRole", "roleDescription", "AXRole", "wdType",
	})
	label := firstString(node.Map, []string{
		"label", "name", "title", "placeholder", "accessibilityLabel", "axLabel", "AXLabel", "wdLabel",
	})
	value := firstString(node.Map, []string{
		"value", "text", "axValue", "AXValue", "displayValue", "wdValue", "selectedText",
	})
	if label == "" && value != "" {
		label = value
	}
	enabled := firstBoolWithDefault(node.Map, []string{"enabled", "isEnabled"}, true)
	focused := firstBoolWithDefault(node.Map, []string{"focused", "isFocused", "hasFocus", "AXFocused"}, false)

	return Element{
		ID:      id,
		Role:    role,
		Label:   label,
		Value:   value,
		Enabled: enabled,
		Focused: focused,
		Visible: true,
		Frame:   rect,
		Center:  FramePoint{X: rect.X + rect.W/2, Y: rect.Y + rect.H/2, Unit: "pt"},
		Source:  ElementSource{Tool: "idb", Method: "describe-all"},
	}, true
}

func inferScreenRect(rawUI any, elements []Element) FrameRect {
	if root, ok := rawUI.(map[string]any); ok {
		if rect, ok := findRect(root); ok && rect.W > 0 && rect.H > 0 {
			return rect
		}
	}
	minX := math.MaxFloat64
	minY := math.MaxFloat64
	maxX := 0.0
	maxY := 0.0
	for _, e := range elements {
		if e.Frame.W <= 0 || e.Frame.H <= 0 {
			continue
		}
		if e.Frame.X < minX {
			minX = e.Frame.X
		}
		if e.Frame.Y < minY {
			minY = e.Frame.Y
		}
		if e.Frame.X+e.Frame.W > maxX {
			maxX = e.Frame.X + e.Frame.W
		}
		if e.Frame.Y+e.Frame.H > maxY {
			maxY = e.Frame.Y + e.Frame.H
		}
	}
	if minX == math.MaxFloat64 || minY == math.MaxFloat64 || maxX <= minX || maxY <= minY {
		return FrameRect{}
	}
	return FrameRect{X: minX, Y: minY, W: maxX - minX, H: maxY - minY, Unit: "pt"}
}

func classifyVisibility(rect FrameRect, screen FrameRect) (bool, bool) {
	if rect.W <= 0 || rect.H <= 0 {
		return false, true
	}
	if screen.W <= 0 || screen.H <= 0 {
		return true, false
	}
	screenRight := screen.X + screen.W
	screenBottom := screen.Y + screen.H
	rectRight := rect.X + rect.W
	rectBottom := rect.Y + rect.H

	interLeft := math.Max(rect.X, screen.X)
	interTop := math.Max(rect.Y, screen.Y)
	interRight := math.Min(rectRight, screenRight)
	interBottom := math.Min(rectBottom, screenBottom)
	if interRight <= interLeft || interBottom <= interTop {
		return false, true
	}
	offscreen := rect.X < screen.X || rect.Y < screen.Y || rectRight > screenRight || rectBottom > screenBottom
	return true, offscreen
}

func addNearbyLabels(elements []Element) []Element {
	for i := range elements {
		if strings.TrimSpace(elements[i].Label) != "" {
			continue
		}
		bestIdx := -1
		bestScore := math.MaxFloat64
		for j := range elements {
			if i == j {
				continue
			}
			label := strings.TrimSpace(elements[j].Label)
			if label == "" || !elements[j].Visible {
				continue
			}
			dx := elements[i].Center.X - elements[j].Center.X
			dy := elements[i].Center.Y - elements[j].Center.Y
			distance := math.Hypot(dx, dy)
			if distance > 220 {
				continue
			}
			if distance < bestScore {
				bestScore = distance
				bestIdx = j
			}
		}
		if bestIdx >= 0 {
			elements[i].NearbyLabel = elements[bestIdx].Label
		}
	}
	return elements
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

type uiTypeOptions struct {
	Text              string
	Into              bool
	Index             int
	ID                string
	Label             string
	Contains          string
	From              string
	Replace           bool
	ASCII             bool
	Paste             bool
	FocusRetries      int
	Verify            bool
	JSON              bool
	LegacyTypeParsing bool
}

type elementSnapshot struct {
	Elements         []Element
	AllCount         int
	InteractiveCount int
}

func countElementSelectors(index int, id, label, contains string) int {
	count := 0
	if index >= 0 {
		count++
	}
	if strings.TrimSpace(id) != "" {
		count++
	}
	if strings.TrimSpace(label) != "" {
		count++
	}
	if strings.TrimSpace(contains) != "" {
		count++
	}
	return count
}

func pickElementBySelectors(elements []Element, index int, id, label, contains string) (Element, error) {
	label = strings.TrimSpace(label)
	contains = strings.TrimSpace(contains)
	id = strings.TrimSpace(id)
	switch countElementSelectors(index, id, label, contains) {
	case 0:
		return Element{}, &AppError{Code: "USAGE", Message: "selector is required: --index|--id|--label|--contains"}
	case 1:
		if index >= 0 || id != "" {
			return pickElement(elements, index, id)
		}
		return pickElementByText(elements, label, contains)
	default:
		return Element{}, &AppError{Code: "USAGE", Message: "choose only one selector: --index|--id|--label|--contains"}
	}
}

func pickElementByText(elements []Element, exactLabel, partial string) (Element, error) {
	exactLabel = strings.ToLower(strings.TrimSpace(exactLabel))
	partial = strings.ToLower(strings.TrimSpace(partial))
	type candidate struct {
		Element Element
		Score   int
	}
	candidates := make([]candidate, 0)
	for _, elem := range elements {
		if !elem.Enabled {
			continue
		}
		score := 0
		if exactLabel != "" {
			exactHit := false
			if strings.EqualFold(strings.TrimSpace(elem.Label), exactLabel) {
				exactHit = true
				score += 90
			}
			if strings.EqualFold(strings.TrimSpace(elem.Value), exactLabel) {
				exactHit = true
				score += 75
			}
			if strings.EqualFold(strings.TrimSpace(elem.NearbyLabel), exactLabel) {
				exactHit = true
				score += 55
			}
			if !exactHit {
				continue
			}
		}
		if partial != "" {
			text := strings.ToLower(elementText(elem))
			if !strings.Contains(text, partial) {
				continue
			}
			score += 45
		}
		if elem.Visible {
			score += 24
		}
		if !elem.Offscreen {
			score += 10
		}
		if isInteractiveRole(elem.Role) {
			score += 15
		}
		if strings.TrimSpace(elem.Label) != "" {
			score += 8
		}
		candidates = append(candidates, candidate{Element: elem, Score: score})
	}
	if len(candidates) == 0 {
		if exactLabel != "" {
			return Element{}, &AppError{Code: "ELEMENT_NOT_FOUND", Message: "element label not found: " + exactLabel}
		}
		return Element{}, &AppError{Code: "ELEMENT_NOT_FOUND", Message: "element text not found: " + partial}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		return candidates[i].Element.Index < candidates[j].Element.Index
	})
	return candidates[0].Element, nil
}

func pickSystemFallbackElement(elements []Element, label, contains string) (Element, bool) {
	query := strings.ToLower(strings.TrimSpace(label))
	if query == "" {
		query = strings.ToLower(strings.TrimSpace(contains))
	}
	if query == "" {
		return Element{}, false
	}

	rightIntent := containsAnyToken(query, []string{"add", "done", "ok", "allow", "choose", "select", "追加", "完了", "許可", "選択"})
	leftIntent := containsAnyToken(query, []string{"cancel", "close", "back", "dismiss", "キャンセル", "閉じる", "戻る"})
	if !rightIntent && !leftIntent {
		return Element{}, false
	}

	best := Element{}
	found := false
	for _, elem := range elements {
		if !elem.Enabled || !elem.Visible {
			continue
		}
		if elem.Frame.Y > 180 {
			continue
		}
		if !isInteractiveRole(elem.Role) && !strings.Contains(strings.ToLower(elem.Role), "navigation") {
			continue
		}
		if !found {
			best = elem
			found = true
			continue
		}
		if elem.Center.Y < best.Center.Y {
			best = elem
			continue
		}
		if math.Abs(elem.Center.Y-best.Center.Y) <= 20 {
			if rightIntent && elem.Center.X > best.Center.X {
				best = elem
			}
			if leftIntent && elem.Center.X < best.Center.X {
				best = elem
			}
		}
	}
	return best, found
}

func pickIntentFallbackElement(elements []Element, label, contains string) (Element, bool) {
	query := strings.ToLower(strings.TrimSpace(label))
	if query == "" {
		query = strings.ToLower(strings.TrimSpace(contains))
	}
	if query == "" {
		return Element{}, false
	}

	wantBack := containsAnyToken(query, []string{"back", "戻る", "キャンセル", "close", "dismiss"})
	wantCheck := containsAnyToken(query, []string{"check", "checkbox", "同意", "規約", "debug", "デバッグ", "確認"})
	if !wantBack && !wantCheck {
		return Element{}, false
	}

	type candidate struct {
		Element Element
		Score   int
	}
	candidates := make([]candidate, 0)
	for _, elem := range elements {
		if !elem.Enabled || !elem.Visible {
			continue
		}
		if !isInteractiveRole(elem.Role) {
			continue
		}
		score := 0
		text := strings.ToLower(elementText(elem))
		if strings.Contains(text, query) {
			score += 60
		}
		if wantBack {
			if elem.Center.Y <= 180 {
				score += 25
			}
			if elem.Center.X <= 120 {
				score += 20
			}
		}
		if wantCheck {
			if strings.Contains(strings.ToLower(elem.Role), "switch") {
				score += 45
			}
			if elem.Frame.W <= 64 && elem.Frame.H <= 64 {
				score += 22
			}
			if containsAnyToken(strings.ToLower(elem.NearbyLabel), []string{"同意", "規約", "確認", "debug", "デバッグ"}) {
				score += 28
			}
		}
		if strings.TrimSpace(elem.Label) == "" {
			score += 10
		}
		if score > 0 {
			candidates = append(candidates, candidate{Element: elem, Score: score})
		}
	}
	if len(candidates) == 0 {
		return Element{}, false
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		return candidates[i].Element.Index < candidates[j].Element.Index
	})
	return candidates[0].Element, true
}

func containsAnyToken(s string, tokens []string) bool {
	for _, token := range tokens {
		if strings.Contains(s, token) {
			return true
		}
	}
	return false
}

func parseUITypeArgs(args []string) (uiTypeOptions, error) {
	opts := uiTypeOptions{Index: -1, FocusRetries: 2}
	positionals := make([]string, 0)

	nextValue := func(i *int, name string) (string, error) {
		if *i+1 >= len(args) {
			return "", &AppError{Code: "USAGE", Message: "flag needs an argument: " + name}
		}
		*i = *i + 1
		return args[*i], nil
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--into":
			opts.Into = true
		case arg == "--replace":
			opts.Replace = true
		case arg == "--ascii":
			opts.ASCII = true
		case arg == "--paste":
			opts.Paste = true
		case arg == "--verify":
			opts.Verify = true
		case arg == "--json":
			opts.JSON = true
		case arg == "--legacy-type-parsing":
			opts.LegacyTypeParsing = true
		case arg == "--focus-retries":
			raw, err := nextValue(&i, "--focus-retries")
			if err != nil {
				return opts, err
			}
			parsed, convErr := strconv.Atoi(strings.TrimSpace(raw))
			if convErr != nil {
				return opts, &AppError{Code: "USAGE", Message: "--focus-retries must be an integer"}
			}
			opts.FocusRetries = parsed
		case strings.HasPrefix(arg, "--focus-retries="):
			raw := strings.TrimSpace(strings.TrimPrefix(arg, "--focus-retries="))
			parsed, convErr := strconv.Atoi(raw)
			if convErr != nil {
				return opts, &AppError{Code: "USAGE", Message: "--focus-retries must be an integer"}
			}
			opts.FocusRetries = parsed
		case arg == "--index":
			raw, err := nextValue(&i, "--index")
			if err != nil {
				return opts, err
			}
			parsed, convErr := strconv.Atoi(strings.TrimSpace(raw))
			if convErr != nil {
				return opts, &AppError{Code: "USAGE", Message: "--index must be an integer"}
			}
			opts.Index = parsed
		case strings.HasPrefix(arg, "--index="):
			raw := strings.TrimSpace(strings.TrimPrefix(arg, "--index="))
			parsed, convErr := strconv.Atoi(raw)
			if convErr != nil {
				return opts, &AppError{Code: "USAGE", Message: "--index must be an integer"}
			}
			opts.Index = parsed
		case arg == "--id":
			raw, err := nextValue(&i, "--id")
			if err != nil {
				return opts, err
			}
			opts.ID = raw
		case strings.HasPrefix(arg, "--id="):
			opts.ID = strings.TrimPrefix(arg, "--id=")
		case arg == "--label":
			raw, err := nextValue(&i, "--label")
			if err != nil {
				return opts, err
			}
			opts.Label = raw
		case strings.HasPrefix(arg, "--label="):
			opts.Label = strings.TrimPrefix(arg, "--label=")
		case arg == "--contains":
			raw, err := nextValue(&i, "--contains")
			if err != nil {
				return opts, err
			}
			opts.Contains = raw
		case strings.HasPrefix(arg, "--contains="):
			opts.Contains = strings.TrimPrefix(arg, "--contains=")
		case arg == "--from":
			raw, err := nextValue(&i, "--from")
			if err != nil {
				return opts, err
			}
			opts.From = raw
		case strings.HasPrefix(arg, "--from="):
			opts.From = strings.TrimPrefix(arg, "--from=")
		case arg == "--text":
			raw, err := nextValue(&i, "--text")
			if err != nil {
				return opts, err
			}
			opts.Text = raw
		case strings.HasPrefix(arg, "--text="):
			opts.Text = strings.TrimPrefix(arg, "--text=")
		case strings.HasPrefix(arg, "-"):
			return opts, &AppError{Code: "USAGE", Message: "unknown flag for ui type: " + arg}
		default:
			positionals = append(positionals, arg)
		}
	}

	opts.ID = strings.TrimSpace(opts.ID)
	opts.Label = strings.TrimSpace(opts.Label)
	opts.Contains = strings.TrimSpace(opts.Contains)
	opts.Text = strings.TrimSpace(opts.Text)
	if opts.FocusRetries <= 0 {
		return opts, &AppError{Code: "USAGE", Message: "--focus-retries must be >= 1"}
	}
	if opts.Text != "" && len(positionals) > 0 {
		return opts, &AppError{Code: "USAGE", Message: "use either --text or positional text, not both"}
	}
	if opts.Text == "" && len(positionals) > 0 {
		opts.Text = strings.Join(positionals, " ")
	}
	return opts, nil
}

func focusPointForElement(elem Element) FramePoint {
	if !isTextInputRole(elem.Role) {
		return elem.Center
	}
	if elem.Frame.W <= 0 {
		return elem.Center
	}
	inset := math.Max(8, math.Min(elem.Frame.W*0.2, 28))
	x := elem.Frame.X + inset
	maxX := elem.Frame.X + elem.Frame.W - 8
	if x > maxX {
		x = elem.Center.X
	}
	return FramePoint{X: x, Y: elem.Center.Y, Unit: "pt"}
}

func clearPointForElement(elem Element) FramePoint {
	if !isTextInputRole(elem.Role) {
		return elem.Center
	}
	if elem.Frame.W <= 0 {
		return elem.Center
	}
	inset := math.Max(8, math.Min(elem.Frame.W*0.15, 24))
	x := elem.Frame.X + elem.Frame.W - inset
	minX := elem.Frame.X + 6
	if x < minX {
		x = elem.Center.X
	}
	return FramePoint{X: x, Y: elem.Center.Y, Unit: "pt"}
}

func isTextInputRole(role string) bool {
	r := strings.ToLower(strings.TrimSpace(role))
	if r == "" {
		return false
	}
	for _, hint := range textInputRoleHints {
		if strings.Contains(r, hint) {
			return true
		}
	}
	return false
}

func (a *App) captureElements(udid string) (elementSnapshot, error) {
	cmd, err := a.runIDB(udid, "ui", "describe-all", "--json")
	if err != nil {
		return elementSnapshot{}, wrapAppErrCode(err, "IDB_UI_FAILED", "failed to capture ui tree")
	}
	parsed, parseErr := decodeJSONOrWrap(cmd.Stdout)
	if parseErr != nil {
		return elementSnapshot{}, wrapErr("IDB_UI_FAILED", "failed to parse ui tree json", parseErr)
	}
	opts := frameOptions{
		Order:           "reading",
		InteractiveOnly: false,
		IncludeRoles:    map[string]bool{},
		ExcludeRoles:    map[string]bool{},
	}
	elements, allCount, _ := normalizeElements(parsed, opts)
	interactiveVisible := 0
	for _, elem := range elements {
		if elem.Enabled && elem.Visible && isInteractiveRole(elem.Role) {
			interactiveVisible++
		}
	}
	return elementSnapshot{
		Elements:         elements,
		AllCount:         allCount,
		InteractiveCount: interactiveVisible,
	}, nil
}

func (a *App) verifyTypeResult(udid, typedText string, focused *Element) (map[string]any, error) {
	snapshot, err := a.captureElements(udid)
	if err != nil {
		return nil, err
	}

	var target *Element
	if focused != nil {
		if matched, ok := findBestVerificationTarget(snapshot.Elements, *focused); ok {
			target = &matched
		}
	}

	typedNorm := normalizeTextForMatch(typedText)
	if target != nil {
		if elementHasTypedText(*target, typedNorm) {
			return map[string]any{
				"elementId": target.ID,
				"label":     target.Label,
				"value":     target.Value,
			}, nil
		}
		if isSecureTextInputRole(target.Role) && strings.TrimSpace(target.Value) != "" && strings.TrimSpace(target.Value) != strings.TrimSpace(focused.Value) {
			return map[string]any{
				"elementId": target.ID,
				"label":     target.Label,
				"value":     target.Value,
			}, nil
		}
	}

	for _, elem := range snapshot.Elements {
		if elementHasTypedText(elem, typedNorm) {
			return map[string]any{
				"elementId": elem.ID,
				"label":     elem.Label,
				"value":     elem.Value,
			}, nil
		}
	}

	return nil, &AppError{
		Code:    "TYPE_VERIFY_FAILED",
		Message: "typed text could not be verified from latest ui tree",
		Details: map[string]any{"typed": typedText, "interactive": snapshot.InteractiveCount},
	}
}

func findBestVerificationTarget(elements []Element, focused Element) (Element, bool) {
	if strings.TrimSpace(focused.ID) != "" {
		for _, elem := range elements {
			if elem.ID == focused.ID {
				return elem, true
			}
		}
	}
	best := Element{}
	found := false
	bestScore := math.MaxFloat64
	for _, elem := range elements {
		if !isTextInputRole(elem.Role) {
			continue
		}
		dx := elem.Center.X - focused.Center.X
		dy := elem.Center.Y - focused.Center.Y
		score := math.Hypot(dx, dy)
		if score < bestScore {
			bestScore = score
			best = elem
			found = true
		}
	}
	return best, found
}

func matchingTextSamples(elements []Element, needle string) []string {
	if needle == "" {
		return nil
	}
	out := make([]string, 0, 3)
	for _, elem := range elements {
		if !elem.Enabled {
			continue
		}
		if strings.Contains(strings.ToLower(elementText(elem)), needle) {
			out = append(out, strings.TrimSpace(fmt.Sprintf("%s %s", elem.Label, elem.Value)))
			if len(out) >= 3 {
				break
			}
		}
	}
	return out
}

func elementText(elem Element) string {
	parts := []string{
		strings.TrimSpace(elem.Label),
		strings.TrimSpace(elem.Value),
		strings.TrimSpace(elem.NearbyLabel),
		strings.TrimSpace(elem.Role),
	}
	return strings.ToLower(strings.Join(parts, " "))
}

func normalizeTextForMatch(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(s)), ""))
}

func elementHasTypedText(elem Element, typedNorm string) bool {
	if typedNorm == "" {
		return false
	}
	candidates := []string{elem.Value, elem.Label}
	for _, c := range candidates {
		if strings.Contains(normalizeTextForMatch(c), typedNorm) {
			return true
		}
	}
	return false
}

func isSecureTextInputRole(role string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(role)), "securetextfield")
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

func minInt(a, b int) int {
	if a < b {
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
