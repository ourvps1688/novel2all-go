package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunMemory_Stats(t *testing.T) {
	dir := t.TempDir()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	if err := memoryStats(stdout, stderr, []string{
		"--project=" + dir,
		"--mode=keyword",
	}); err != nil {
		t.Fatalf("memoryStats: %v", err)
	}
	if !strings.Contains(stdout.String(), "MODE") {
		t.Errorf("expected MODE header, got: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "keyword") {
		t.Errorf("expected mode=keyword, got: %s", stdout.String())
	}
}

func TestRunMemory_AddEventAndQuery(t *testing.T) {
	dir := t.TempDir()

	// add-event
	stdout1 := &bytes.Buffer{}
	stderr1 := &bytes.Buffer{}
	if err := memoryAddEvent(stdout1, stderr1, []string{
		"--project=" + dir,
		"--mode=keyword",
		"--chapter=1",
		"--type=general",
		"--text=林雷觉醒血脉之力",
	}); err != nil {
		t.Fatalf("add-event: %v", err)
	}
	if !strings.Contains(stdout1.String(), "added event:") {
		t.Errorf("expected 'added event:', got: %s", stdout1.String())
	}

	// query
	stdout2 := &bytes.Buffer{}
	stderr2 := &bytes.Buffer{}
	if err := memoryQuery(stdout2, stderr2, []string{
		"--project=" + dir,
		"--mode=keyword",
		"--text=觉醒",
		"--top-k=3",
	}); err != nil {
		t.Fatalf("query: %v", err)
	}
	if !strings.Contains(stdout2.String(), "觉醒") {
		t.Errorf("expected query result contains '觉醒', got: %s", stdout2.String())
	}
}

func TestRunMemory_RebuildDryRun(t *testing.T) {
	dir := t.TempDir()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	if err := memoryRebuild(stdout, stderr, []string{
		"--project=" + dir,
		"--mode=keyword",
	}); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if !strings.Contains(stdout.String(), "[dry-run]") {
		t.Errorf("expected [dry-run] marker, got: %s", stdout.String())
	}
}

func TestRunMemory_RebuildForce(t *testing.T) {
	dir := t.TempDir()
	// add 2 events first
	for _, text := range []string{"event1", "event2"} {
		stdout := &bytes.Buffer{}
		_ = memoryAddEvent(stdout, &bytes.Buffer{}, []string{
			"--project=" + dir, "--mode=keyword",
			"--chapter=1", "--type=general", "--text=" + text,
		})
	}

	// rebuild --force
	stdout := &bytes.Buffer{}
	if err := memoryRebuild(stdout, &bytes.Buffer{}, []string{
		"--project=" + dir, "--mode=keyword", "--force",
	}); err != nil {
		t.Fatalf("rebuild --force: %v", err)
	}
	if !strings.Contains(stdout.String(), "cleared 2 events") {
		t.Errorf("expected 'cleared 2 events', got: %s", stdout.String())
	}
}

func TestRunMemory_Query_JSON(t *testing.T) {
	dir := t.TempDir()
	// setup event
	_ = memoryAddEvent(&bytes.Buffer{}, &bytes.Buffer{}, []string{
		"--project=" + dir, "--mode=keyword",
		"--chapter=1", "--type=general", "--text=觉醒血脉",
	})
	stdout := &bytes.Buffer{}
	if err := memoryQuery(stdout, &bytes.Buffer{}, []string{
		"--project=" + dir, "--mode=keyword",
		"--text=觉醒", "--format=json",
	}); err != nil {
		t.Fatalf("query json: %v", err)
	}
	if !strings.Contains(stdout.String(), "\"content\"") {
		t.Errorf("expected JSON output, got: %s", stdout.String())
	}
}

func TestRunMemory_Help(t *testing.T) {
	stdout := &bytes.Buffer{}
	if err := runMemory(stdout, &bytes.Buffer{}, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "内存子系统 CLI") {
		t.Errorf("expected help text, got: %s", stdout.String())
	}
}

func TestRunMemory_UnknownSubcommand(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	err := runMemory(stdout, stderr, []string{"unknown"})
	if err == nil {
		t.Error("expected error for unknown subcommand")
	}
	if !strings.Contains(stderr.String(), "unknown memory subcommand") {
		t.Errorf("expected error message, got: %s", stderr.String())
	}
}
