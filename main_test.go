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
