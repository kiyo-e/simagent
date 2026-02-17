package main

import "testing"

func TestParseUITypeArgsInterspersedFlags(t *testing.T) {
	opts, err := parseUITypeArgs([]string{"170", "--into", "--index", "3", "--verify"})
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if opts.Text != "170" {
		t.Fatalf("unexpected text: %q", opts.Text)
	}
	if !opts.Into {
		t.Fatal("expected into=true")
	}
	if opts.Index != 3 {
		t.Fatalf("unexpected index: %d", opts.Index)
	}
	if !opts.Verify {
		t.Fatal("expected verify=true")
	}
}

func TestParseUITypeArgsTextFlag(t *testing.T) {
	opts, err := parseUITypeArgs([]string{"--text", "170", "--into", "--label", "Height"})
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if opts.Text != "170" {
		t.Fatalf("unexpected text: %q", opts.Text)
	}
	if opts.Label != "Height" {
		t.Fatalf("unexpected label: %q", opts.Label)
	}
	if opts.FocusRetries != 2 {
		t.Fatalf("unexpected focus retries: %d", opts.FocusRetries)
	}
}

func TestParseUITypeArgsReplaceAndModes(t *testing.T) {
	opts, err := parseUITypeArgs([]string{"--text", "１２3abc", "--into", "--index", "4", "--replace", "--ascii", "--paste", "--focus-retries", "3"})
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if !opts.Replace {
		t.Fatal("expected replace=true")
	}
	if !opts.ASCII {
		t.Fatal("expected ascii=true")
	}
	if !opts.Paste {
		t.Fatal("expected paste=true")
	}
	if opts.FocusRetries != 3 {
		t.Fatalf("unexpected focus retries: %d", opts.FocusRetries)
	}
}

func TestPrepareTypedTextASCII(t *testing.T) {
	text, mode, err := prepareTypedText("１２3-abc", true, false)
	if err != nil {
		t.Fatalf("prepare failed: %v", err)
	}
	if text != "3-abc" {
		t.Fatalf("unexpected text: %q", text)
	}
	if mode != "ascii" {
		t.Fatalf("unexpected mode: %q", mode)
	}
}

func TestPrepareTypedTextASCIIEmpty(t *testing.T) {
	if _, _, err := prepareTypedText("あいう", true, false); err == nil {
		t.Fatal("expected error for empty ascii normalized text")
	}
}

func TestAllStringsEqual(t *testing.T) {
	if !allStringsEqual([]string{"aa", "aa", "aa"}) {
		t.Fatal("expected all equal")
	}
	if allStringsEqual([]string{"aa", "bb"}) {
		t.Fatal("expected non-equal")
	}
}

func TestSplitIntoInputChunks(t *testing.T) {
	chunks := splitIntoInputChunks("090-0000-0000", 4)
	if len(chunks) != 4 {
		t.Fatalf("unexpected chunk count: %d", len(chunks))
	}
	if chunks[0] != "090-" || chunks[1] != "0000" || chunks[2] != "-000" || chunks[3] != "0" {
		t.Fatalf("unexpected chunks: %#v", chunks)
	}
}

func TestTypedMissingSuffix(t *testing.T) {
	missing, ok := typedMissingSuffix("090-0000-0000", "090-0000-00")
	if !ok {
		t.Fatal("expected comparable input")
	}
	if missing != "00" {
		t.Fatalf("unexpected missing suffix: %q", missing)
	}
}

func TestTypedMissingSuffixExact(t *testing.T) {
	missing, ok := typedMissingSuffix("090-0000-0000", "090-0000-0000")
	if !ok {
		t.Fatal("expected comparable input")
	}
	if missing != "" {
		t.Fatalf("expected empty missing suffix, got: %q", missing)
	}
}

func TestTypedMissingSuffixMismatch(t *testing.T) {
	if _, ok := typedMissingSuffix("090-0000-0000", "091-0000"); ok {
		t.Fatal("expected non-comparable input")
	}
}

func TestTypedMissingSuffixIgnoresSpaces(t *testing.T) {
	missing, ok := typedMissingSuffix("090-0000-0000", "090 -0000-00")
	if !ok {
		t.Fatal("expected comparable input")
	}
	if missing != "00" {
		t.Fatalf("unexpected missing suffix: %q", missing)
	}
}

func TestPickElementByTextPrefersVisibleInteractive(t *testing.T) {
	elements := []Element{
		{
			Index:   1,
			ID:      "hidden",
			Label:   "Next",
			Visible: false,
			Role:    "Button",
			Enabled: true,
		},
		{
			Index:   2,
			ID:      "visible",
			Label:   "Next",
			Visible: true,
			Role:    "Button",
			Enabled: true,
		},
	}

	picked, err := pickElementByText(elements, "next", "")
	if err != nil {
		t.Fatalf("pick failed: %v", err)
	}
	if picked.ID != "visible" {
		t.Fatalf("picked wrong element: %s", picked.ID)
	}
}

func TestClassifyVisibility(t *testing.T) {
	screen := FrameRect{X: 0, Y: 0, W: 100, H: 100, Unit: "pt"}

	visible, offscreen := classifyVisibility(FrameRect{X: 10, Y: 10, W: 20, H: 20, Unit: "pt"}, screen)
	if !visible || offscreen {
		t.Fatalf("expected visible in-screen element, got visible=%v offscreen=%v", visible, offscreen)
	}

	visible, offscreen = classifyVisibility(FrameRect{X: -5, Y: 10, W: 20, H: 20, Unit: "pt"}, screen)
	if !visible || !offscreen {
		t.Fatalf("expected partially visible offscreen element, got visible=%v offscreen=%v", visible, offscreen)
	}

	visible, offscreen = classifyVisibility(FrameRect{X: 120, Y: 10, W: 20, H: 20, Unit: "pt"}, screen)
	if visible || !offscreen {
		t.Fatalf("expected invisible offscreen element, got visible=%v offscreen=%v", visible, offscreen)
	}
}
