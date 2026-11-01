package books

import (
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

func clockTestVolume() domain.StructuredMarkdown {
	return ParseStructuredMarkdown(`# 卷纲 · v01
- schema_version: 1
- volume_id: v01

## 节拍 · event-1
- event: 完成复核
- cause: 主角申请
- effect: 获得实证
- chapter_window: 1-3

## 单元 · v01-U1
- title: 起步
- chapter_range: 1-3
- clock_start_day: 3
- clock_end_day: 5
- clock_gap_days: 1
- clock_deadline: event-1@5
- timeline_id: main

## 单元 · v01-U2
- title: 核对
- chapter_range: 4-6
- clock_start_day: 6
- clock_end_day: 7
- clock_gap_days: 1
- timeline_id: main
`, VolumeOutlineProtocol)
}

func TestVolumeClockContract(t *testing.T) {
	if err := ValidateVolumeClockContract(clockTestVolume()); err != nil {
		t.Fatal(err)
	}
}

func TestVolumeClockRejectsRegressionAndUnknownDeadline(t *testing.T) {
	doc := clockTestVolume()
	entities := doc.EntitiesOf("单元")
	entities[1].Fields = replaceFieldForTest(entities[1].Fields, "clock_start_day", "5")
	doc.Entities[1] = entities[1]
	if err := ValidateVolumeClockContract(doc); err == nil || !strings.Contains(err.Error(), "间隔") {
		t.Fatalf("time regression must be rejected: %v", err)
	}
	doc = clockTestVolume()
	for i := range doc.Entities {
		if doc.Entities[i].ID == "v01-U1" {
			doc.Entities[i].Fields = replaceFieldForTest(doc.Entities[i].Fields, "clock_deadline", "unknown@4")
		}
	}
	if err := ValidateVolumeClockContract(doc); err == nil {
		t.Fatal("unknown deadline event must be rejected")
	}
}

func replaceFieldForTest(fields []domain.MarkdownField, name, value string) []domain.MarkdownField {
	out := append([]domain.MarkdownField(nil), fields...)
	for i := range out {
		if out[i].Name == name {
			out[i].Value = value
		}
	}
	return out
}
